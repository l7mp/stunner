package sip

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand/v2"
	"strconv"
	"strings"
	"time"

	sipmsg "github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"github.com/pion/stun/v4"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

var (
	// RegisterTimeout bounds a REGISTER transaction.
	RegisterTimeout = 10 * time.Second
	// KeepaliveInterval is the range the STUN keepalives of a datagram flow are sent at, at a
	// random point each time (RFC 5626 section 4.4.1).
	KeepaliveInterval = [2]time.Duration{24 * time.Second, 29 * time.Second}
	// KeepaliveMisses is the number of consecutive unanswered keepalives that fail a flow whose
	// registrar has answered one.
	KeepaliveMisses = 2
)

// answerChallenge answers the digest challenge of a registrar with the username and password of the
// auth policy of a registration, returning the Authorization or Proxy-Authorization header to
// retry with.
func (s *Server) answerChallenge(reg stnrv2.SIPRegistrationConfig, req *sipmsg.Request, res *sipmsg.Response) (sipmsg.Header, error) {
	auth := s.rt.AuthConfig(reg.Auth)
	if auth == nil || auth.Credentials["username"] == "" {
		return nil, fmt.Errorf("no username in auth policy %q", reg.Auth)
	}
	challenge, answer := "WWW-Authenticate", "Authorization"
	if res.StatusCode == sipmsg.StatusProxyAuthRequired {
		challenge, answer = "Proxy-Authenticate", "Proxy-Authorization"
	}
	h := res.GetHeader(challenge)
	if h == nil {
		return nil, fmt.Errorf("no %s header", challenge)
	}
	chal, err := digest.ParseChallenge(h.Value())
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", challenge, err)
	}
	// registrars send the algorithm in lower case too
	chal.Algorithm = strings.ToUpper(chal.Algorithm)
	cred, err := digest.Digest(chal, digest.Options{Method: req.Method.String(), URI: req.Recipient.String(),
		Count: 1, Username: auth.Credentials["username"], Password: auth.Credentials["password"]})
	if err != nil {
		return nil, err
	}
	return sipmsg.NewHeader(answer, cred.String()), nil
}

// register keeps a registration up over the flow of a peer: a REGISTER now, a refresh at half of
// each granted expiry, and STUN keepalives in between on a datagram flow. A registration that
// fails closes the flow, so its dialing listener redials, backing off, and registers again; so
// do KeepaliveMisses unanswered keepalives once the registrar has answered one (registrars
// without outbound support never answer).
func (s *Server) register(p *peer, name string) {
	callID, tag := randomHex(12), randomHex(6)
	cseq := uint32(0)
	interval := KeepaliveInterval
	var last [stun.TransactionIDSize]byte
	armed, unanswered := false, 0
	for {
		reg, ok := s.registration(name)
		if !ok {
			s.log.Warnf("server %s: flow %s names no registration %q", s.Name(), p.RemoteAddr(), name)
			return
		}
		granted, err := s.registerOnce(p, reg, callID, tag, &cseq)
		select {
		case <-p.done:
			// the flow closed under the REGISTER
			return
		default:
		}
		if err != nil {
			s.log.Warnf("server %s: registration %s of %s failed: %s", s.Name(), reg.Name, reg.AOR,
				err.Error())
			p.e.Close("registration failed")
			return
		}
		s.log.Infof("server %s: registered %s over %s for %s", s.Name(), reg.AOR, p.RemoteAddr(),
			granted)

		refresh := time.After(granted / 2)
	wait:
		for {
			var keepalive <-chan time.Time
			if !p.stream() {
				keepalive = time.After(interval[0] + mrand.N(interval[1]-interval[0]))
			}
			select {
			case <-p.done:
				return
			case <-keepalive:
				if armed && unanswered >= KeepaliveMisses {
					s.log.Warnf("server %s: registration %s: %d keepalives over %s unanswered",
						s.Name(), reg.Name, unanswered, p.RemoteAddr())
					p.e.Close("keepalives unanswered")
					return
				}
				msg, _ := stun.Build(stun.TransactionID, stun.BindingRequest)
				last, unanswered = msg.TransactionID, unanswered+1
				_, _ = p.Write(msg.Raw)
			case id := <-p.answered:
				if id == last {
					armed, unanswered = true, 0
				}
			case <-refresh:
				break wait
			}
		}
	}
}

// registerOnce sends the REGISTER of a registration over the flow of a peer and returns the expiry
// the registrar granted: a challenge is answered once, an interval found too brief is raised to the
// registrar's minimum.
func (s *Server) registerOnce(p *peer, reg stnrv2.SIPRegistrationConfig, callID, tag string, cseq *uint32) (time.Duration, error) {
	var aor sipmsg.Uri
	if err := sipmsg.ParseUri(reg.AOR, &aor); err != nil {
		return 0, fmt.Errorf("invalid AOR: %w", err)
	}
	// a validated config holds a positive duration
	d, _ := time.ParseDuration(reg.Expires)
	expires := uint32(d.Seconds())

	var auth sipmsg.Header
	for range 3 {
		*cseq++
		req := s.newRegister(p, aor, callID, tag, *cseq, expires)
		if auth != nil {
			req.AppendHeader(auth)
		}
		ctx, cancel := context.WithTimeout(context.Background(), RegisterTimeout)
		res, err := s.txl.Do(ctx, req, p)
		cancel()
		if err != nil {
			return 0, err
		}
		switch {
		case res.IsSuccess():
			if granted := grantedExpiry(res, expires); granted > 0 {
				return granted, nil
			}
			return 0, errors.New("the registrar granted no expiry")
		case (res.StatusCode == sipmsg.StatusUnauthorized || res.StatusCode == sipmsg.StatusProxyAuthRequired) &&
			auth == nil:
			if auth, err = s.answerChallenge(reg, req, res); err != nil {
				return 0, fmt.Errorf("%d %s: %w", res.StatusCode, res.Reason, err)
			}
		case res.StatusCode == sipmsg.StatusIntervalToBrief:
			h := res.GetHeader("Min-Expires")
			if h == nil {
				return 0, fmt.Errorf("%d %s without Min-Expires", res.StatusCode, res.Reason)
			}
			v, err := strconv.ParseUint(h.Value(), 10, 32)
			if err != nil || uint32(v) <= expires {
				return 0, fmt.Errorf("%d %s: invalid Min-Expires %q", res.StatusCode, res.Reason, h.Value())
			}
			expires = uint32(v)
		default:
			return 0, fmt.Errorf("%d %s", res.StatusCode, res.Reason)
		}
	}
	return 0, errors.New("the registrar keeps refusing")
}

// newRegister makes a REGISTER of an AOR over the flow of a peer. The Contact and the Via sent-by
// are the local address of the flow, marked as an RFC 5626 outbound flow: registrars route the
// requests of the AOR over the flow, and the ones that do not fix the Contact to the source they
// saw, which is the flow too.
func (s *Server) newRegister(p *peer, aor sipmsg.Uri, callID, tag string, cseq, expires uint32) *sipmsg.Request {
	local := addrPort(p.LocalAddr())
	req := sipmsg.NewRequest(sipmsg.REGISTER, sipmsg.Uri{Scheme: aor.Scheme, Host: aor.Host, Port: aor.Port})

	via := sipmsg.NewParams()
	via.Add("branch", sipmsg.RFC3261BranchMagicCookie+randomHex(10))
	via.Add("rport", "")
	req.AppendHeader(&sipmsg.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0",
		Transport: transport(p), Host: local.Addr().String(), Port: int(local.Port()), Params: via})
	mf := sipmsg.MaxForwardsHeader(70)
	req.AppendHeader(&mf)
	from := &sipmsg.FromHeader{Address: aor, Params: sipmsg.NewParams()}
	from.Params.Add("tag", tag)
	req.AppendHeader(from)
	req.AppendHeader(&sipmsg.ToHeader{Address: aor, Params: sipmsg.NewParams()})
	cid := sipmsg.CallIDHeader(callID)
	req.AppendHeader(&cid)
	req.AppendHeader(&sipmsg.CSeqHeader{SeqNo: cseq, MethodName: sipmsg.REGISTER})

	uri := sipmsg.NewParams()
	if t := transport(p); t != "UDP" {
		uri.Add("transport", strings.ToLower(t))
	}
	uri.Add("ob", "")
	params := sipmsg.NewParams()
	params.Add("+sip.instance", `"<urn:uuid:`+s.instance+`>"`)
	params.Add("reg-id", "1")
	req.AppendHeader(&sipmsg.ContactHeader{Address: sipmsg.Uri{Scheme: "sip", User: aor.User,
		Host: local.Addr().String(), Port: int(local.Port()), UriParams: uri}, Params: params})
	exp := sipmsg.ExpiresHeader(expires)
	req.AppendHeader(&exp)
	req.AppendHeader(sipmsg.NewHeader("Supported", "outbound, path"))
	req.SetBody(nil)
	return req
}

// grantedExpiry returns the expiry a 2xx to a REGISTER grants: the expires of the Contact, else the
// Expires header, else the one asked for.
func grantedExpiry(res *sipmsg.Response, asked uint32) time.Duration {
	secs := asked
	if h := res.GetHeader("Expires"); h != nil {
		if v, err := strconv.ParseUint(h.Value(), 10, 32); err == nil {
			secs = uint32(v)
		}
	}
	if c := res.Contact(); c != nil {
		if e, ok := c.Params.Get("expires"); ok {
			if v, err := strconv.ParseUint(e, 10, 32); err == nil {
				secs = uint32(v)
			}
		}
	}
	return time.Duration(secs) * time.Second
}

// registration returns the registration config of a name.
func (s *Server) registration(name string) (stnrv2.SIPRegistrationConfig, bool) {
	s.confMu.RLock()
	defer s.confMu.RUnlock()
	for _, r := range s.registrations {
		if r.Name == name {
			return r, true
		}
	}
	return stnrv2.SIPRegistrationConfig{}, false
}
