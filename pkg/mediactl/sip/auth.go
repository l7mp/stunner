package sip

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
)

// authorize answers a digest challenge header value for a request (RFC 2617, MD5, qop auth or
// none).
func authorize(username, password, chal, method, uri string) string {
	p := map[string]string{}
	v := strings.TrimSpace(chal)
	if len(v) >= 6 && strings.EqualFold(v[:6], "digest") {
		v = v[6:]
	}
	for len(v) > 0 {
		v = strings.TrimLeft(v, " ,")
		eq := strings.IndexByte(v, '=')
		if eq < 0 {
			break
		}
		key := strings.ToLower(strings.TrimSpace(v[:eq]))
		v = v[eq+1:]
		var val string
		if strings.HasPrefix(v, `"`) {
			end := strings.IndexByte(v[1:], '"')
			if end < 0 {
				break
			}
			val, v = v[1:end+1], v[end+2:]
		} else {
			end := strings.IndexByte(v, ',')
			if end < 0 {
				end = len(v)
			}
			val, v = strings.TrimSpace(v[:end]), v[end:]
		}
		p[key] = val
	}

	md5hex := func(s string) string {
		h := md5.Sum([]byte(s))
		return hex.EncodeToString(h[:])
	}
	ha1 := md5hex(username + ":" + p["realm"] + ":" + password)
	ha2 := md5hex(method + ":" + uri)
	ret := fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", algorithm=MD5`,
		username, p["realm"], p["nonce"], uri)
	if p["opaque"] != "" {
		ret += fmt.Sprintf(`, opaque="%s"`, p["opaque"])
	}
	if !strings.Contains(p["qop"], "auth") {
		return ret + fmt.Sprintf(`, response="%s"`, md5hex(ha1+":"+p["nonce"]+":"+ha2))
	}
	cnonce := randomHex(8)
	resp := md5hex(ha1 + ":" + p["nonce"] + ":00000001:" + cnonce + ":auth:" + ha2)
	return ret + fmt.Sprintf(`, response="%s", qop=auth, nc=00000001, cnonce="%s"`, resp, cnonce)
}
