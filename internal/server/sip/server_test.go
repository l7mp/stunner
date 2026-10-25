package sip

import (
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pion/sdp/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// TestStreamFraming pins that the conns of the stream listeners, TCP and TLS, are read as SIP
// streams framed by Content-Length: a message split over two writes and two messages in one write
// are each handled whole, and the proxy's answers name the transport in their Via.
func TestStreamFraming(t *testing.T) {
	for _, proto := range []stnrv2.Protocol{stnrv2.ProtocolTCP, stnrv2.ProtocolTLS} {
		t.Run(proto.String(), func(t *testing.T) {
			rt := objruntime.New(objruntime.Config{Logger: logger.NewLoggerFactory("all:ERROR")})
			conf := &stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: "sip", Type: "SIP"}}
			require.NoError(t, conf.Validate())
			s, err := newServer(conf, rt)
			require.NoError(t, err)
			require.NoError(t, s.Start())
			defer s.Close(false) //nolint:errcheck

			client, server := net.Pipe()
			defer client.Close() //nolint:errcheck
			_, _, err = s.Serve(netconn.NewConn(server, api.Tag{Name: "sip", Proto: proto}, nil))
			require.NoError(t, err)

			options := func(branch string) string {
				return fmt.Sprintf("OPTIONS sip:192.0.2.9 SIP/2.0\r\n"+
					"Via: SIP/2.0/%s 192.0.2.1:5060;branch=z9hG4bK%s\r\n"+
					"From: <sip:carrier@192.0.2.1>;tag=f%s\r\nTo: <sip:192.0.2.9>\r\n"+
					"Call-ID: %s@192.0.2.1\r\nCSeq: 1 OPTIONS\r\nMax-Forwards: 70\r\n"+
					"Content-Length: 0\r\n\r\n", proto.String(), branch, branch, branch)
			}
			first, second := options("first"), options("second")
			go func() {
				_, _ = client.Write([]byte(first[:40]))
				time.Sleep(20 * time.Millisecond)
				_, _ = client.Write([]byte(first[40:] + second))
			}()

			var got strings.Builder
			buf := make([]byte, 4096)
			require.NoError(t, client.SetReadDeadline(time.Now().Add(5*time.Second)))
			for strings.Count(got.String(), "SIP/2.0 200 OK") < 2 {
				n, err := client.Read(buf)
				require.NoError(t, err, "both answers arrive: %q", got.String())
				got.Write(buf[:n])
			}
			assert.Contains(t, got.String(), "branch=z9hG4bKfirst", "the split message is answered")
			assert.Contains(t, got.String(), "branch=z9hG4bKsecond", "the second message is answered")
			assert.Equal(t, "TLS", transport(netconn.NewConn(server, api.Tag{Proto: stnrv2.ProtocolTLS}, nil)))
		})
	}
}

func TestDeclined(t *testing.T) {
	offer := "v=0\r\no=- 1 1 IN IP4 10.0.0.5\r\ns=-\r\nc=IN IP4 10.0.0.5\r\nt=0 0\r\na=sendrecv\r\n" +
		"m=audio 4000 RTP/AVP 0 8\r\nc=IN IP6 fd00::5\r\na=rtcp:4001\r\na=candidate:1 1 UDP 1 10.0.0.5 4000 typ host\r\n" +
		"m=video 5000 RTP/AVP 96\r\na=rtpmap:96 VP8/90000\r\n"
	d := &sdp.SessionDescription{}
	require.NoError(t, d.Unmarshal(declined([]byte(offer))), "a valid SDP")
	assert.Equal(t, "0.0.0.0", d.Origin.UnicastAddress, "no origin address")
	assert.Equal(t, "0.0.0.0", d.ConnectionInformation.Address.Address, "no session address")
	assert.Len(t, d.Attributes, 1, "the session attributes are kept")
	require.Len(t, d.MediaDescriptions, 2)
	for _, m := range d.MediaDescriptions {
		assert.Equal(t, 0, m.MediaName.Port.Value, "%s declined", m.MediaName.Media)
		assert.Empty(t, m.Attributes, "%s: no media attributes, no candidates", m.MediaName.Media)
	}
	assert.Equal(t, "::", d.MediaDescriptions[0].ConnectionInformation.Address.Address, "no media address")
	assert.Equal(t, []string{"0", "8"}, d.MediaDescriptions[0].MediaName.Formats, "the formats are kept")

	assert.Nil(t, declined([]byte("not an SDP")), "nothing of an SDP that cannot be parsed")
}
