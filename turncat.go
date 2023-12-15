package stunner

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"

	"github.com/pion/dtls/v2"
	"github.com/pion/logging"
	"github.com/pion/turn/v3"

	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

const UDP_PACKET_SIZE = 1500

// AuthGen is a function called by turncat to generate authentication tokens.
type AuthGen func() (string, string, error)

// TurncatConfig is the main configuration for the turncat relay.
type TurncatConfig struct {
	// ListenAddr is the listeninging socket address (local tunnel endpoint).
	ListenerAddr string
	// ServerAddr is the TURN server addrees (e.g. "turn://turn.abc.com:3478").
	ServerAddr string
	// PeerAddr specifies the remote peer to connect to.
	PeerAddr string
	// Realm is the STUN/TURN realm.
	Realm string
	// AuthGet specifies the function to generate auth tokens.
	AuthGen AuthGen
	// InsecureMode controls whether self-signed TLS certificates are accepted by the TURN
	// client.
	InsecureMode bool
	// LoggerFactory is an optional external logger.
	LoggerFactory logging.LoggerFactory
}

// Turncat is the internal structure for representing a turncat relay.
type Turncat struct {
	listenerAddr  net.Addr
	serverAddr    net.Addr
	serverProto   string
	peerAddr      net.Addr
	realm         string
	listenerConn  interface{}            // net.Conn or net.PacketConn
	connTrack     map[string]*connection // Conntrack table.
	lock          *sync.Mutex            // Sync access to the conntrack state.
	authGen       AuthGen                // Generate auth tokens.
	insecure      bool
	loggerFactory logging.LoggerFactory
	log           logging.LeveledLogger
}

type connection struct {
	clientAddr net.Addr       // Address of the client
	turnClient *turn.Client   // TURN client associated with the connection
	clientConn net.Conn       // Socket connected back to the client
	turnConn   net.PacketConn // Socket for the TURN client
	serverConn net.PacketConn // Relayed UDP connection to server
}

// NewTurncat creates a new turncat relay from the specified config, creating a listener socket for
// clients to connect to and relaying client connections through the speficied STUN/TURN server to
// the peer.
func NewTurncat(config *TurncatConfig) (*Turncat, error) {
	loggerFactory := config.LoggerFactory
	if loggerFactory == nil {
		loggerFactory = logging.NewDefaultLoggerFactory()
	}
	log := loggerFactory.NewLogger("turncat")

	log.Tracef("Resolving TURN server address: %s", config.ServerAddr)
	server, sErr := ParseUri(config.ServerAddr)
	if sErr != nil {
		return nil, fmt.Errorf("error resolving server address %s: %w", config.ServerAddr, sErr)
	}
	if server.Address == "" || server.Port == 0 {
		return nil, fmt.Errorf("error resolving TURN server address %s: empty address (\"%s\") "+
			"or invalid port (%d)", config.ServerAddr, server.Address, server.Port)
	}

	log.Tracef("Resolving listener address: %s", config.ListenerAddr)
	// special case the "-" client address
	if config.ListenerAddr == "-" {
		config.ListenerAddr = "file://stdin"
	}
	listener, lErr := url.Parse(config.ListenerAddr)
	if lErr != nil {
		return nil, fmt.Errorf("error parsing listener address %q: %w", config.ListenerAddr, lErr)
	}
	listenerProtocol := strings.ToLower(listener.Scheme)

	log.Tracef("Resolving peer address: %s", config.PeerAddr)
	peer, pErr := url.Parse(config.PeerAddr)
	if pErr != nil {
		return nil, fmt.Errorf("error parsing peer address %q: %w", config.PeerAddr, pErr)
	}
	// default to UDP
	peerAddress, err := net.ResolveUDPAddr("udp", peer.Host)
	if err != nil {
		return nil, fmt.Errorf("error resolving peer address %q: %w", config.PeerAddr, err)
	}

	if config.Realm == "" {
		config.Realm = stnrv1.DefaultRealm
	}

	// a global listener connection for the local tunnel endpoint
	// per-client connections will connect back to the client
	log.Tracef("Setting up listener connection on %s", config.ListenerAddr)
	var listenerConn interface{}
	listenerConf := &net.ListenConfig{Control: reuseAddr}

	var listenerAddress net.Addr
	switch listenerProtocol {
	case "file":
		listenerConn = util.NewFileConn(os.Stdin)
	case "udp", "udp4", "udp6", "unixgram", "ip", "ip4", "ip6":
		addr, err := net.ResolveUDPAddr("udp", listener.Host)
		if err != nil {
			return nil, fmt.Errorf("error resolving listener address %q: %w", config.ListenerAddr, err)
		}

		l, err := listenerConf.ListenPacket(context.Background(), addr.Network(), addr.String())
		if err != nil {
			return nil, fmt.Errorf("cannot create listening client packet socket at %s: %s",
				config.ListenerAddr, err)
		}
		listenerAddress = addr
		listenerConn = l
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
		addr, err := net.ResolveTCPAddr("tcp", listener.Host)
		if err != nil {
			return nil, fmt.Errorf("error resolving listener address %q: %w", config.ListenerAddr, err)
		}

		l, err := listenerConf.Listen(context.Background(), addr.Network(), addr.String())
		if err != nil {
			return nil, fmt.Errorf("cannot create listening client socket at %s: %s",
				config.ListenerAddr, err)
		}
		listenerAddress = addr
		listenerConn = l
	default:
		return nil, fmt.Errorf("unknown client protocol %s", listenerProtocol)
	}

	t := &Turncat{
		listenerAddr:  listenerAddress,
		serverAddr:    server.Addr,
		serverProto:   server.Protocol,
		peerAddr:      peerAddress,
		listenerConn:  listenerConn,
		connTrack:     make(map[string]*connection),
		lock:          new(sync.Mutex),
		realm:         config.Realm,
		authGen:       config.AuthGen,
		insecure:      config.InsecureMode,
		loggerFactory: loggerFactory,
		log:           log,
	}

	switch listenerProtocol {
	case "udp", "udp4", "udp6", "unixgram", "ip", "ip4", "ip6":
		// client connection is a packet conn, write our own Listen/Accept loop for UDP
		// main loop: for every new packet we create a new connection and connect it back to the client
		go t.runListenPacket()
	case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
		// client connection is bytestream, we are supposed to have a Listen/Accept loop available
		go t.runListen()
	case "file":
		// client connection is file
		go t.runListenFile()
	default:
		t.log.Errorf("internal error: unknown client protocol %q for client %s:%s",
			listenerAddress.Network(), listenerAddress.Network(), listenerAddress.String())
	}

	log.Infof("Turncat client listening on %s, TURN server: %s, peer: %s:%s",
		config.ListenerAddr, config.ServerAddr,
		peerAddress.Network(), peerAddress.String())

	return t, nil
}

// Close terminates all relay connections created via turncat and deletes the relay. Errors in this
// phase are not critical and not propagated back to the caller.
func (t *Turncat) Close() {
	t.log.Info("closing Turncat")

	// close all active connections
	for _, conn := range t.connTrack {
		t.deleteConnection(conn)
	}

	// close the global listener socket
	switch t.listenerConn.(type) {
	case net.Listener:
		t.log.Tracef("closing turncat listener connection")
		l := t.listenerConn.(net.Listener)
		if err := l.Close(); err != nil {
			t.log.Warnf("error closing listener connection: %s", err.Error())
		}
	case net.PacketConn:
		t.log.Tracef("closing turncat packet listener connection")
		l := t.listenerConn.(net.PacketConn)
		if err := l.Close(); err != nil {
			t.log.Warnf("error closing listener packet connection: %s", err.Error())
		}
	case *util.FileConn:
		// do nothing
	default:
		t.log.Error("internal error: unknown listener socket type")
	}
}

// Generate a new connection by opening a UDP connection to the server
func (t *Turncat) newConnection(clientConn net.Conn) (*connection, error) {
	clientAddr := clientConn.RemoteAddr()
	t.log.Debugf("new connection from client %s", clientAddr.String())

	conn := new(connection)
	conn.clientAddr = clientAddr
	conn.clientConn = clientConn

	t.log.Tracef("Setting up TURN client to server %s:%s", t.serverAddr.Network(), t.serverAddr.String())

	user, passwd, errAuth := t.authGen()
	if errAuth != nil {
		return nil, fmt.Errorf("cannot generate username/password pair for client %s:%s: %s",
			clientAddr.Network(), clientAddr.String(), errAuth)
	}

	// connection for the TURN client
	var turnConn net.PacketConn
	switch strings.ToLower(t.serverProto) {
	case "turn-udp":
		t, err := net.ListenPacket(t.serverAddr.Network(), "0.0.0.0:0")
		if err != nil {
			return nil, fmt.Errorf("cannot allocate TURN listening packet socket for client %s:%s: %s",
				clientAddr.Network(), clientAddr.String(), err)
		}
		turnConn = t
	case "turn-tcp":
		c, err := net.Dial(t.serverAddr.Network(), t.serverAddr.String())
		if err != nil {
			return nil, fmt.Errorf("cannot allocate TURN socket for client %s:%s: %s",
				clientAddr.Network(), clientAddr.String(), err)
		}
		turnConn = turn.NewSTUNConn(c)
	case "turn-tls":
		// cert, err := tls.LoadX509KeyPair(certFile.Name(), keyFile.Name())
		// assert.NoError(t, err, "cannot create certificate for TLS client socket")
		c, err := tls.Dial("tcp", t.serverAddr.String(), &tls.Config{
			MinVersion:         tls.VersionTLS10,
			InsecureSkipVerify: t.insecure,
		})
		if err != nil {
			return nil, fmt.Errorf("cannot allocate TURN/TLS socket for client %s:%s: %s",
				clientAddr.Network(), clientAddr.String(), err)
		}
		turnConn = turn.NewSTUNConn(c)
	case "turn-dtls":
		// cert, err := tls.LoadX509KeyPair(certFile.Name(), keyFile.Name())
		// assert.NoError(t, err, "cannot create certificate for DTLS client socket")
		udpAddr, _ := net.ResolveUDPAddr("udp", t.serverAddr.String())
		conn, err := dtls.Dial("udp", udpAddr, &dtls.Config{
			InsecureSkipVerify: t.insecure,
		})
		if err != nil {
			return nil, fmt.Errorf("cannot allocate TURN/DTLS socket for client %s:%s: %s",
				clientAddr.Network(), clientAddr.String(), err)
		}
		turnConn = turn.NewSTUNConn(conn)
	default:
		return nil, fmt.Errorf("unknown TURN server protocol %q for client %s:%s",
			t.serverProto, clientAddr.Network(), clientAddr.String())
	}

	turnClient, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: t.serverAddr.String(),
		TURNServerAddr: t.serverAddr.String(),
		Conn:           turnConn,
		Username:       user,
		Password:       passwd,
		Realm:          t.realm,
		LoggerFactory:  t.loggerFactory,
	})
	if err != nil {
		turnConn.Close()
		return nil, fmt.Errorf("cannot allocate TURN client for client %s:%s: %s",
			clientAddr.Network(), clientAddr.String(), err)
	}
	conn.turnConn = turnConn

	// Start the TURN client
	if err = turnClient.Listen(); err != nil {
		turnConn.Close()
		return nil, fmt.Errorf("cannot listen on TURN client: %s", err)

	}
	conn.turnClient = turnClient

	t.log.Tracef("Allocating relay transport for client %s:%s", clientAddr.Network(), clientAddr.String())
	serverConn, serverErr := turnClient.Allocate()
	if serverErr != nil {
		turnClient.Close()
		return nil, fmt.Errorf("could not allocate new TURN relay transport for client %s:%s: %s",
			clientAddr.Network(), clientAddr.String(), serverErr.Error())
	}
	conn.serverConn = serverConn

	// The relayConn's local address is actually the transport
	// address assigned on the TURN server.
	t.log.Infof("new connection: client-address=%s, relayed-address=%s",
		clientAddr.String(), conn.serverConn.LocalAddr().String())

	return conn, nil
}

// don't err, just warn
func (t *Turncat) deleteConnection(conn *connection) {
	caddr := fmt.Sprintf("%s:%s", conn.clientAddr.Network(), conn.clientAddr.String())

	t.lock.Lock()
	_, found := t.connTrack[caddr]
	if !found {
		t.lock.Unlock()
		t.log.Debugf("deleteConnection: cannot find client connection for %s", caddr)
		return
	}
	delete(t.connTrack, caddr)
	t.lock.Unlock()

	t.log.Infof("closing client connection to %s", caddr)

	if err := conn.clientConn.Close(); err != nil {
		t.log.Warnf("error closing client connection for %s:%s: %s",
			conn.clientAddr.Network(), conn.clientAddr.String(), err.Error())
	}
	if err := conn.serverConn.Close(); err != nil {
		t.log.Warnf("error closing relayed TURN server connection for %s:%s: %s",
			conn.clientAddr.Network(), conn.clientAddr.String(), err.Error())
	}

	conn.turnClient.Close()
	conn.turnConn.Close()
}

// any error on read/write will delete the connection and terminate the goroutine
func (t *Turncat) runConnection(conn *connection) {
	// Read from server
	go func() {
		buffer := make([]byte, UDP_PACKET_SIZE)
		for {
			n, peerAddr, readErr := conn.serverConn.ReadFrom(buffer[0:])
			if readErr != nil {
				if !util.IsClosedErr(readErr) {
					t.log.Debugf("cannot read from TURN relay connection for client %s:%s: %s",
						conn.clientAddr.Network(), conn.clientAddr.String(), readErr.Error())
					t.deleteConnection(conn)
				}
				return
			}

			// TODO: not sure if this is the recommended way to compare net.Addrs
			if peerAddr.Network() != t.peerAddr.Network() || peerAddr.String() != t.peerAddr.String() {
				t.log.Debugf("received packet of %d bytes from unknown peer %s:%s (expected: "+
					"%s:%s) on TURN relay connection for client %s:%s: ignoring",
					n, peerAddr.Network(), peerAddr.String(),
					t.peerAddr.Network(), t.peerAddr.String(),
					conn.clientAddr.Network(), conn.clientAddr.String())
				continue
			}

			t.log.Tracef("forwarding packet of %d bytes from peer %s:%s on TURN relay connection "+
				"for client %s:%s", n, peerAddr.Network(), peerAddr.String(),
				conn.clientAddr.Network(), conn.clientAddr.String())

			if _, writeErr := conn.clientConn.Write(buffer[0:n]); writeErr != nil {
				t.log.Debugf("cannot write to client connection for client %s:%s: %s",
					conn.clientAddr.Network(), conn.clientAddr.String(), writeErr.Error())
				t.deleteConnection(conn)
				return
			}
		}
	}()

	// Read from client
	go func() {
		buffer := make([]byte, UDP_PACKET_SIZE)
		for {
			n, readErr := conn.clientConn.Read(buffer[0:])
			if readErr != nil {
				if !util.IsClosedErr(readErr) {
					t.log.Debugf("cannot read from client connection for client %s:%s (likely hamrless): %s",
						conn.clientAddr.Network(), conn.clientAddr.String(), readErr.Error())
					t.deleteConnection(conn)
				}
				return
			}

			t.log.Tracef("forwarding packet of %d bytes from client %s:%s to peer %s:%s on TURN relay connection",
				n, conn.clientAddr.Network(), conn.clientAddr.String(),
				t.peerAddr.Network(), t.peerAddr.String())

			if _, writeErr := conn.serverConn.WriteTo(buffer[0:n], t.peerAddr); writeErr != nil {
				t.log.Debugf("cannot write to TURN relay connection for client %s (likely harmless): %s",
					conn.clientAddr.String(), writeErr.Error())
				t.deleteConnection(conn)
				return
			}
		}
	}()
}

func (t *Turncat) runListenPacket() {
	listenerConn, ok := t.listenerConn.(net.PacketConn)
	if !ok {
		t.log.Error("cannot listen on client connection: expected net.PacketConn")
		// terminate go routine
		return
	}

	buffer := make([]byte, UDP_PACKET_SIZE)
	for {
		n, clientAddr, err := listenerConn.ReadFrom(buffer[0:])
		if err != nil {
			if !util.IsClosedErr(err) {
				t.log.Warnf("cannot read from listener connection: %s", err.Error())
			}
			return
		}

		// handle connection
		t.lock.Lock()
		caddr := fmt.Sprintf("%s:%s", clientAddr.Network(), clientAddr.String())
		trackConn, found := t.connTrack[caddr]
		if !found {
			t.log.Tracef("new client connection: read initial packet of %d bytes on listener"+
				"connnection from client %s", n, caddr)

			// create per-client connection, connect back to client, then call runConnection
			t.log.Tracef("connnecting back to client %s", caddr)
			dialer := &net.Dialer{LocalAddr: t.listenerAddr, Control: reuseAddr}
			clientConn, clientErr := dialer.Dial(clientAddr.Network(), clientAddr.String())
			if clientErr != nil {
				t.log.Warnf("cannot connect back to client %s:%s: %s",
					clientAddr.Network(), clientAddr.String(), clientErr.Error())
				continue
			}

			conn, err := t.newConnection(clientConn)
			if err != nil {
				t.lock.Unlock()
				t.log.Warnf("relay setup failed for client %s: %s", caddr, err.Error())
				continue
			}

			t.connTrack[caddr] = conn
			t.lock.Unlock()

			// Fire up routine to manage new connection
			// terminated once we kill their connection
			t.runConnection(conn)

			// and send the packet out
			if _, err := conn.serverConn.WriteTo(buffer[0:n], t.peerAddr); err != nil {
				t.log.Warnf("cannot write initial packet to TURN relay connection for client %s: %s",
					caddr, err.Error())
				t.deleteConnection(conn)
				continue
			}
		} else {
			// received a packet for an established client connection on the main
			// listener: this can happen if the client is too fast and a couple of
			// packets are left stuck in the global listener socket
			t.lock.Unlock()

			t.log.Debugf("received packet from a known client %s on the global listener connection, sender too fast?",
				caddr)
			// send out anyway
			if _, err := trackConn.serverConn.WriteTo(buffer[0:n], t.peerAddr); err != nil {
				t.log.Warnf("cannot write packet to TURN relay connection for client %s: %s",
					caddr, err.Error())
				t.deleteConnection(trackConn)
				continue
			}
		}

	}
}

func (t *Turncat) runListen() {
	listenerConn, ok := t.listenerConn.(net.Listener)
	if !ok {
		t.log.Error("cannot listen on client connection: expected net.Conn")
		// terminate go routine
		return
	}

	for {
		clientConn, err := listenerConn.Accept()
		if err != nil {
			if !util.IsClosedErr(err) {
				t.log.Warnf("cannot accept() in listener connection: %s", err.Error())
				continue
			} else {
				// terminate go routine
				return
			}
		}

		// handle connection
		t.lock.Lock()
		clientAddr := clientConn.RemoteAddr()
		caddr := fmt.Sprintf("%s:%s", clientAddr.Network(), clientAddr.String())
		_, found := t.connTrack[caddr]
		if !found {
			t.log.Tracef("new client connection: %s", caddr)

			conn, err := t.newConnection(clientConn)
			if err != nil {
				t.lock.Unlock()
				t.log.Warnf("relay setup failed for client %s, dropping client connection",
					caddr)
				continue
			}

			t.connTrack[caddr] = conn
			t.lock.Unlock()

			// Fire up routine to manage new connection
			// terminated once we kill their connection
			t.runConnection(conn)
		} else {
			// received a packet for an established client connection on the main
			// listener: this should never happen
			t.lock.Unlock()

			t.log.Errorf("internal error: received packet from a known client %s on the global listener connection",
				caddr)
		}
	}
}

func (t *Turncat) runListenFile() {
	listenerConn, ok := t.listenerConn.(*util.FileConn)
	if !ok {
		t.log.Error("cannot listen on client connection: expected file")
		// terminate go routine
		return
	}

	// handle connection
	caddr := listenerConn.LocalAddr().String()
	t.log.Tracef("new client connection: %s", caddr)
	t.lock.Lock()
	defer t.lock.Unlock()

	conn, err := t.newConnection(listenerConn)
	if err != nil {
		t.log.Warnf("relay setup failed for client %s: %s", caddr, err.Error())
		return
	}

	t.connTrack[caddr] = conn

	t.runConnection(conn)
}
