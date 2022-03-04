// creates a tunnel through a TURN server
// turncat --client=127.0.0.1:5000 --server=127.0.0.1:3478 --peer=127.0.0.1:5001 --users test=test
// go run main.go --user test=test --verbose 127.0.0.1:5000 34.118.22.69:3478 10.116.0.10:11111

package main

import (
	"flag"
	"net"
	"fmt"
	"context"
	"strings"
	"syscall"
	"os"
	"os/signal"
	"sync"
	"errors"

	// "bytes"
	// "reflect"
	
	"github.com/pion/logging"
	"github.com/pion/turn/v2"
)

const UDP_PACKET_SIZE = 1500

type packet struct {
	clientAddr net.Addr       // Address of the client
	buffer *[]byte            // packet content
}

type connection struct {
	clientAddr net.Addr       // Address of the client
	turnClient *turn.Client   // TURN client associated with connection
	clientConn net.Conn       // UDP connection connected back to the client
	serverConn net.PacketConn // Relayed UDP connection to server
}

type TurncatConfig struct {
	ListenAddr     string			// Listening socket address (local tunnel endpoint).
	ServerAddr     string			// TURN server addrees (e.g. "turn:turn.abc.com:3478").
	PeerAddr       string			// The remote peer to connec to.
	Username       string			// Username.
	Password       string			// Password.
	Realm          string			// Realm.
	LoggerFactory  logging.LoggerFactory	// Logger.
}

type Turncat struct {
	listenAddr    net.Addr
	serverAddr    net.Addr
	peerAddr      net.Addr
	listenConn    net.PacketConn
	connTrack     map[string]*connection  // conntrack table
	lock          *sync.Mutex             // sync access to the conntrack state
	username      string
	password      string
	realm         string
	loggerFactory logging.LoggerFactory	// Logger.
}

//////////
func resolveAddr(addr string) (net.Addr, error) {
	netaddr := strings.SplitN(addr, ":", 2)

	if len(netaddr) != 2 {
		return nil, fmt.Errorf("invalid address '%s', expecting <type>:<addr>[:<port>]", addr)
	}

	// default to IPv4
	switch netaddr[0] {
	case "udp", "turn":
		a, err := net.ResolveUDPAddr("udp", netaddr[1])
		if err != nil {
			return nil, fmt.Errorf("cannot resolve UDP address '%s', expecting <type>:<addr>[:<port>]: %s",
				addr, err)
		}
		return a, nil
	case "tcp":
		a, err := net.ResolveTCPAddr("tcp", netaddr[1])
		if err != nil {
			return nil, fmt.Errorf("cannot resolve TCP address '%s', expecting <type>:<addr>[:<port>]: %s",
				addr, err)
		}
		return a, nil
	case "ip":
		a, err := net.ResolveIPAddr("ip", netaddr[1])
		if err != nil {
			return nil, fmt.Errorf("cannot resolve IP address '%s', expecting <type>:<addr>[:<port>]: %s",
				addr, err)
		}
		return a, nil
	default:
		return nil, fmt.Errorf("invalid address '%s', expecting <type>:<addr>[:<port>]",
			addr)
	}
	
	panic("cannot happen")
}

func reuseAddr(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(descriptor uintptr) {
		syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		// syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
	})
}

///////
func NewLogger(levelSpec string) *logging.DefaultLoggerFactory{
	logger := logging.NewDefaultLoggerFactory()
	logger.ScopeLevels["turncat"] = logging.LogLevelError

	logLevels := map[string]logging.LogLevel{
		"DISABLE": logging.LogLevelDisabled,
		"ERROR":   logging.LogLevelError,
		"WARN":    logging.LogLevelWarn,
		"INFO":    logging.LogLevelInfo,
		"DEBUG":   logging.LogLevelDebug,
		"TRACE":   logging.LogLevelTrace,
	}

	levels := strings.Split(levelSpec, ",")
	for _, s := range levels {
		scopedLevel := strings.SplitN(s, ":", 2)
		scope := scopedLevel[0]
		level := scopedLevel[1]
		l, found := logLevels[strings.ToUpper(level)]
		if found == false {
			continue
		}
		
		if strings.ToLower(scope) == "all" {
			logger.DefaultLogLevel = l
			logger.ScopeLevels["turncat"] = l
			continue
		}

		logger.ScopeLevels[scope] = l
	}
	return logger
}

func NewTurncat(config TurncatConfig) (*Turncat, error) {
	loggerFactory := config.LoggerFactory
	if loggerFactory == nil {
		loggerFactory = logging.NewDefaultLoggerFactory()
	}
	log := loggerFactory.NewLogger("turncat")

	serverAddr, serverErr := resolveAddr(config.ServerAddr)
	if serverErr != nil {
		log.Errorf("error resolving server address: %s", serverErr.Error())
		return nil, serverErr
	}
	
	listenAddr, listenErr := resolveAddr(config.ListenAddr)
	if listenErr != nil {
		log.Errorf("error resolving listener address: %s", listenErr.Error())
		return nil, listenErr
	}

	peerAddr, peerErr := resolveAddr(config.PeerAddr)
	if peerErr != nil {
		log.Errorf("error resolving peer address: %s", peerErr.Error())
		return nil, serverErr
	}

	log.Tracef("Setting up listener connection on %s", config.PeerAddr)
	
	// a global listener connection for the local tunnel endpoint
	// per-client connections will connect back to the client
	listenConf := &net.ListenConfig{Control: reuseAddr}
	listenConn, err := listenConf.ListenPacket(context.Background(), listenAddr.Network(), listenAddr.String())
	if err != nil {
		log.Warnf("cannot create listening client socket at %s: %s",
			config.ListenAddr, err)
		return nil, err
	}

	t := &Turncat{
		listenAddr:     listenAddr,
		serverAddr:     serverAddr,
		peerAddr:       peerAddr,
		listenConn:     listenConn,
		connTrack:      make(map[string]*connection),
		lock:           new(sync.Mutex),
		username:       config.Username,
		password:       config.Password,
		realm:          config.Realm,
		loggerFactory:  loggerFactory,
	}

	log.Infof("Turncat client listening on %s, TURN server: %s, peer address: %s",
		config.ListenAddr, config.ServerAddr, config.PeerAddr)
	
	return t, nil
}

func (t *Turncat) Close() {
	log := t.loggerFactory.NewLogger("turncat")
	log.Info("closing Turncat")

	// close all active connections
	for _, conn := range t.connTrack {
		t.DeleteConnection(conn)
	}

	// close the clobal listener socket
	if err := t.listenConn.Close(); err != nil {
		log.Warnf("error closing listener connection: %s", err.Error())
	}
}

// Generate a new connection by opening a UDP connection to the server
func (t *Turncat) NewConnection(clientAddr net.Addr) (*connection, error) {
	log := t.loggerFactory.NewLogger("turncat")
	log.Debugf("new connection from client %s", clientAddr.String())

	conn := new(connection)
	conn.clientAddr = clientAddr

	log.Tracef("Setting up TURN client to server %s:%s",
		t.serverAddr.Network(), t.serverAddr.String())
	
	// PacketConn for the TURN server as it will not create one
	turnConn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		log.Warnf("cannot allocate TURN listening socket for client %s: %s",
			clientAddr.Network(), clientAddr.String(), err)
		return nil, err
	}

	turnClient, err := turn.NewClient(&turn.ClientConfig{
		STUNServerAddr: t.serverAddr.String(),
		TURNServerAddr: t.serverAddr.String(),
		Conn:           turnConn,
		Username:       t.username,
		Password:       t.password,
		Realm:          t.realm,
		LoggerFactory:  t.loggerFactory,
	})
	if err != nil {
		log.Warnf("cannot allocate TURN client for client %s:%s: %s",
			clientAddr.Network(), clientAddr.String(), err)
		turnConn.Close()
		return nil, err
	}

	// Start the TURN client
	if err = turnClient.Listen(); err != nil {
		log.Warnf("cannot listen on TURN client: %s", err)
		turnConn.Close()
		return nil, err
	}
	conn.turnClient = turnClient
	
	log.Tracef("Allocating relay transport for client %s:%s", clientAddr.Network(), clientAddr.String())
	
	serverConn, serverErr := turnClient.Allocate()
	if serverErr != nil {
		log.Warnf("could not allocate new TURN relay transport for client %s:%s: %s",
			clientAddr.Network(), clientAddr.String(), serverErr.Error())
		turnClient.Close()
		return nil, serverErr
	}
	conn.serverConn = serverConn

	log.Tracef("Connnecting back to client %s:%s", clientAddr.Network(), clientAddr.String())
	
	dialer := &net.Dialer{LocalAddr: t.listenAddr, Control: reuseAddr}
	clientConn, clientErr := dialer.Dial(clientAddr.Network(), clientAddr.String())
	if clientErr != nil {
		log.Warnf("cannot connect back to client %s:%s: %s",
			clientAddr.Network(), clientAddr.String(), clientErr.Error())
		turnClient.Close()
		return nil, clientErr
	}
	conn.clientConn = clientConn

	// The relayConn's local address is actually the transport
	// address assigned on the TURN server.
	log.Infof("new connection: client-address=%s, relayed-address=%s",
		clientAddr.String(), conn.serverConn.LocalAddr().String())

	return conn, nil
}

// don't err, just warn
func (t *Turncat) DeleteConnection(conn *connection) {
	log := t.loggerFactory.NewLogger("turncat")
	caddr := fmt.Sprintf("%s:%s", conn.clientAddr.Network(), conn.clientAddr.String())

	t.lock.Lock()
	_, found := t.connTrack[caddr]
	if !found {
		// already deleted
		t.lock.Unlock()
		return
	}
	delete(t.connTrack, caddr)
	t.lock.Unlock()
	
	log.Infof("closing client connection to %s", caddr)

	if err := conn.clientConn.Close(); err != nil {
		log.Warnf("error closing client connection for %s:%s: %s",
			conn.clientAddr.Network(), conn.clientAddr.String(), err.Error())
		}
	if err := conn.serverConn.Close(); err != nil {
		log.Warnf("error closing relayed TURN server connection for %s:%s: %s",
			conn.clientAddr.Network(), conn.clientAddr.String(), err.Error())
	}

	conn.turnClient.Close()
}

// any error on read/write will delete the connection and terminate the goroutine
func (t *Turncat) RunConnection(conn *connection) {
	log := t.loggerFactory.NewLogger("turncat")

	// Read from server
	go func() {
		buffer := make([]byte, UDP_PACKET_SIZE)
		for {
			n, peerAddr, readErr := conn.serverConn.ReadFrom(buffer[0:])
			if readErr != nil {
				// fmt.Println(PrettySprint(readErr.Error()))
				
				// ignore "use of closed network connection" errors on exit (DeleteConn already called)
				// unfoftunately, the PacketConn forged by pion/turn fails to check for net.ErrClosed...
				if !errors.Is(readErr, net.ErrClosed) &&
					!strings.Contains(readErr.Error(), "use of closed network connection"){
					log.Debugf("cannot read from TURN relay connection for client %s:%s (likely hamrless): %s",
						conn.clientAddr.Network(), conn.clientAddr.String(), readErr.Error())
					t.DeleteConnection(conn)
				}
				return
			}

			log.Tracef("forwarding packet of %d bytes from peer %s:%s on TURN relay connection for client %s:%s",
				n, peerAddr.Network(), peerAddr.String(),
				conn.clientAddr.Network(), conn.clientAddr.String())

			if _, writeErr := conn.clientConn.Write(buffer[0:n]); writeErr != nil {
				log.Debugf("cannot write to client connection for client %s:%s (likely hamrless): %s",
					conn.clientAddr.Network(), conn.clientAddr.String(), writeErr.Error())
				t.DeleteConnection(conn)
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
				if !errors.Is(readErr, net.ErrClosed) {
					log.Debugf("cannot read from client connection for client %s:%s (likely hamrless): %s",
						conn.clientAddr.Network(), conn.clientAddr.String(), readErr.Error())
					t.DeleteConnection(conn)
				}
				return
			}

			log.Tracef("forwarding packet of %d bytes from client %s:%s to peer %s:%s on TURN relay connection",
				n, conn.clientAddr.Network(), conn.clientAddr.String(),
				t.peerAddr.Network(), t.peerAddr.String())
			
			if _, writeErr := conn.serverConn.WriteTo(buffer[0:n], t.peerAddr); writeErr != nil {
				log.Debugf("cannot write to TURN relay connection for client %s (likely hamrless): %s",
					conn.clientAddr.String(), writeErr.Error())
				t.DeleteConnection(conn)
				return
			}
		}
	}()
}

func (t *Turncat) Run(p context.Context) {
	// main loop: for every new packet, create a new connection 'conn'
	// the rest of the packets of the connection should be received on conn.ClientConn
	log := t.loggerFactory.NewLogger("turncat")
	ctx, _ := context.WithCancel(p)
	c := make(chan packet, 10)

	// Read from server
	go func() {
		defer func() {close(c)}()
		buffer := make([]byte, UDP_PACKET_SIZE)

		for {
			n, clientAddr, err := t.listenConn.ReadFrom(buffer[0:])
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					log.Warnf("cannot read from listener connection: %s", err.Error())
				}
				return
			}

			log.Tracef("read initial packet of %d bytes on listener connnection from client %s:%s",
				n, clientAddr.Network(), clientAddr.String())

			b := make([]byte, n)
			copy(b, buffer)
			
			c <- packet{clientAddr: clientAddr, buffer: &b}
		}
	}()

	// create relayed connection and write packet
	go func() {
		for {
			select {
			case p := <- c:
				caddr := fmt.Sprintf("%s:%s", p.clientAddr.Network(), p.clientAddr.String())
				
				t.lock.Lock()
				trackConn, found := t.connTrack[caddr]
				if !found {
					conn, err := t.NewConnection(p.clientAddr)
					if err != nil {
						t.lock.Unlock()
						log.Warnf("relay setup failed for client %s, dropping client connection",
							caddr)
					} else {
						t.connTrack[caddr] = conn
						t.lock.Unlock()
						
						// Fire up routine to manage new connection
						// terminated once we kill their connection
						t.RunConnection(conn)
						
						// and send the packet out
						if _, err := conn.serverConn.WriteTo(*p.buffer, t.peerAddr); err != nil {
							log.Warnf("cannot write initial packet to TURN relay connection for client %s: %s",
								caddr, err.Error())
							t.DeleteConnection(conn)
						}
					}
				} else {
					t.lock.Unlock()
					
					// we should never receive a packet for an established
					// client here, but it can happen that the client is too
					// fast and a couple of packets are left stuck in the
					// global listener socket
					log.Debugf("received packet from a known client %s on the listener connection, sender too fast?", caddr)
					// send out anyway!
					if _, err := trackConn.serverConn.WriteTo(*p.buffer, t.peerAddr); err != nil {
						log.Warnf("cannot write packet to TURN relay connection for client %s: %s",
							caddr, err.Error())
						t.DeleteConnection(trackConn)
					}
				}
			case <- ctx.Done():
				log.Debug("terminating main Run() thread")
				return
			}
		}
	}()
}

//////////////////

func main() {
	var realm, level, user string
	usage := "turncat <-u|--user user1=pwd1> [-r|--realm realm] [-l|--log <turn:TRACE,all:INFO>] <udp:listener_addr:listener_port> <turn:server_addr:server_port> <udp:peer_addr:peer_port>"
	flag.StringVar(&user,  "u",     "user=pass", "A pair of username and password (default: \"user=pass\")")
	flag.StringVar(&user,  "user",  "user=pass", "A pair of username and password (default: \"user=pass\")")
	flag.StringVar(&realm, "r",     "stunner.l7mp.io", "Realm (default: \"stunner.l7mp.io\")")
	flag.StringVar(&realm, "realm", "stunner.l7mp.io", "Realm (defaults to \"stunner.l7mp.io\")")
	flag.StringVar(&level, "l",     "all:ERROR", "Log level (default: all:ERROR)")
	flag.StringVar(&level, "log",   "all:ERROR", "Log level (default: all:ERROR)")
	flag.Parse()

	if len(user) == 0 || flag.NArg() != 3 {
		fmt.Println(usage)
		os.Exit(1)
	}
	cred := strings.SplitN(user, "=", 2)

	logger := NewLogger(level)
	
	cfg := &TurncatConfig{
		ListenAddr:    flag.Arg(0),
		ServerAddr:    flag.Arg(1),
		PeerAddr:      flag.Arg(2),
		Username:      cred[0],
		Password:      cred[1],
		Realm:         realm,
		LoggerFactory: logger,
	}

	t, err := NewTurncat(*cfg)
	if err != nil {
		fmt.Println("cannot init Turncat")
		os.Exit(1)
	}

	// exitCh := make(chan os.Signal, 1)
	// signal.Notify(exitCh, os.Interrupt)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	t.Run(ctx)

	<- ctx.Done()
	t.Close()
	
}

///////////////
const (
	bracketOpen  string = "{\n"
	bracketClose string = "}"
	pointerSign  string = "&"
	nilSign      string = "<nil>"
)

// // PrettySprint generates a human readable representation of the value v.
// func PrettySprint(v interface{}) string {
// 	value := reflect.ValueOf(v)

// 	// nil
// 	switch value.Kind() {
// 	case reflect.Interface, reflect.Map, reflect.Slice, reflect.Ptr:
// 		if value.IsNil() {
// 			return nilSign
// 		}
// 	}

// 	buff := bytes.Buffer{}

// 	switch value.Kind() {
// 	case reflect.Struct:
// 		buff.WriteString(fullName(value.Type()) + bracketOpen)

// 		for i := 0; i < value.NumField(); i++ {
// 			l := string(value.Type().Field(i).Name[0])
// 			if strings.ToUpper(l) == l {
// 				buff.WriteString(fmt.Sprintf("%s: %s,\n", value.Type().Field(i).Name, PrettySprint(value.Field(i).Interface())))
// 			}
// 		}

// 		buff.WriteString(bracketClose)

// 		return buff.String()
// 	case reflect.Map:
// 		buff.WriteString("map[" + fullName(value.Type().Key()) + "]" + fullName(value.Type().Elem()) + bracketOpen)

// 		for _, k := range value.MapKeys() {
// 			buff.WriteString(fmt.Sprintf(`"%s":%s,\n`, k.String(), PrettySprint(value.MapIndex(k).Interface())))
// 		}

// 		buff.WriteString(bracketClose)

// 		return buff.String()
// 	case reflect.Ptr:
// 		if e := value.Elem(); e.IsValid() {
// 			return fmt.Sprintf("%s%s", pointerSign, PrettySprint(e.Interface()))
// 		}

// 		return nilSign
// 	case reflect.Slice:
// 		buff.WriteString("[]" + fullName(value.Type().Elem()) + bracketOpen)

// 		for i := 0; i < value.Len(); i++ {
// 			buff.WriteString(fmt.Sprintf("%s,\n", PrettySprint(value.Index(i).Interface())))
// 		}

// 		buff.WriteString(bracketClose)

// 		return buff.String()
// 	default:
// 		return fmt.Sprintf("%#v", v)
// 	}
// }

// func pkgName(t reflect.Type) string {
// 	pkg := t.PkgPath()
// 	c := strings.Split(pkg, "/")

// 	return c[len(c)-1]
// }

// func fullName(t reflect.Type) string {
// 	if pkg := pkgName(t); pkg != "" {
// 		return pkg + "." + t.Name()
// 	}

// 	return t.Name()
// }
