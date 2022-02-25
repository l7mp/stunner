// creates a tunnel through a TURN server
// turncat --client=127.0.0.1:5000 --server=127.0.0.1:3478 --peer=127.0.0.1:5001 --users test=test

package main

import (
	"flag"
	"log"
	"net"
	"strings"
	"sync"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"
)

// Information maintained for each client/server connection
type Connection struct {
	ClientAddr net.Addr    // Address of the client
	ServerConn net.PacketConn // Relayed UDP connection to server
}

// Generate a new connection by opening a UDP connection to the server
func NewConnection(turnClient *turn.Client, cliAddr net.Addr) *Connection {
	conn := new(Connection)
	conn.ClientAddr = cliAddr

	// Allocate a relay socket on the TURN server. On success, it will return a net.PacketConn
	// which represents the remote socket.
	c, err := turnClient.Allocate()
	if err != nil {
		panic(err)
	}
	conn.ServerConn = c

	// The relayConn's local address is actually the transport
	// address assigned on the TURN server.
	log.Printf("new connection: client-address=%s, relayed-address=%s",
		cliAddr.String(), conn.ServerConn.LocalAddr().String())

	return conn
}

// Go routine which manages connection from server to single client
func RunConnection(conn *Connection, clconn net.PacketConn) {
	var buffer [1500]byte
	for {
		// Read from server
		n, _, err := conn.ServerConn.ReadFrom(buffer[0:])
		if err != nil {
			panic(err)
		}
		// Relay it to client
		_, err = clconn.WriteTo(buffer[0:n], conn.ClientAddr)
		if err != nil {
			panic(err)
		}
	}
}

// Mapping from client addresses (as host:port) to connection
var ClientDict map[string]*Connection = make(map[string]*Connection)
// Mutex used to serialize access to the dictionary
var dmutex *sync.Mutex = new(sync.Mutex)

//////////////////

func main() {
	usage := "turncat --client=<ADDR:port> --server=<ADDR:port> --peer=<ADDR:port> --user=<user1=pwd1>"
	server := flag.String("server", "", "TURN server addres:port")
	client := flag.String("client", "", "local tunnel endpoint addres:port")
	peer := flag.String("peer", "", "remote tunnel endpoint addres:port")
	user := flag.String("user", "", "A pair of username and password (e.g. \"user=pass\")")
	realm := flag.String("realm", "pion.ly", "Realm (defaults to \"pion.ly\")")
	verbose := flag.Bool("verbose", false, "Verbose logging")
	flag.Parse()

	if len(*server) == 0 || len(*client) == 0 || len(*peer) == 0 || len(*user) == 0 {
		log.Fatalf(usage)
	}

	peerAddr, err := net.ResolveUDPAddr("udp", *peer)
	if err != nil {
		panic(err)
	}
	
	cred := strings.SplitN(*user, "=", 2)

	// TURN client won't create a local listening socket by itself
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		panic(err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			panic(closeErr)
		}
	}()

	logger := logging.NewDefaultLoggerFactory()
	logger.DefaultLogLevel = logging.LogLevelWarn
	if *verbose == true {
		logger.ScopeLevels["turn"] = logging.LogLevelDebug
	}

	cfg := &turn.ClientConfig{
		STUNServerAddr: *server,
		TURNServerAddr: *server,
		Conn:           conn,
		Username:       cred[0],
		Password:       cred[1],
		Realm:          *realm,
		LoggerFactory:  logger,
	}

	turnClient, err := turn.NewClient(cfg)
	if err != nil {
		panic(err)
	}
	defer turnClient.Close()

	// Start listening on the conn provided.
	err = turnClient.Listen()
	if err != nil {
		panic(err)
	}

	// local tunnel endpoint
	clconn, err := net.ListenPacket("udp4", *client)
	if err != nil {
		panic(err)
	}
	defer func() {
		if closeErr := clconn.Close(); closeErr != nil {
			panic(closeErr)
		}
	}()

	// main loop
	var buffer [1500]byte
	for {
		n, cliaddr, err := clconn.ReadFrom(buffer[0:])
		if err != nil {
			panic(err)
		}
		saddr := cliaddr.String()

		dmutex.Lock()
		conn, found := ClientDict[saddr]
		if !found {
			conn = NewConnection(turnClient, cliaddr)
			if conn == nil {
				dmutex.Unlock()
				continue
			}
			ClientDict[saddr] = conn
			dmutex.Unlock()

			defer func() {
				if closeErr := conn.ServerConn.Close(); closeErr != nil {
					panic(closeErr)
				}
			}()
			
			// Fire up routine to manage new connection
			go RunConnection(conn, clconn)
		} else {
			dmutex.Unlock()
		}
		
		// Relay to server
		_, err = conn.ServerConn.WriteTo(buffer[0:n], peerAddr)
		if err != nil {
			log.Println(err)
		}
	}

	return
}
