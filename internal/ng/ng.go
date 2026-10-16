// Package ng implements rtpengine's ng control protocol, in bencode or JSON.
package ng

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Commands.
const (
	CommandPing   = "ping"
	CommandOffer  = "offer"
	CommandAnswer = "answer"
	CommandDelete = "delete"
	CommandQuery  = "query"
)

// Results.
const (
	ResultOK    = "ok"
	ResultPong  = "pong"
	ResultError = "error"
)

// Direction names: the side of the media proxy a party of the call is on.
const (
	DirectionExternal = "external"
	DirectionInternal = "internal"
)

// RetransmitInterval is how long a client waits for a reply before sending a request again.
var RetransmitInterval = 500 * time.Millisecond

// maxMessageSize is the largest ng datagram.
const maxMessageSize = 65535

// replyCacheLen is the number of replies a server keeps to answer retransmitted requests.
const replyCacheLen = 64

var errMalformed = errors.New("malformed ng message")

// Encoding is the encoding of the dictionary of an ng message.
type Encoding int

const (
	// Bencode is rtpengine's native encoding, the compact one for datagrams.
	Bencode Encoding = iota
	// JSON is the encoding of the in-process media proxy.
	JSON
)

// Message is an ng dictionary.
type Message map[string]any

// Request is an ng command, with the keys a SIP proxy sets.
type Request struct {
	Command string
	CallID  string
	FromTag string
	ToTag   string
	SDP     string
	// Direction names the side of the offerer and of the answerer, DirectionExternal or
	// DirectionInternal.
	Direction []string
	// ICE is "remove" to strip the ICE attributes.
	ICE string
	// Replace lists what to rewrite beside the media addresses, such as "origin".
	Replace []string
	// RTCPMux lists the rtcp-mux handling, such as "offer" or "demux".
	RTCPMux []string
	// Flags lists the plain flags.
	Flags []string
}

// Response is the reply to an ng command.
type Response struct {
	Result      string
	SDP         string
	ErrorReason string
	Warning     string
	// Fields holds the further keys of the reply, such as the statistics of a query.
	Fields Message
}

// Message returns the ng dictionary of a request.
func (r *Request) Message() Message {
	m := Message{"command": r.Command}
	for k, v := range map[string]string{"call-id": r.CallID, "from-tag": r.FromTag,
		"to-tag": r.ToTag, "sdp": r.SDP, "ICE": r.ICE} {
		if v != "" {
			m[k] = v
		}
	}
	for k, v := range map[string][]string{"direction": r.Direction, "replace": r.Replace,
		"rtcp-mux": r.RTCPMux, "flags": r.Flags} {
		if len(v) != 0 {
			m[k] = v
		}
	}
	return m
}

// ParseRequest reads a request from an ng dictionary.
func ParseRequest(m Message) (*Request, error) {
	r := &Request{}
	for k, dst := range map[string]*string{"command": &r.Command, "call-id": &r.CallID,
		"from-tag": &r.FromTag, "to-tag": &r.ToTag, "sdp": &r.SDP, "ICE": &r.ICE} {
		if v, ok := m[k]; ok {
			s, ok := v.(string)
			if !ok {
				return nil, fmt.Errorf("%w: %q is not a string", errMalformed, k)
			}
			*dst = s
		}
	}
	for k, dst := range map[string]*[]string{"direction": &r.Direction, "replace": &r.Replace,
		"rtcp-mux": &r.RTCPMux, "flags": &r.Flags} {
		v, ok := m[k]
		if !ok {
			continue
		}
		l, ok := v.([]any)
		if !ok {
			return nil, fmt.Errorf("%w: %q is not a list", errMalformed, k)
		}
		for _, e := range l {
			s, ok := e.(string)
			if !ok {
				return nil, fmt.Errorf("%w: %q lists a non-string", errMalformed, k)
			}
			*dst = append(*dst, s)
		}
	}
	if r.Command == "" {
		return nil, fmt.Errorf("%w: no command", errMalformed)
	}
	return r, nil
}

// Message returns the ng dictionary of a response.
func (r *Response) Message() Message {
	m := Message{}
	for k, v := range r.Fields {
		m[k] = v
	}
	m["result"] = r.Result
	for k, v := range map[string]string{"sdp": r.SDP, "error-reason": r.ErrorReason,
		"warning": r.Warning} {
		if v != "" {
			m[k] = v
		}
	}
	return m
}

// ParseResponse reads a response from an ng dictionary.
func ParseResponse(m Message) (*Response, error) {
	r := &Response{Fields: Message{}}
	for k, v := range m {
		s, isStr := v.(string)
		switch k {
		case "result":
			r.Result = s
		case "sdp":
			r.SDP = s
		case "error-reason":
			r.ErrorReason = s
		case "warning":
			r.Warning = s
		default:
			r.Fields[k] = v
			continue
		}
		if !isStr {
			return nil, fmt.Errorf("%w: %q is not a string", errMalformed, k)
		}
	}
	if r.Result == "" {
		return nil, fmt.Errorf("%w: no result", errMalformed)
	}
	return r, nil
}

// Errorf makes an error response.
func Errorf(format string, args ...any) *Response {
	return &Response{Result: ResultError, ErrorReason: fmt.Sprintf(format, args...)}
}

// marshal makes the datagram of a dictionary.
func marshal(cookie string, m Message, enc Encoding) ([]byte, error) {
	var body []byte
	var err error
	if enc == JSON {
		body, err = json.Marshal(m)
	} else {
		body, err = Encode(m)
	}
	if err != nil {
		return nil, err
	}
	return append([]byte(cookie+" "), body...), nil
}

// unmarshal splits a datagram into its cookie and dictionary, and tells its encoding.
func unmarshal(b []byte) (string, Message, Encoding, error) {
	sp := bytes.IndexByte(b, ' ')
	if sp <= 0 {
		return "", nil, Bencode, fmt.Errorf("%w: no cookie", errMalformed)
	}
	cookie, body := string(b[:sp]), b[sp+1:]
	if len(body) > 0 && body[0] == '{' {
		d := json.NewDecoder(bytes.NewReader(body))
		d.UseNumber()
		var m map[string]any
		if err := d.Decode(&m); err != nil {
			return "", nil, JSON, fmt.Errorf("%w: %s", errMalformed, err.Error())
		}
		return cookie, fromJSON(m).(Message), JSON, nil
	}
	v, err := Decode(body)
	if err != nil {
		return "", nil, Bencode, err
	}
	m, ok := v.(Message)
	if !ok {
		return "", nil, Bencode, fmt.Errorf("%w: not a dictionary", errMalformed)
	}
	return cookie, m, Bencode, nil
}

// fromJSON gives a decoded JSON value the types Decode gives: integers as int64, objects as
// Message.
func fromJSON(v any) any {
	switch v := v.(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return n
		}
		return v.String()
	case map[string]any:
		m := Message{}
		for k, e := range v {
			m[k] = fromJSON(e)
		}
		return m
	case []any:
		for i := range v {
			v[i] = fromJSON(v[i])
		}
		return v
	}
	return v
}

// Client sends ng commands over a datagram conn, such as a UDP or a fifo conn.
type Client struct {
	conn    net.Conn
	enc     Encoding
	prefix  string
	seq     atomic.Uint64
	mu      sync.Mutex
	pending map[string]chan Message
	done    chan struct{}
	err     error
}

// NewClient creates a client sending in an encoding over a conn and starts reading the replies.
func NewClient(conn net.Conn, enc Encoding) *Client {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	c := &Client{conn: conn, enc: enc, prefix: hex.EncodeToString(b), pending: map[string]chan Message{},
		done: make(chan struct{})}
	go c.read()
	return c
}

func (c *Client) read() {
	buf := make([]byte, maxMessageSize)
	for {
		n, err := c.conn.Read(buf)
		if err != nil {
			c.mu.Lock()
			c.err = err
			c.mu.Unlock()
			close(c.done)
			return
		}
		cookie, m, _, err := unmarshal(buf[:n])
		if err != nil {
			continue
		}
		c.mu.Lock()
		ch, ok := c.pending[cookie]
		delete(c.pending, cookie)
		c.mu.Unlock()
		if ok {
			ch <- m
		}
	}
}

// Do sends a request and waits for its reply, sending it again every RetransmitInterval. An
// error reply is returned as an error.
func (c *Client) Do(ctx context.Context, req *Request) (*Response, error) {
	cookie := fmt.Sprintf("%s_%d", c.prefix, c.seq.Add(1))
	msg, err := marshal(cookie, req.Message(), c.enc)
	if err != nil {
		return nil, err
	}
	ch := make(chan Message, 1)
	c.mu.Lock()
	c.pending[cookie] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.pending, cookie)
		c.mu.Unlock()
	}()

	t := time.NewTicker(RetransmitInterval)
	defer t.Stop()
	for {
		if _, err := c.conn.Write(msg); err != nil {
			return nil, err
		}
		select {
		case m := <-ch:
			resp, err := ParseResponse(m)
			if err != nil {
				return nil, err
			}
			if resp.Result == ResultError {
				return resp, fmt.Errorf("ng %s: %s", req.Command, resp.ErrorReason)
			}
			return resp, nil
		case <-t.C:
		case <-c.done:
			c.mu.Lock()
			err := c.err
			c.mu.Unlock()
			return nil, err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// Done is closed once the conn of the client fails.
func (c *Client) Done() <-chan struct{} { return c.done }

// Close closes the conn of the client.
func (c *Client) Close() error { return c.conn.Close() }

// Serve answers the ng commands of a datagram conn with a handler, in each request's encoding,
// until the conn fails; a retransmission gets the cached reply.
func Serve(conn net.Conn, handler func(*Request) *Response) error {
	buf := make([]byte, maxMessageSize)
	replies := map[string][]byte{}
	order := []string{}
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return err
		}
		cookie, m, enc, err := unmarshal(buf[:n])
		if err != nil {
			continue
		}
		reply, ok := replies[cookie]
		if !ok {
			var resp *Response
			if req, err := ParseRequest(m); err != nil {
				resp = Errorf("%s", err.Error())
			} else {
				resp = handler(req)
			}
			if reply, err = marshal(cookie, resp.Message(), enc); err != nil {
				reply, _ = marshal(cookie, Errorf("%s", err.Error()).Message(), enc)
			}
			replies[cookie], order = reply, append(order, cookie)
			if len(order) > replyCacheLen {
				delete(replies, order[0])
				order = order[1:]
			}
		}
		if _, err := conn.Write(reply); err != nil {
			return err
		}
	}
}
