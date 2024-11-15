package icetester

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/datachannel"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
)

var (
	// Send pings to the CDS server with this period. Must be less than PongWait.
	PingPeriod = 5 * time.Second

	// Time allowed to read the next pong message from the CDS server.
	PongWait = 8 * time.Second

	// Time allowed to write a message to the CDS server.
	WriteWait = 2 * time.Second

	// Period for retrying failed CDS connections.
	RetryPeriod = 1 * time.Second
)

var _ net.Conn = &dialerConn{}

type Dialer struct {
	iceConfig webrtc.Configuration
	api       *webrtc.API
	logger    logging.LoggerFactory
	log       logging.LeveledLogger
}

func NewDialer(iceConfig webrtc.Configuration, logger logging.LoggerFactory) *Dialer {
	e := webrtc.SettingEngine{}
	e.DetachDataChannels()

	return &Dialer{
		iceConfig: iceConfig,
		api:       webrtc.NewAPI(webrtc.WithSettingEngine(e)),
		logger:    logger,
		log:       logger.NewLogger("tester-dialer"),
	}
}

func (d *Dialer) DialContext(ctx context.Context, addr string) (net.Conn, error) {
	signalingServerURI, err := getURI(addr)
	if err != nil {
		return nil, fmt.Errorf("Failed to parse server address %q: %w", addr, err)
	}
	rawWsConn, _, err := websocket.DefaultDialer.DialContext(ctx, signalingServerURI.String(), makeHeader(signalingServerURI))
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to singlaing server at %q: %w",
			signalingServerURI.String(), err)
	}
	// wrap with a locker to prevent concurrent writes
	wsConn := &ThreadSafeWriter{Conn: rawWsConn}

	conn := &dialerConn{
		wsConn: wsConn,
		log: d.logger.NewLogger(fmt.Sprintf("tester-client-%s-%s",
			wsConn.LocalAddr(), wsConn.RemoteAddr())),
	}

	conn.log.Debugf("Signaling connection successfully opened to tester server at %s", signalingServerURI.String())

	// Start pinger thread: lifetime is the connection's lifetime, stops when connCtx is
	// canceled
	pingTicker := time.NewTicker(PingPeriod)
	pingerCtx, pingerCancel := context.WithCancel(context.Background())
	conn.pingerCancel = pingerCancel
	go func() {
		defer pingTicker.Stop()

		for {
			select {
			case <-pingTicker.C:
				wsConn.SetWriteDeadline(time.Now().Add(WriteWait)) //nolint:errcheck
				if err := wsConn.WriteMessage(websocket.PingMessage, []byte("keepalive")); err != nil {
					conn.log.Errorf("Could not ping tester signaling server at %q: %s",
						wsConn.RemoteAddr(), err.Error())
					return
				}
			case <-pingerCtx.Done():
				conn.log.Tracef("Closing pinger thread WS connection %s-%s",
					wsConn.LocalAddr().String(), wsConn.RemoteAddr().String())
				return
			}
		}
	}()

	conn.log.Tracef("Creating new PeerConnection for WS connection %s-%s",
		wsConn.LocalAddr().String(), wsConn.RemoteAddr().String())
	peerConn, err := d.api.NewPeerConnection(d.iceConfig)
	if err != nil {
		return nil, fmt.Errorf("Failed to create a PeerConnection: %w", err)
	}
	conn.peerConn = peerConn

	// Trickle ICE: emit server candidates to client, errors are not fatal
	peerConn.OnICECandidate(func(i *webrtc.ICECandidate) {
		if i == nil {
			return
		}
		// When serializing a candidate use ToJSON, othwerwise json.Marshal will result in
		// errors around `sdpMid`
		candidateString, err := json.Marshal(i.ToJSON())
		if err != nil {
			conn.log.Errorf("Failed to marshal candidate to json: %v", err)
			return
		}

		conn.log.Infof("Local candidate: %s", candidateString)

		if writeErr := wsConn.WriteJSON(&Message{
			Type: MessageTypeIceCandidate,
			Data: string(candidateString),
		}); writeErr != nil {
			conn.log.Errorf("Failed to write JSON: %v", writeErr)
			return
		}
	})

	// If PeerConnection is closed, close the client
	peerConn.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		conn.log.Infof("Connection State has changed: %s", p)
		if p == webrtc.PeerConnectionStateFailed || p == webrtc.PeerConnectionStateClosed {
			conn.Close() //nolint
		}
	})

	// the next pong must arrive within the PongWait period
	wsConn.SetReadDeadline(time.Now().Add(PongWait)) //nolint:errcheck
	// reinit the deadline when we get a pong
	wsConn.SetPongHandler(func(string) error {
		// a.Tracef("Got PONG from server %q", url)
		wsConn.SetReadDeadline(time.Now().Add(PongWait)) //nolint:errcheck
		return nil
	})

	// Register data channel creation handling
	connCh := make(chan any, 1)
	defer close(connCh)
	errCh := make(chan error)

	peerConn.OnDataChannel(func(dataChannel *webrtc.DataChannel) {
		conn.log.Tracef("New DataChannel %s %d for WS connection %s-%s", dataChannel.Label(),
			dataChannel.ID(), wsConn.LocalAddr().String(), wsConn.RemoteAddr().String())

		// Register channel opening handling
		dataChannel.OnOpen(func() {
			conn.log.Debugf("Data channel '%s'-'%d' open for WS connection %s-%s",
				dataChannel.Label(), dataChannel.ID(),
				wsConn.LocalAddr().String(), wsConn.RemoteAddr().String())

			raw, err := dataChannel.Detach()
			if err != nil {
				errCh <- fmt.Errorf("Failed to detach DataChannel: %w", err)
				return
			}
			conn.dataConn = raw

			connCh <- struct{}{}
		})
	})

	candidateCache := []webrtc.ICECandidateInit{}
	// Start signaling client speaker: lifetime is the connection's lifetime, stops when wsConn
	// is closed
	go func() {
		defer close(errCh)

		message := &Message{}
		for {
			// ping-pong deadline misses will end up being caught here as a read beyond
			// the deadline
			msgType, raw, err := wsConn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}

			// Decoding errors are not fatal
			if msgType != websocket.TextMessage {
				conn.log.Errorf("Unexpected message type (code: %d) from client %q",
					msgType, wsConn.RemoteAddr().String())
				continue
			}

			if err := json.Unmarshal(raw, &message); err != nil {
				conn.log.Errorf("Failed to unmarshal json to message: %v", err)
				continue
			}

			conn.log.Tracef("Got signaling message WS connection %s-%s: %v",
				wsConn.LocalAddr().String(), wsConn.RemoteAddr().String(), message)

			switch message.Type {
			case MessageTypeIceCandidate:
				candidate := webrtc.ICECandidateInit{}
				if err := json.Unmarshal([]byte(message.Data), &candidate); err != nil {
					conn.log.Errorf("Failed to unmarshal json to candidate: %v", err)
					continue
				}

				conn.log.Infof("Remote candidate: %s", message.Data)

				if peerConn.RemoteDescription() == nil {
					// cannot set candidates yet: cache candidate
					candidateCache = append(candidateCache, candidate)
				} else if err := peerConn.AddICECandidate(candidate); err != nil {
					errCh <- fmt.Errorf("Failed to add ICE candidate: %w", err)
				}

			case MessageTypeOffer:
				offer := webrtc.SessionDescription{}
				if err := json.Unmarshal([]byte(message.Data), &offer); err != nil {
					conn.log.Errorf("Failed to unmarshal json to Offer: %v", err)
					continue
				}

				conn.log.Debugf("Got Offer on WS connection %s-%s: %v",
					wsConn.LocalAddr().String(), wsConn.RemoteAddr().String(),
					offer)

				if err := peerConn.SetRemoteDescription(offer); err != nil {
					errCh <- fmt.Errorf("Failed to set remote description: %w", err)
					return
				}

				// flush candidate cache
				for _, candidate := range candidateCache {
					if err := peerConn.AddICECandidate(candidate); err != nil {
						errCh <- fmt.Errorf("Failed to add cached ICE candidate: %w", err)
					}
				}
				candidateCache = []webrtc.ICECandidateInit{}

				// Create an offer to send to the other process
				conn.log.Tracef("Creating Answer WS connection %s-%s",
					wsConn.LocalAddr().String(), wsConn.RemoteAddr().String())
				answer, err := peerConn.CreateAnswer(nil)
				if err != nil {
					errCh <- fmt.Errorf("Failed to create Answer: %w", err)
					return
				}

				// Sets the LocalDescription, and starts our UDP listeners
				// Note: this will start the gathering of ICE candidates
				if err = peerConn.SetLocalDescription(answer); err != nil {
					errCh <- fmt.Errorf("Failed to set local description: %w", err)
					return
				}

				payload, err := json.Marshal(answer)
				if err != nil {
					errCh <- fmt.Errorf("Failed to JSON encode Answer: %v", err)
					return
				}

				conn.log.Debugf("Sending Answer on WS connection %s-%s: %v",
					wsConn.LocalAddr().String(), wsConn.RemoteAddr().String(),
					answer)

				if writeErr := wsConn.WriteJSON(&Message{
					Type: MessageTypeAnswer,
					Data: string(payload),
				}); writeErr != nil {
					errCh <- fmt.Errorf("Failed to write Answer: %v", writeErr)
				}
			default:
				conn.log.Errorf("unknown message: %+v", message)
			}
		}
	}()

	select {
	case <-connCh:
		d.log.Infof("Creating new connection %s", conn.String())
		return conn, nil
	case err := <-errCh:
		conn.Close()
		return nil, err
	}
}

type dialerConn struct {
	pingerCancel context.CancelFunc
	wsConn       *ThreadSafeWriter
	peerConn     *webrtc.PeerConnection
	dataConn     datachannel.ReadWriteCloser
	closed       bool
	log          logging.LeveledLogger
}

func (c *dialerConn) Close() error {
	c.log.Tracef("Closing tester client connection %s", c.String())

	if c.closed {
		return nil
	}
	c.closed = true

	// Close the pinger thread
	c.pingerCancel()

	// Close the WebSocket signaling connection: closes the signaling thread
	c.wsConn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck
	c.wsConn.Close()

	// Close the peerconnection
	if err := c.peerConn.Close(); err != nil {
		return fmt.Errorf("Failed to close PeerConnection: %w", err)
	}

	return nil
}

func (c *dialerConn) Read(b []byte) (int, error) {
	return c.dataConn.Read(b)
}

func (c *dialerConn) Write(b []byte) (int, error) {
	return c.dataConn.Write(b)
}

// TODO: implement
func (c *dialerConn) LocalAddr() net.Addr                { return nil }
func (c *dialerConn) RemoteAddr() net.Addr               { return nil }
func (c *dialerConn) SetDeadline(t time.Time) error      { return nil }
func (c *dialerConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *dialerConn) SetWriteDeadline(t time.Time) error { return nil }

// String returns a unique identifier for the connection based on the underlying signaling connection.
func (c *dialerConn) String() string {
	return fmt.Sprintf("%s-%s", c.wsConn.LocalAddr(), c.wsConn.RemoteAddr())
}

// creates an origin header
func makeHeader(url *url.URL) http.Header {
	header := http.Header{}
	origin := *url
	origin.Scheme = "http"
	origin.Path = ""
	header.Set("origin", origin.String())
	return header
}

func getURI(addr string) (*url.URL, error) {
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") &&
		!strings.HasPrefix(addr, "ws://") && !strings.HasPrefix(addr, "wss://") {
		addr = "ws://" + addr
	}

	url, err := url.Parse(addr)
	if err != nil {
		return nil, err
	}
	url.Path = signalingServerPath
	return url, nil
}
