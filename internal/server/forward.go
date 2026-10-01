package server

import "github.com/l7mp/stunner/v2/internal/api"

// bufferSize is the forwarder chunk size, the largest UDP datagram: one read makes one datagram
// on a datagram conn, so a stream-to-datagram flow frames the stream by the chunks read.
const bufferSize = 65535

// Forward pumps an entry's client conn and leg into each other, touching the entry, until either
// side ends; it returns on teardown.
func Forward(e *Entry) {
	go pump(e, e.Leg, e.Client, "peer side", "client side")
	pump(e, e.Client, e.Leg, "client side", "peer side")
}

func pump(e *Entry, from, to api.Conn, fromName, toName string) {
	buf := make([]byte, bufferSize)
	for {
		n, err := from.Read(buf)
		if err != nil {
			// io.EOF included: a TCP FIN or stdin EOF ends the flow
			e.Close(fromName + " closed")
			return
		}
		e.Touch()
		if _, err := to.Write(buf[:n]); err != nil {
			if e.Hooks.OnError != nil {
				e.Hooks.OnError(toName + " write error: " + err.Error())
			}
			e.Close(toName + " write error")
			return
		}
	}
}
