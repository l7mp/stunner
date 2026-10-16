package ng

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
)

var errBencode = errors.New("malformed bencode")

// Encode bencodes a value: a string or []byte, an int or int64, a []any or []string of values, or
// a map[string]any (Message) of values.
func Encode(v any) ([]byte, error) {
	var b bytes.Buffer
	if err := encode(&b, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func encode(b *bytes.Buffer, v any) error {
	switch v := v.(type) {
	case string:
		b.WriteString(strconv.Itoa(len(v)))
		b.WriteByte(':')
		b.WriteString(v)
	case []byte:
		b.WriteString(strconv.Itoa(len(v)))
		b.WriteByte(':')
		b.Write(v)
	case int:
		fmt.Fprintf(b, "i%de", v)
	case int64:
		fmt.Fprintf(b, "i%de", v)
	case []string:
		b.WriteByte('l')
		for _, e := range v {
			_ = encode(b, e)
		}
		b.WriteByte('e')
	case []any:
		b.WriteByte('l')
		for _, e := range v {
			if err := encode(b, e); err != nil {
				return err
			}
		}
		b.WriteByte('e')
	case Message:
		return encode(b, map[string]any(v))
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('d')
		for _, k := range keys {
			_ = encode(b, k)
			if err := encode(b, v[k]); err != nil {
				return err
			}
		}
		b.WriteByte('e')
	default:
		return fmt.Errorf("cannot bencode %T", v)
	}
	return nil
}

// Decode parses one bencoded value: strings decode to string, integers to int64, lists to []any
// and dictionaries to Message.
func Decode(data []byte) (any, error) {
	v, rest, err := decode(data)
	if err != nil {
		return nil, err
	}
	if len(rest) != 0 {
		return nil, fmt.Errorf("%w: %d trailing bytes", errBencode, len(rest))
	}
	return v, nil
}

func decode(data []byte) (any, []byte, error) {
	if len(data) == 0 {
		return nil, nil, fmt.Errorf("%w: unexpected end", errBencode)
	}
	switch c := data[0]; {
	case c == 'i':
		end := bytes.IndexByte(data, 'e')
		if end < 0 {
			return nil, nil, fmt.Errorf("%w: unterminated integer", errBencode)
		}
		n, err := strconv.ParseInt(string(data[1:end]), 10, 64)
		if err != nil {
			return nil, nil, fmt.Errorf("%w: %s", errBencode, err.Error())
		}
		return n, data[end+1:], nil
	case c == 'l':
		list, rest := []any{}, data[1:]
		for len(rest) > 0 && rest[0] != 'e' {
			v, r, err := decode(rest)
			if err != nil {
				return nil, nil, err
			}
			list, rest = append(list, v), r
		}
		if len(rest) == 0 {
			return nil, nil, fmt.Errorf("%w: unterminated list", errBencode)
		}
		return list, rest[1:], nil
	case c == 'd':
		dict, rest := Message{}, data[1:]
		for len(rest) > 0 && rest[0] != 'e' {
			k, r, err := decode(rest)
			if err != nil {
				return nil, nil, err
			}
			key, ok := k.(string)
			if !ok {
				return nil, nil, fmt.Errorf("%w: dictionary key is not a string", errBencode)
			}
			v, r, err := decode(r)
			if err != nil {
				return nil, nil, err
			}
			dict[key], rest = v, r
		}
		if len(rest) == 0 {
			return nil, nil, fmt.Errorf("%w: unterminated dictionary", errBencode)
		}
		return dict, rest[1:], nil
	case c >= '0' && c <= '9':
		colon := bytes.IndexByte(data, ':')
		if colon < 0 {
			return nil, nil, fmt.Errorf("%w: string without length", errBencode)
		}
		n, err := strconv.Atoi(string(data[:colon]))
		if err != nil || n < 0 || colon+1+n > len(data) {
			return nil, nil, fmt.Errorf("%w: bad string length", errBencode)
		}
		return string(data[colon+1 : colon+1+n]), data[colon+1+n:], nil
	default:
		return nil, nil, fmt.Errorf("%w: unexpected %q", errBencode, c)
	}
}
