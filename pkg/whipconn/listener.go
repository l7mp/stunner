// A simple WHIP client and server that implement a stream connection abstraction on top of a WebRTC data channel published via WHIP.
// Adopted from https://github.com/pion/webrtc/tree/master/examples/whip-whep
package whipconn

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
)

var _ net.Listener = &Listener{}
var _ net.Conn = &ListenerConn{}

type Listener struct {
	api          *webrtc.API
	config       Config
	addr         string
	server       *http.Server
	errCh        chan error
	connCh       chan *ListenerConn
	conns        map[string]*ListenerConn
	lock         sync.Mutex
	logger       logging.LoggerFactory
	log, connLog logging.LeveledLogger
	closed       bool
}

func NewListener(addr string, config Config, logger logging.LoggerFactory) (*Listener, error) {
	e := webrtc.SettingEngine{}
	e.DetachDataChannels()

	if config.WHIPEndpoint == "" {
		config.WHIPEndpoint = WhipEndpoint
	}

	l := &Listener{
		addr:    addr,
		config:  config,
		api:     webrtc.NewAPI(webrtc.WithSettingEngine(e), webrtc.WithMediaEngine(&webrtc.MediaEngine{})),
		errCh:   make(chan error, 5),
		connCh:  make(chan *ListenerConn, 128),
		conns:   map[string]*ListenerConn{},
		logger:  logger,
		log:     logger.NewLogger("whip-listener"),
		connLog: logger.NewLogger("whip-conn"),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /config", l.configGetHandler)
	mux.HandleFunc("POST /config", l.configPostHandler)

	deletePatternWithoutSlash := fmt.Sprintf("DELETE %s/{resourceId}", config.WHIPEndpoint)
	mux.HandleFunc(deletePatternWithoutSlash, l.whipDeleteHandler)
	deletePatternWithSlash := fmt.Sprintf("DELETE %s/{resourceId}/{$}", config.WHIPEndpoint)
	mux.HandleFunc(deletePatternWithSlash, l.whipDeleteHandler)

	requestPatternWithoutSlash := fmt.Sprintf("POST %s", config.WHIPEndpoint)
	mux.HandleFunc(requestPatternWithoutSlash, l.whipRequestHandler)
	requestPatternWithSlash := fmt.Sprintf("POST %s/{$}", config.WHIPEndpoint)
	mux.HandleFunc(requestPatternWithSlash, l.whipRequestHandler)

	c, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("failed to open WHIP server socket on %s: %w", addr, err)
	}
	l.server = &http.Server{Addr: addr, Handler: mux}
	go func() {
		defer close(l.errCh)
		defer close(l.connCh)

		if err := l.server.Serve(c); err != nil {
			l.errCh <- err
		}
	}()

	return l, nil
}

func (l *Listener) Accept() (net.Conn, error) {
	l.log.Trace("Accept: waiting for new connection")

	select {
	case err := <-l.errCh:
		l.log.Tracef("Accept error: %s", err.Error())
		return nil, err
	case conn := <-l.connCh:
		l.log.Info("Accept: New connection")

		l.lock.Lock()
		l.conns[conn.String()] = conn
		l.lock.Unlock()

		return conn, nil
	}
}

func (l *Listener) Close() error {
	if l.closed {
		return nil
	}
	l.closed = true

	l.log.Tracef("Closing WHIP server listener at address %s", l.addr)

	// Send an error to stop any Accept() calls running
	l.errCh <- net.ErrClosed

	return l.server.Close()
}

func (l *Listener) configGetHandler(w http.ResponseWriter, r *http.Request) {
	l.log.Infof("New Config GET request from client %s", r.RemoteAddr)

	if r.Header.Get("Content-Type") != "application/json" {
		err := fmt.Errorf("Expected Content-Type:application/json, got %q", r.Header.Get("Content-Type"))
		l.log.Error(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	if err := json.NewEncoder(w).Encode(l.config); err != nil {
		l.log.Errorf("Failed to encode config %#v for client %s: %s",
			l.config, r.RemoteAddr, err.Error())
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// Note: is is unsafe to update the whip endpoint without restarting the listener
func (l *Listener) configPostHandler(w http.ResponseWriter, r *http.Request) {
	l.log.Infof("New Config POST request from client %s", r.RemoteAddr)

	if r.Header.Get("Content-Type") != "application/json" {
		err := fmt.Errorf("Expected Content-Type:application/json, got %q", r.Header.Get("Content-Type"))
		l.log.Error(err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var config Config
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		l.log.Errorf("Failed to decode config request %#v from client %s: %s",
			config, r.RemoteAddr, err.Error())
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	l.config.ICEServers = config.ICEServers
	if config.ICETransportPolicy != l.config.ICETransportPolicy {
		l.config.ICETransportPolicy = config.ICETransportPolicy
	}
	if config.BearerToken != "" {
		l.config.BearerToken = config.BearerToken
	}
	if config.WHIPEndpoint != "" {
		l.log.Debugf("Ignoring WHIP endpoint in received config: %s", config.WHIPEndpoint)
	}

	l.log.Infof("Using new config: %#v", l.config)
}

func (l *Listener) whipRequestHandler(w http.ResponseWriter, r *http.Request) {
	l.log.Infof("New WHIP POST request from client %s", r.RemoteAddr)

	// Check bearer token
	if l.config.BearerToken != "" {
		if token := r.Header.Get("Authorization"); token != "Bearer "+l.config.BearerToken {
			err := fmt.Errorf("Unauthorized WHIP request from client %s", r.RemoteAddr)
			l.errCh <- err
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
	}

	if ctype := r.Header.Get("Content-Type"); ctype != "application/sdp" {
		err := fmt.Errorf("invalid WHIP request from client %s, expected Content-Type=application/sdp",
			r.RemoteAddr)
		l.errCh <- err
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Read the offer from HTTP Request
	offer, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		l.errCh <- fmt.Errorf("failed to read WHIP request body: %w", err)
		return
	}

	conn := &ListenerConn{
		listener: l,
		log:      l.connLog,
	}

	conn.log.Tracef("Creating PeerConnection for client %s", r.RemoteAddr)
	peerConn, err := l.api.NewPeerConnection(webrtc.Configuration{
		ICEServers:         l.config.ICEServers,
		ICETransportPolicy: l.config.ICETransportPolicy,
	})
	if err != nil {
		l.errCh <- fmt.Errorf("failed to create a PeerConnection: %w", err)
		return
	}
	conn.PeerConn = peerConn

	peerConn.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		conn.log.Debugf("PeerConnection state for client %s has changed: %s", r.RemoteAddr, p.String())
		if p == webrtc.PeerConnectionStateFailed || p == webrtc.PeerConnectionStateClosed {
			conn.Close() // nolint:errcheck
			return
		}
	})

	peerConn.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
		conn.log.Tracef("New data channel %s-%d", dataChannel.Label(), dataChannel.ID())

		dataChannel.OnOpen(func() {
			conn.log.Tracef("Data channel %s-%d open for client %s", dataChannel.Label(),
				dataChannel.ID(), r.RemoteAddr)
			conn.dataChan = dataChannel

			raw, dErr := dataChannel.Detach()
			if dErr != nil {
				l.errCh <- fmt.Errorf("failed to detach DataChannel: %w", err)
				return
			}
			conn.DataConn = raw
			conn.started = true

			l.log.Infof("Creating new connection for client %s", r.RemoteAddr)
			l.connCh <- conn
		})
	})

	conn.log.Tracef("Set remote SDP (Offer) for client %s", r.RemoteAddr)
	if err := peerConn.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  string(offer),
	}); err != nil {
		l.errCh <- fmt.Errorf("failed to set remote SDP (Offer): %w", err)
		return
	}

	// Create channel that is blocked until ICE Gathering is complete
	gatherComplete := webrtc.GatheringCompletePromise(peerConn)

	// Create answer
	answer, err := peerConn.CreateAnswer(nil)
	if err != nil {
		l.errCh <- fmt.Errorf("failed to create SDP (Answer): %w", err)
		return
	} else if err = peerConn.SetLocalDescription(answer); err != nil {
		l.errCh <- fmt.Errorf("failed to set local SDP (Answer): %w", err)
		return
	}

	// Block until ICE Gathering is complete, disabling trickle ICE
	// we do this because we only can exchange one signaling message
	// in a production application you should exchange ICE Candidates via OnICECandidate
	<-gatherComplete

	sdp := peerConn.LocalDescription().SDP
	l.log.Debugf("ICE gathering complete: %s", sdp)

	// WHIP expects a Location header: the hash of our local SDP
	resourceId := resourceHash(sdp)
	conn.ResourceUrl = makeResourceURL(l.config.WHIPEndpoint, resourceId)
	w.Header().Add("Location", resourceId)

	// WHIP+WHEP expects a HTTP Status Code of 201
	w.WriteHeader(http.StatusCreated)

	// Write Answer with Candidates as HTTP Response
	fmt.Fprint(w, peerConn.LocalDescription().SDP) //nolint: errcheck
}

func (l *Listener) whipDeleteHandler(w http.ResponseWriter, r *http.Request) {
	l.log.Infof("New WHIP DELETE request from client %s for resource id %q",
		r.RemoteAddr, r.PathValue("resourceId"))

	resourceId := r.PathValue("resourceId")
	if resourceId == "" {
		http.Error(w, "Empty resource id", http.StatusBadRequest)
		return
	}

	l.lock.Lock()
	conn, ok := l.conns[resourceId]
	l.lock.Unlock()

	if !ok {
		http.Error(w, "Unknown resource id", http.StatusNotFound)
		return
	}

	l.log.Infof("Deleting connection with resource id %q", resourceId)

	if err := conn.Close(); err != nil {
		http.Error(w, fmt.Sprintf("Failed to close connection: %s", err.Error()), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, fmt.Sprintf("Resource %s deleted", resourceId)) //nolint
}

func (_ *Listener) Addr() net.Addr {
	return nil
}

func (l *Listener) GetConns() []*ListenerConn {
	l.lock.Lock()
	defer l.lock.Unlock()
	ret := []*ListenerConn{}
	for _, c := range l.conns {
		ret = append(ret, c)
	}
	return ret
}

type ListenerConn struct {
	ResourceUrl     string
	listener        *Listener
	PeerConn        *webrtc.PeerConnection
	dataChan        *webrtc.DataChannel
	DataConn        datachannel.ReadWriteCloser
	started, closed bool
	log             logging.LeveledLogger
}

func (c *ListenerConn) Close() error {
	c.log.Tracef("Closing WHIP listener connection %s", c.String())

	if c.closed {
		return nil
	}
	c.closed = true

	// Close the datachannel
	var err error
	if c.dataChan != nil && c.dataChan.ReadyState() == webrtc.DataChannelStateOpen {
		if err = c.DataConn.Close(); err != nil {
			c.log.Debugf("Error closing DataChannel: %s", err.Error())
		}
	}

	// Close the peer connection
	err = c.PeerConn.Close()
	if err != nil {
		c.log.Debugf("Error closing PeerConnection: %s", err.Error())
	}

	if c.started {
		c.listener.lock.Lock()
		delete(c.listener.conns, c.String())
		c.listener.lock.Unlock()
	}

	// Return the last error
	return err
}

func (c *ListenerConn) Read(b []byte) (int, error) {
	return c.DataConn.Read(b)
}

func (c *ListenerConn) Write(b []byte) (int, error) {
	return c.DataConn.Write(b)
}

// TODO: implement
func (c *ListenerConn) LocalAddr() net.Addr                { return nil }
func (c *ListenerConn) RemoteAddr() net.Addr               { return nil }
func (c *ListenerConn) SetDeadline(t time.Time) error      { return nil }
func (c *ListenerConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *ListenerConn) SetWriteDeadline(t time.Time) error { return nil }

// String returns a unique identifier for the connection based on the underlying signaling connection.
func (c *ListenerConn) String() string {
	return c.ResourceUrl
}
