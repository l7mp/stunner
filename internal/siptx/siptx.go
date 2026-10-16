// Package siptx runs SIP transactions over net.Conns, on sipgo's transactions.
package siptx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/pion/logging"
	"github.com/pion/stun/v4"
)

// CancelTimeout bounds the CANCEL a cancelled INVITE sends.
var CancelTimeout = 5 * time.Second

// responseBuffer is the number of responses a client transaction holds for its user.
const responseBuffer = 16

// RequestHandler handles a new request in its own goroutine; tx is nil for the ACK of a 2xx.
type RequestHandler func(req *sip.Request, tx *ServerTx, conn net.Conn)

// ResponseHandler handles a response no client transaction takes. It must not block.
type ResponseHandler func(res *sip.Response, conn net.Conn)

// Layer runs the client and server transactions of the conns it serves.
type Layer struct {
	log  logging.LeveledLogger
	slog *slog.Logger

	mu         sync.Mutex
	onRequest  RequestHandler
	onResponse ResponseHandler
	clients    map[string]*ClientTx
	servers    map[serverKey]*ServerTx
}

// serverKey is the key of a server transaction (RFC 3261 section 17.2.3), scoped to its conn.
type serverKey struct {
	conn net.Conn
	key  string
}

// New creates a transaction layer.
func New(log logging.LeveledLogger) *Layer {
	return &Layer{log: log, slog: slog.New(slog.DiscardHandler),
		onRequest:  func(*sip.Request, *ServerTx, net.Conn) {},
		onResponse: func(*sip.Response, net.Conn) {},
		clients:    map[string]*ClientTx{},
		servers:    map[serverKey]*ServerTx{}}
}

// OnRequest sets the handler of new requests.
func (l *Layer) OnRequest(h RequestHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onRequest = h
}

// OnResponse sets the handler of the responses no client transaction takes.
func (l *Layer) OnResponse(h ResponseHandler) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.onResponse = h
}

// conn is the sip.Connection of the transactions of a conn.
type conn struct{ net.Conn }

func (c conn) WriteMsg(msg sip.Message) error {
	_, err := c.Write([]byte(msg.String()))
	return err
}
func (c conn) Ref(int) int            { return 1 }
func (c conn) TryClose() (int, error) { return 1, nil }
func (c conn) Close() error           { return nil }

// Serve reads the messages of a conn in order until it fails, then terminates its transactions.
// A stream carries messages framed by Content-Length, a datagram one message.
func (l *Layer) Serve(c net.Conn, stream bool) error {
	defer l.terminate(c)
	parser := sip.NewParser().NewSIPStream()
	buf := make([]byte, 65535)
	for {
		n, err := c.Read(buf)
		if err != nil {
			return err
		}
		handle := func(msg sip.Message) {
			msg.SetSource(c.RemoteAddr().String())
			l.handle(msg, c)
		}
		if stream {
			if err := parser.ParseSIPStream(buf[:n], handle); err != nil && !errors.Is(err, sip.ErrParseSipPartial) {
				return fmt.Errorf("malformed SIP stream from %s: %w", c.RemoteAddr(), err)
			}
			continue
		}
		// a STUN keepalive of the flow or its response (RFC 5626 section 4.4.2) is no SIP message
		if stun.IsMessage(buf[:n]) {
			continue
		}
		msg, err := sip.ParseMessage(append([]byte(nil), buf[:n]...))
		if err != nil {
			l.log.Debugf("dropping malformed SIP message from %s: %s", c.RemoteAddr(), err.Error())
			continue
		}
		handle(msg)
	}
}

// terminate ends the transactions of a closed conn.
func (l *Layer) terminate(c net.Conn) {
	l.mu.Lock()
	var clients []*ClientTx
	var servers []*ServerTx
	for _, tx := range l.clients {
		if tx.conn == c {
			clients = append(clients, tx)
		}
	}
	for k, tx := range l.servers {
		if k.conn == c {
			servers = append(servers, tx)
		}
	}
	l.mu.Unlock()
	for _, tx := range clients {
		tx.Terminate()
	}
	for _, tx := range servers {
		tx.Terminate()
	}
}

func (l *Layer) handle(msg sip.Message, c net.Conn) {
	l.mu.Lock()
	onRequest, onResponse := l.onRequest, l.onResponse
	l.mu.Unlock()

	res, ok := msg.(*sip.Response)
	if ok {
		l.mu.Lock()
		tx, found := l.clients[txKey(res, "")]
		l.mu.Unlock()
		if found {
			tx.Receive(res)
		} else {
			onResponse(res, c)
		}
		return
	}

	req := msg.(*sip.Request)
	if req.Via() == nil || req.CSeq() == nil || req.CallID() == nil || req.From() == nil || req.To() == nil {
		if req.Method != sip.ACK {
			_ = l.Write(c, sip.NewResponseFromRequest(req, sip.StatusBadRequest, "Bad Request", nil))
		}
		return
	}
	switch req.Method {
	case sip.ACK:
		l.mu.Lock()
		tx, found := l.servers[serverKey{c, serverTxKey(req, sip.INVITE)}]
		l.mu.Unlock()
		if found {
			// the ACK of a non-2xx final response
			_ = tx.Receive(req)
			return
		}
		go onRequest(req, nil, c)
		return
	case sip.CANCEL:
		l.mu.Lock()
		tx, found := l.servers[serverKey{c, serverTxKey(req, sip.INVITE)}]
		l.mu.Unlock()
		if !found {
			_ = l.Write(c, sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists,
				"Call/Transaction Does Not Exist", nil))
			return
		}
		// the 200 of the CANCEL, then the INVITE transaction sends the 487
		_ = l.Write(c, sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil))
		_ = tx.Receive(req)
		return
	}

	key := serverKey{c, serverTxKey(req, "")}
	l.mu.Lock()
	if tx, found := l.servers[key]; found {
		l.mu.Unlock()
		// a retransmission: the transaction answers it
		_ = tx.Receive(req)
		return
	}
	tx := &ServerTx{ServerTx: sip.NewServerTx(key.key, req, conn{c}, l.slog), cancelled: make(chan struct{})}
	if err := tx.Init(); err != nil {
		l.mu.Unlock()
		return
	}
	l.servers[key] = tx
	l.mu.Unlock()
	tx.OnCancel(func(*sip.Request) { tx.cancelOnce.Do(func() { close(tx.cancelled) }) })
	tx.OnTerminate(func(string, error) {
		l.mu.Lock()
		delete(l.servers, key)
		l.mu.Unlock()
	})
	go onRequest(req, tx, c)
}

// Write sends a message outside a transaction, such as the ACK of a 2xx.
func (l *Layer) Write(c net.Conn, msg sip.Message) error {
	_, err := c.Write([]byte(msg.String()))
	return err
}

// Request starts a client transaction on a conn.
func (l *Layer) Request(req *sip.Request, c net.Conn) (*ClientTx, error) {
	key := txKey(req, "")
	tx := &ClientTx{ClientTx: sip.NewClientTx(key, req, conn{c}, l.slog), l: l, req: req, conn: c,
		responses: make(chan *sip.Response, responseBuffer)}
	l.mu.Lock()
	l.clients[key] = tx
	l.mu.Unlock()
	tx.OnTerminate(func(string, error) {
		l.mu.Lock()
		delete(l.clients, key)
		l.mu.Unlock()
	})
	if err := tx.Init(); err != nil {
		tx.Terminate()
		return nil, err
	}
	go func() {
		// drain the transaction so that Receive never waits for the user
		defer close(tx.responses)
		for {
			select {
			case res := <-tx.ClientTx.Responses():
				// over a reliable transport the transaction ends right after it hands up its final
				// response: deliver it first, waiting for the user only on a full buffer
				select {
				case tx.responses <- res:
					continue
				default:
				}
				select {
				case tx.responses <- res:
				case <-tx.Done():
					return
				}
			case <-tx.Done():
				return
			}
		}
	}()
	return tx, nil
}

// Do sends a request on a conn and waits for its final response.
func (l *Layer) Do(ctx context.Context, req *sip.Request, c net.Conn) (*sip.Response, error) {
	tx, err := l.Request(req, c)
	if err != nil {
		return nil, err
	}
	return tx.Wait(ctx)
}

// ServerTx is a server transaction.
type ServerTx struct {
	*sip.ServerTx
	cancelled  chan struct{}
	cancelOnce sync.Once
}

// Cancelled is closed once a CANCEL ends the transaction, which then sends its 487.
func (tx *ServerTx) Cancelled() <-chan struct{} { return tx.cancelled }

// ClientTx is a client transaction.
type ClientTx struct {
	*sip.ClientTx
	l         *Layer
	req       *sip.Request
	conn      net.Conn
	responses chan *sip.Response
}

// Responses returns the responses of the transaction, closed once it ends.
func (tx *ClientTx) Responses() <-chan *sip.Response { return tx.responses }

// Wait returns the final response. Cancelling the context of a ringing INVITE sends a CANCEL and
// waits for the final response it brings; otherwise it ends the wait.
func (tx *ClientTx) Wait(ctx context.Context) (*sip.Response, error) {
	defer func() {
		// an INVITE transaction stays up for the 2xx retransmissions
		if !tx.req.IsInvite() {
			tx.Terminate()
		}
	}()
	ringing := false
	for {
		select {
		case res, ok := <-tx.responses:
			if !ok {
				if err := tx.Err(); err != nil {
					return nil, err
				}
				return nil, errors.New("transaction terminated")
			}
			if res.IsProvisional() {
				ringing = true
				continue
			}
			return res, nil
		case <-ctx.Done():
			// RFC 3261 section 9.1: no CANCEL before a provisional response
			if !tx.req.IsInvite() || !ringing {
				return nil, ctx.Err()
			}
			cctx, cancel := context.WithTimeout(context.Background(), CancelTimeout)
			_, err := tx.l.Do(cctx, NewCancel(tx.req), tx.conn)
			cancel()
			if err != nil {
				return nil, ctx.Err()
			}
			ctx = context.Background()
		}
	}
}

// NewCancel makes the CANCEL of an INVITE (RFC 3261 section 9.1).
func NewCancel(invite *sip.Request) *sip.Request {
	cancel := sip.NewRequest(sip.CANCEL, invite.Recipient)
	cancel.AppendHeader(invite.Via().Clone())
	for _, name := range []string{"From", "To", "Call-ID", "Route"} {
		for _, h := range invite.GetHeaders(name) {
			cancel.AppendHeader(sip.HeaderClone(h))
		}
	}
	cancel.AppendHeader(&sip.CSeqHeader{SeqNo: invite.CSeq().SeqNo, MethodName: sip.CANCEL})
	mf := sip.MaxForwardsHeader(70)
	cancel.AppendHeader(&mf)
	cancel.SetBody(nil)
	return cancel
}

// txKey is the key of a client transaction: the branch of the top Via and the method of the CSeq,
// INVITE for an ACK, or the method given.
func txKey(msg sip.Message, method sip.RequestMethod) string {
	branch := ""
	if v := msg.Via(); v != nil {
		branch, _ = v.Params.Get("branch")
	}
	if method == "" && msg.CSeq() != nil {
		method = msg.CSeq().MethodName
	}
	if method == sip.ACK {
		method = sip.INVITE
	}
	return branch + "|" + string(method)
}

// serverTxKey is the key of a server transaction: txKey and the sent-by of the top Via.
func serverTxKey(req *sip.Request, method sip.RequestMethod) string {
	v := req.Via()
	return txKey(req, method) + "|" + v.Host + ":" + strconv.Itoa(v.Port)
}
