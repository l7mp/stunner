package icetester

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/pion/datachannel"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
)

const (
	messageSize         = 2048
	signalingServerPath = "websocket"
)

var _ net.Listener = &Listener{}
var _ net.Conn = &listenerConn{}

// nolint
var (
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	serverDataPeriod = 100 * time.Millisecond // 10 pkt/sec
)

type Listener struct {
	*http.Server
	addr        string
	iceConfig   webrtc.Configuration
	errCh       chan error
	connCh      chan *listenerConn
	api         *webrtc.API
	conns       map[string]*listenerConn
	lock        sync.Mutex
	activeConns int
	logger      logging.LoggerFactory
	log         logging.LeveledLogger
}

func NewListener(addr string, iceConfig webrtc.Configuration, logger logging.LoggerFactory) (*Listener, error) {
	e := webrtc.SettingEngine{}
	e.DetachDataChannels()
	l := &Listener{
		addr:      addr,
		iceConfig: iceConfig,
		api:       webrtc.NewAPI(webrtc.WithSettingEngine(e)),
		errCh:     make(chan error, 5),
		connCh:    make(chan *listenerConn, 128),
		conns:     map[string]*listenerConn{},
		logger:    logger,
		log:       logger.NewLogger("tester-listener"),
	}

	router := mux.NewRouter()
	router.HandleFunc("/"+signalingServerPath, l.ServeHTTP)
	l.Server = &http.Server{Addr: addr, Handler: router}

	c, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("WS tester signaling server on %s: %w", addr, err)
	}

	go func() {
		defer close(l.errCh)
		defer close(l.connCh)

		if err := l.Server.Serve(c); err != nil {
			l.errCh <- err
		}
	}()

	return l, nil
}

func (l *Listener) Accept() (net.Conn, error) {
	l.log.Trace("Accept: waiting for new connection")

	select {
	case err := <-l.errCh:
		return nil, err
	case conn := <-l.connCh:
		l.log.Infof("Accepting connection for WS connection %s-%s",
			conn.wsConn.RemoteAddr(), conn.wsConn.LocalAddr())

		l.lock.Lock()
		l.activeConns += 1
		l.conns[conn.String()] = conn
		l.lock.Unlock()

		return conn, nil
	}
}

func (l *Listener) Close() error {
	l.log.Tracef("Closing tester server listener at address %s", l.addr)
	defer l.Server.Close()

	select {
	case err := <-l.errCh:
		return err
	default:
		return nil
	}
}

func (l *Listener) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// upgrade to webSocket
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	rawWsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		l.errCh <- fmt.Errorf("Failed to upgrade WebSocket connection: %w", err)
		return
	}

	wsConn := &ThreadSafeWriter{Conn: rawWsConn}

	l.log.Tracef("Tester listener received new connection from client %s", wsConn.RemoteAddr())
	connLog := l.logger.NewLogger(fmt.Sprintf("tester-lconn-%s-%s",
		wsConn.RemoteAddr(), wsConn.LocalAddr()))

	wsConn.SetPingHandler(func(string) error {
		return wsConn.WriteMessage(websocket.PongMessage, []byte("keepalive"))
	})

	connLog.Trace("Creating PeerConnection")
	peerConn, err := l.api.NewPeerConnection(l.iceConfig)
	if err != nil {
		l.errCh <- fmt.Errorf("Failed to create a PeerConnection: %w", err)
		return
	}

	connLog.Tracef("Creating DataChannel")
	dataChannel, err := peerConn.CreateDataChannel("data", nil)
	if err != nil {
		l.errCh <- fmt.Errorf("Failed to create DataChannel: %w", err)
		return
	}

	peerConn.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		connLog.Debugf("Connection State has changed: %s", p.String())
		if p == webrtc.PeerConnectionStateFailed || p == webrtc.PeerConnectionStateClosed {
			l.errCh <- errors.New("ICE connection closed prematurely")
			return
		}
	})

	dataChannel.OnOpen(func() {
		connLog.Tracef("Data channel '%s'-'%d' open", dataChannel.Label(), dataChannel.ID())

		raw, dErr := dataChannel.Detach()
		if dErr != nil {
			l.errCh <- fmt.Errorf("Failed to detach DataChannel: %w", err)
			return
		}

		l.log.Infof("Creating new connection for WS connection %s-%s",
			wsConn.RemoteAddr(), wsConn.LocalAddr())

		conn := &listenerConn{
			listener: l,
			wsConn:   wsConn,
			peerConn: peerConn,
			dataChan: dataChannel,
			dataConn: raw,
			log:      connLog,
		}

		l.connCh <- conn

		// redo ICE state change callback to react to the peerconnection going away
		peerConn.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
			conn.Close() //nolint:errcheck
		})
	})

	peerConn.OnICECandidate(func(i *webrtc.ICECandidate) {
		if i == nil {
			return
		}

		// When serializing a candidate use ToJSON, othwerwise json.Marshal will result in
		// errors around `sdpMid`
		candidateString, err := json.Marshal(i.ToJSON())
		if err != nil {
			connLog.Errorf("Failed to marshal candidate to json: %v", err)
			return
		}

		l.log.Debugf("Sending candidate: %s", candidateString)

		if writeErr := wsConn.WriteJSON(&Message{
			Type: MessageTypeIceCandidate,
			Data: string(candidateString),
		}); writeErr != nil {
			connLog.Errorf("Failed to write JSON: %v", writeErr)
		}
	})

	connLog.Trace("Creating Offer for client")
	offer, err := peerConn.CreateOffer(nil)
	if err != nil {
		l.errCh <- fmt.Errorf("Failed to create Offer: %w", err)
		return
	}

	// Note: this will start the gathering of ICE candidates
	connLog.Tracef("Setting local descrition (Offer)")
	if err = peerConn.SetLocalDescription(offer); err != nil {
		l.errCh <- fmt.Errorf("Failed to set local description: %w", err)
		return
	}

	payload, err := json.Marshal(offer)
	if err != nil {
		l.errCh <- fmt.Errorf("Failed to JSON encode Offer: %v", err)
		return
	}

	connLog.Tracef("Sending Offer: %s", offer)
	if writeErr := wsConn.WriteJSON(&Message{
		Type: MessageTypeOffer,
		Data: string(payload),
	}); writeErr != nil {
		l.errCh <- fmt.Errorf("Failed to write Offer: %v", writeErr)
		return
	}

	message := &Message{}
	for {
		_, raw, err := wsConn.ReadMessage()
		if err != nil {
			l.errCh <- fmt.Errorf("Failed to read message: %v", err)
			return
		}

		connLog.Tracef("Got message: %s", raw)

		if err := json.Unmarshal(raw, &message); err != nil {
			l.errCh <- fmt.Errorf("Failed to unmarshal json to message: %v", err)
			return
		}

		switch message.Type {
		case MessageTypeIceCandidate:
			candidate := webrtc.ICECandidateInit{}
			if err := json.Unmarshal([]byte(message.Data), &candidate); err != nil {
				l.errCh <- fmt.Errorf("Failed to unmarshal json to candidate: %v", err)
				return
			}

			connLog.Debugf("Got ICE candidate: %v", candidate)

			if err := peerConn.AddICECandidate(candidate); err != nil {
				l.errCh <- fmt.Errorf("Failed to add ICE candidate: %v", err)
				return
			}

		case MessageTypeAnswer:
			answer := webrtc.SessionDescription{}
			if err := json.Unmarshal([]byte(message.Data), &answer); err != nil {
				l.errCh <- fmt.Errorf("Failed to unmarshal json to answer: %v", err)
				return
			}

			connLog.Debugf("Got Answer: %v", answer)

			connLog.Tracef("Setting remote descrition (Answer)")
			if err := peerConn.SetRemoteDescription(answer); err != nil {
				l.errCh <- fmt.Errorf("Failed to set remote description: %v", err)
				return
			}
		default:
			l.log.Errorf("unknown message: %+v", message)
		}
	}
}

func (_ *Listener) Addr() net.Addr {
	//TODO
	return nil
}

func (l *Listener) Conns() []*listenerConn {
	l.lock.Lock()
	defer l.lock.Unlock()
	ret := []*listenerConn{}
	for _, c := range l.conns {
		ret = append(ret, c)
	}
	return ret
}

type listenerConn struct {
	listener *Listener
	wsConn   *ThreadSafeWriter
	peerConn *webrtc.PeerConnection
	dataChan *webrtc.DataChannel
	dataConn datachannel.ReadWriteCloser
	closed   bool
	log      logging.LeveledLogger
}

func (c *listenerConn) Close() error {
	c.log.Tracef("Closing tester server listener connection %s", c.String())

	if c.closed {
		return nil
	}
	c.closed = true

	// Close the datachannel
	var err error
	if c.dataChan.ReadyState() == webrtc.DataChannelStateOpen {
		if err = c.dataConn.Close(); err != nil {
			c.log.Debugf("Error closing DataChannel for client %s: %s",
				c.wsConn.RemoteAddr().String(), err.Error())
		}
	}

	// Close the peer connection too
	err = c.peerConn.Close()
	if err != nil {
		c.log.Debugf("Error closing PeerConnection for client %s: %s",
			c.wsConn.RemoteAddr().String(), err.Error())
	}

	// Close the websocket, this will exit the peerconnection and the http handler
	err = c.wsConn.Close()
	if err != nil {
		c.log.Debugf("Error closing WS connection for client %s: %s",
			c.wsConn.RemoteAddr().String(), err.Error())
	}

	c.listener.lock.Lock()
	c.listener.activeConns -= 1
	delete(c.listener.conns, c.String())
	c.listener.lock.Unlock()

	// Return the last error
	return err
}

func (c *listenerConn) Read(b []byte) (int, error) {
	return c.dataConn.Read(b)
}

func (c *listenerConn) Write(b []byte) (int, error) {
	return c.dataConn.Write(b)
}

// TODO: implement
func (c *listenerConn) LocalAddr() net.Addr                { return nil }
func (c *listenerConn) RemoteAddr() net.Addr               { return nil }
func (c *listenerConn) SetDeadline(t time.Time) error      { return nil }
func (c *listenerConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *listenerConn) SetWriteDeadline(t time.Time) error { return nil }

// String returns a unique identifier for the connection based on the underlying signaling connection.
func (c *listenerConn) String() string {
	return fmt.Sprintf("%s-%s", c.wsConn.RemoteAddr(), c.wsConn.LocalAddr())
}
