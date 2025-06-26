// A simple WHIP client and server that implement a stream connection abstraction on top of a WebRTC data channel published via WHIP.
// Client adopted from https://github.com/ggarber/whip-go/
package whipconn

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/pion/datachannel"
	"github.com/pion/logging"
	"github.com/pion/webrtc/v4"
)

var _ net.Conn = &DialerConn{}

type Dialer struct {
	config       Config
	api          *webrtc.API
	logger       logging.LoggerFactory
	log, connLog logging.LeveledLogger
}

func NewDialer(config Config, logger logging.LoggerFactory) *Dialer {
	e := webrtc.SettingEngine{LoggerFactory: logger}
	e.DetachDataChannels()

	if config.WHIPEndpoint == "" {
		config.WHIPEndpoint = WhipEndpoint
	}

	return &Dialer{
		config:  config,
		api:     webrtc.NewAPI(webrtc.WithSettingEngine(e), webrtc.WithMediaEngine(&webrtc.MediaEngine{})),
		logger:  logger,
		log:     logger.NewLogger("whip-dialer"),
		connLog: logger.NewLogger("whip-conn"),
	}
}

func (d *Dialer) WithSettingEngine(e webrtc.SettingEngine) *Dialer {
	e.DetachDataChannels() // make sure this is set
	d.api = webrtc.NewAPI(webrtc.WithSettingEngine(e), webrtc.WithMediaEngine(&webrtc.MediaEngine{}))
	return d
}

func (d *Dialer) DialContext(ctx context.Context, addr string) (net.Conn, error) {
	stopped := false
	var connCh chan any
	var errCh chan error
	defer func() {
		stopped = true
		if connCh != nil {
			close(connCh)
		}
		if errCh != nil {
			close(errCh)
		}
	}()

	peerConn, err := d.api.NewPeerConnection(webrtc.Configuration{
		ICEServers:         d.config.ICEServers,
		ICETransportPolicy: d.config.ICETransportPolicy,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create PeerConnection: %w", err)
	}

	d.log.Trace("Creating DataChannel")
	dataChannel, err := peerConn.CreateDataChannel("whipconn", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create DataChannel: %w", err)
	}

	conn := &DialerConn{
		dialer:   d,
		addr:     addr,
		peerConn: peerConn,
		log:      d.connLog,
	}

	connCh = make(chan any, 1)
	errCh = make(chan error)

	// Register channel opening handling
	dataChannel.OnOpen(func() {
		conn.log.Debugf("Creating new connection in data channel %s-%d",
			dataChannel.Label(), dataChannel.ID())

		raw, err := dataChannel.Detach()
		if err != nil {
			errCh <- fmt.Errorf("failed to detach DataChannel: %w", err)
		}
		conn.dataConn = raw

		connCh <- struct{}{}
	})

	// If PeerConnection is closed, close the client
	peerConn.OnConnectionStateChange(func(p webrtc.PeerConnectionState) {
		conn.log.Infof("Connection state has changed: %s", p)
		if p == webrtc.PeerConnectionStateFailed || p == webrtc.PeerConnectionStateClosed {
			if stopped {
				conn.Close() //nolint
			} else {
				errCh <- fmt.Errorf("ICE connection terminated with state: %s", p.String())
			}
		}
	})

	offer, err := peerConn.CreateOffer(nil)
	if err != nil {
		conn.Close() //nolint
		return nil, fmt.Errorf("failed to create offer: %w", err)
	}

	err = peerConn.SetLocalDescription(offer)
	if err != nil {
		conn.Close() //nolint
		return nil, fmt.Errorf("failed to set local SDP (Offer): %w", err)
	}

	// Block until ICE Gathering is complete, disabling trickle ICE we do this because we only
	// can exchange one signaling message
	gatherComplete := webrtc.GatheringCompletePromise(peerConn)
	<-gatherComplete

	d.log.Debugf("ICE gathering complete: %s", peerConn.LocalDescription().SDP)

	sdp := []byte(peerConn.LocalDescription().SDP)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		makeURL(addr, d.config.WHIPEndpoint).String(), bytes.NewBuffer(sdp))
	if err != nil {
		conn.Close() //nolint
		return nil, fmt.Errorf("unexpected error building HTTP request: %w", err)
	}

	req.Header.Add("Content-Type", "application/sdp")
	if d.config.BearerToken != "" {
		req.Header.Add("Authorization", "Bearer "+d.config.BearerToken)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		conn.Close() //nolint
		return nil, fmt.Errorf("failed to POST WHIP request: %w", err)
	}

	d.log.Tracef("Received POST response with status code: %d", resp.StatusCode)

	if resp.StatusCode != 201 {
		conn.Close() //nolint
		return nil, fmt.Errorf("POST request returned invalid status: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		conn.Close() //nolint
		return nil, fmt.Errorf("failed to read HTTP response body: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	resourceId := resp.Header.Get("Location")
	if resourceId == "" {
		conn.Close() //nolint
		return nil, errors.New("empty resource id in POST response")
	}
	conn.resourceId = resourceId

	answer := webrtc.SessionDescription{}
	answer.Type = webrtc.SDPTypeAnswer
	answer.SDP = string(body)

	err = peerConn.SetRemoteDescription(answer)
	if err != nil {
		conn.Close() //nolint
		return nil, fmt.Errorf("failed to set remote SDP (Answer): %w", err)
	}

	// Waiting for the connection or errors surfaced from the callbacks
	select {
	case <-connCh:
		d.log.Infof("Creating new connection %s", conn.String())
		return conn, nil
	case err := <-errCh:
		conn.Close() //nolint:errcheck
		return nil, err
	case <-ctx.Done():
		conn.Close() //nolint:errcheck
		return nil, ctx.Err()
	}
}

type DialerConn struct {
	dialer     *Dialer
	addr       string
	peerConn   *webrtc.PeerConnection
	dataConn   datachannel.ReadWriteCloser
	resourceId string
	closed     bool
	log        logging.LeveledLogger
}

func (c *DialerConn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true

	c.log.Trace("Closing WHIP client connection")

	uri := makeURL(c.addr, makeResourceURL(c.dialer.config.WHIPEndpoint, c.resourceId))
	req, err := http.NewRequest("DELETE", uri.String(), nil)
	if err != nil {
		return fmt.Errorf("unexpected error building http request: %w", err)
	}
	if c.dialer.config.BearerToken != "" {
		req.Header.Add("Authorization", "Bearer "+c.dialer.config.BearerToken)
	}

	if _, err = http.DefaultClient.Do(req); err != nil {
		return fmt.Errorf("failed WHIP DELETE request: %w", err)
	}

	// Close the peerconnection
	if err := c.peerConn.Close(); err != nil {
		return fmt.Errorf("failed to close PeerConnection: %w", err)
	}

	return nil
}

func (c *DialerConn) Read(b []byte) (int, error) {
	return c.dataConn.Read(b)
}

func (c *DialerConn) Write(b []byte) (int, error) {
	return c.dataConn.Write(b)
}

// TODO: implement
func (c *DialerConn) LocalAddr() net.Addr                { return nil }
func (c *DialerConn) RemoteAddr() net.Addr               { return nil }
func (c *DialerConn) SetDeadline(t time.Time) error      { return nil }
func (c *DialerConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *DialerConn) SetWriteDeadline(t time.Time) error { return nil }

// String returns a unique identifier for the connection based on the underlying signaling connection.
func (c *DialerConn) String() string {
	return c.resourceId
}

// GetPeerConnection returns the PeerConnection underlying the data channnel. Useful for checking ICE candidate status.
func (c *DialerConn) GetPeerConnection() *webrtc.PeerConnection {
	return c.peerConn
}
