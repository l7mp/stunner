package rtp

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"github.com/pion/sdp/v3"
)

// endpoint is where a party of a call sends and receives one media stream: symmetric RTP, so the
// address it advertises is also the address it sends from.
type endpoint struct {
	rtp, rtcp netip.AddrPort
	// mux is true when the party offered or accepted rtcp-mux.
	mux bool
}

// iceAttrs are the ICE attributes stripped from every SDP: trunks do not do ICE.
var iceAttrs = map[string]bool{"candidate": true, "ice-ufrag": true, "ice-pwd": true,
	"ice-options": true, "ice-lite": true, "end-of-candidates": true, "remote-candidates": true,
	"ice-mismatch": true}

// endpoints reads the endpoint of every media section of an SDP; a rejected or held section
// (port 0) has the zero endpoint.
func endpoints(d *sdp.SessionDescription) ([]endpoint, error) {
	ret := make([]endpoint, len(d.MediaDescriptions))
	for i, m := range d.MediaDescriptions {
		port := m.MediaName.Port.Value
		if port == 0 {
			continue
		}
		c := d.ConnectionInformation
		if m.ConnectionInformation != nil {
			c = m.ConnectionInformation
		}
		if c == nil || c.Address == nil {
			return nil, fmt.Errorf("media section %d has no connection address", i)
		}
		addr, err := netip.ParseAddr(c.Address.Address)
		if err != nil {
			return nil, fmt.Errorf("media section %d: %w", i, err)
		}
		e := endpoint{rtp: netip.AddrPortFrom(addr, uint16(port))}
		rtcpPort := port + 1
		for _, a := range m.Attributes {
			switch a.Key {
			case "rtcp-mux":
				e.mux = true
			case "rtcp":
				f := strings.Fields(a.Value)
				if len(f) > 0 {
					if p, err := strconv.Atoi(f[0]); err == nil {
						rtcpPort = p
					}
				}
				// a=rtcp:port IN IP4 addr names another address
				if len(f) == 4 {
					if a, err := netip.ParseAddr(f[3]); err == nil {
						addr = a
					}
				}
			}
		}
		e.rtcp = netip.AddrPortFrom(addr, uint16(rtcpPort))
		if e.mux {
			e.rtcp = e.rtp
		}
		ret[i] = e
	}
	return ret, nil
}

// rewrite points media section i of an SDP at addrs[i] (a zero address leaves it alone), with
// a=rtcp naming rtcp[i] and a=rtcp-mux when mux[i], and drops the ICE attributes.
func rewrite(d *sdp.SessionDescription, addrs, rtcp []netip.AddrPort, mux []bool) {
	var first netip.Addr
	for i, m := range d.MediaDescriptions {
		if !addrs[i].IsValid() {
			continue
		}
		a := addrs[i].Addr()
		if !first.IsValid() {
			first = a
		}
		m.MediaName.Port = sdp.RangedPort{Value: int(addrs[i].Port())}
		if m.ConnectionInformation != nil {
			m.ConnectionInformation = connInfo(a)
		}
		attrs := m.Attributes[:0]
		for _, at := range m.Attributes {
			if iceAttrs[at.Key] || at.Key == "rtcp" || at.Key == "rtcp-mux" {
				continue
			}
			attrs = append(attrs, at)
		}
		m.Attributes = attrs
		if mux[i] {
			m.WithPropertyAttribute("rtcp-mux")
		}
		if rtcp[i].IsValid() {
			v := strconv.Itoa(int(rtcp[i].Port()))
			if rtcp[i].Addr() != a {
				v += " IN " + addrType(rtcp[i].Addr()) + " " + rtcp[i].Addr().String()
			}
			m.WithValueAttribute("rtcp", v)
		}
	}
	if !first.IsValid() {
		return
	}
	// the session connection line stays, or appears when a media section lacks its own
	needed := d.ConnectionInformation != nil
	for _, m := range d.MediaDescriptions {
		needed = needed || m.ConnectionInformation == nil
	}
	if needed {
		d.ConnectionInformation = connInfo(first)
	}
	d.Origin.AddressType, d.Origin.UnicastAddress = addrType(first), first.String()
	attrs := d.Attributes[:0]
	for _, at := range d.Attributes {
		if !iceAttrs[at.Key] {
			attrs = append(attrs, at)
		}
	}
	d.Attributes = attrs
}

func connInfo(a netip.Addr) *sdp.ConnectionInformation {
	return &sdp.ConnectionInformation{NetworkType: "IN", AddressType: addrType(a),
		Address: &sdp.Address{Address: a.String()}}
}

func addrType(a netip.Addr) string {
	if a.Is4() || a.Is4In6() {
		return "IP4"
	}
	return "IP6"
}
