package goja

import (
	"strconv"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/dop251/goja/unistring"
)

// jsonParser is a direct recursive-descent JSON parser producing goja Values.
// It replaces the previous encoding/json Decoder-based implementation, which
// built an intermediate interface{} tree and dominated the allocation profile
// of HTTP scenarios calling resp.json().
//
// Error messages are not byte-for-byte identical to encoding/json; only the
// "Unexpected end of JSON input" wording is required by the public behaviour.
type jsonParser struct {
	s      string
	pos    int
	failed bool
	err    string
}

func (p *jsonParser) fail(msg string) {
	if !p.failed {
		p.failed = true
		p.err = msg
	}
}

func (p *jsonParser) unexpectedEnd() {
	p.fail("Unexpected end of JSON input")
}

func (p *jsonParser) skipSpaces() {
	for p.pos < len(p.s) {
		switch p.s[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) consume(lit string) bool {
	if len(p.s)-p.pos >= len(lit) && p.s[p.pos:p.pos+len(lit)] == lit {
		p.pos += len(lit)
		return true
	}
	p.fail("Invalid JSON")
	return false
}

func (p *jsonParser) parseValue(r *Runtime) Value {
	if p.failed {
		return nil
	}
	if p.pos >= len(p.s) {
		p.unexpectedEnd()
		return nil
	}
	switch c := p.s[p.pos]; {
	case c == '{':
		return p.parseObject(r)
	case c == '[':
		return p.parseArray(r)
	case c == '"':
		s, ok := p.parseString()
		if !ok {
			return nil
		}
		return newStringValue(s)
	case c == 't':
		if p.consume("true") {
			return valueTrue
		}
		return nil
	case c == 'f':
		if p.consume("false") {
			return valueFalse
		}
		return nil
	case c == 'n':
		if p.consume("null") {
			return _null
		}
		return nil
	case c == '-' || (c >= '0' && c <= '9'):
		return p.parseNumber()
	default:
		p.fail("Invalid character in JSON")
		return nil
	}
}

func (p *jsonParser) parseObject(r *Runtime) *Object {
	p.pos++ // '{'
	object := r.NewObject()
	p.skipSpaces()
	if p.pos < len(p.s) && p.s[p.pos] == '}' {
		p.pos++
		return object
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.s) {
			p.unexpectedEnd()
			return nil
		}
		if p.s[p.pos] != '"' {
			p.fail("Invalid character in object key")
			return nil
		}
		key, ok := p.parseString()
		if !ok {
			return nil
		}
		p.skipSpaces()
		if p.pos >= len(p.s) {
			p.unexpectedEnd()
			return nil
		}
		if p.s[p.pos] != ':' {
			p.fail("Invalid character after object key")
			return nil
		}
		p.pos++
		p.skipSpaces()
		value := p.parseValue(r)
		if p.failed {
			return nil
		}
		object.self._putProp(unistring.NewFromString(key), value, true, true, true)
		p.skipSpaces()
		if p.pos >= len(p.s) {
			p.unexpectedEnd()
			return nil
		}
		switch p.s[p.pos] {
		case ',':
			p.pos++
		case '}':
			p.pos++
			return object
		default:
			p.fail("Invalid character after object value")
			return nil
		}
	}
}

func (p *jsonParser) parseArray(r *Runtime) *Object {
	p.pos++ // '['
	arrayValue := make([]Value, 0, 8)
	p.skipSpaces()
	if p.pos < len(p.s) && p.s[p.pos] == ']' {
		p.pos++
		return r.newArrayValues(arrayValue)
	}
	for {
		p.skipSpaces()
		if p.pos >= len(p.s) {
			p.unexpectedEnd()
			return nil
		}
		value := p.parseValue(r)
		if p.failed {
			return nil
		}
		arrayValue = append(arrayValue, value)
		p.skipSpaces()
		if p.pos >= len(p.s) {
			p.unexpectedEnd()
			return nil
		}
		switch p.s[p.pos] {
		case ',':
			p.pos++
		case ']':
			p.pos++
			return r.newArrayValues(arrayValue)
		default:
			p.fail("Invalid character after array value")
			return nil
		}
	}
}

func (p *jsonParser) parseString() (string, bool) {
	p.pos++ // '"'
	start := p.pos
	for {
		if p.pos >= len(p.s) {
			p.unexpectedEnd()
			return "", false
		}
		c := p.s[p.pos]
		if c == '"' {
			s := p.s[start:p.pos]
			p.pos++
			return s, true
		}
		if c == '\\' || c < 0x20 {
			break
		}
		p.pos++
	}

	buf := make([]byte, 0, p.pos-start+16)
	buf = append(buf, p.s[start:p.pos]...)
	for {
		if p.pos >= len(p.s) {
			p.unexpectedEnd()
			return "", false
		}
		c := p.s[p.pos]
		switch {
		case c == '"':
			p.pos++
			return string(buf), true
		case c == '\\':
			p.pos++
			if p.pos >= len(p.s) {
				p.unexpectedEnd()
				return "", false
			}
			esc := p.s[p.pos]
			p.pos++
			switch esc {
			case '"':
				buf = append(buf, '"')
			case '\\':
				buf = append(buf, '\\')
			case '/':
				buf = append(buf, '/')
			case 'b':
				buf = append(buf, '\b')
			case 'f':
				buf = append(buf, '\f')
			case 'n':
				buf = append(buf, '\n')
			case 'r':
				buf = append(buf, '\r')
			case 't':
				buf = append(buf, '\t')
			case 'u':
				r, ok := p.parseHex4()
				if !ok {
					return "", false
				}
				if utf16.IsSurrogate(r) {
					if p.pos+1 < len(p.s) && p.s[p.pos] == '\\' && p.s[p.pos+1] == 'u' {
						p.pos += 2
						r2, ok := p.parseHex4()
						if !ok {
							return "", false
						}
						combined := utf16.DecodeRune(r, r2)
						if combined == utf8.RuneError {
							buf = utf8.AppendRune(buf, utf8.RuneError)
						} else {
							buf = utf8.AppendRune(buf, combined)
						}
					} else {
						buf = utf8.AppendRune(buf, utf8.RuneError)
					}
				} else {
					buf = utf8.AppendRune(buf, r)
				}
			default:
				p.fail("Invalid escape character in string")
				return "", false
			}
		case c < 0x20:
			p.fail("Invalid control character in string")
			return "", false
		default:
			buf = append(buf, c)
			p.pos++
		}
	}
}

func (p *jsonParser) parseHex4() (rune, bool) {
	if p.pos+4 > len(p.s) {
		p.unexpectedEnd()
		return 0, false
	}
	var r rune
	for i := 0; i < 4; i++ {
		c := p.s[p.pos]
		var d byte
		switch {
		case c >= '0' && c <= '9':
			d = c - '0'
		case c >= 'a' && c <= 'f':
			d = c - 'a' + 10
		case c >= 'A' && c <= 'F':
			d = c - 'A' + 10
		default:
			p.fail("Invalid \\u escape in string")
			return 0, false
		}
		r = r<<4 | rune(d)
		p.pos++
	}
	return r, true
}

func (p *jsonParser) parseNumber() Value {
	start := p.pos
	if p.s[p.pos] == '-' {
		p.pos++
		if p.pos >= len(p.s) {
			p.unexpectedEnd()
			return nil
		}
	}
	switch c := p.s[p.pos]; {
	case c == '0':
		p.pos++
	case c >= '1' && c <= '9':
		p.pos++
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	default:
		p.fail("Invalid number in JSON")
		return nil
	}
	if p.pos < len(p.s) && p.s[p.pos] == '.' {
		p.pos++
		if p.pos >= len(p.s) || p.s[p.pos] < '0' || p.s[p.pos] > '9' {
			p.fail("Invalid number in JSON")
			return nil
		}
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	if p.pos < len(p.s) && (p.s[p.pos] == 'e' || p.s[p.pos] == 'E') {
		p.pos++
		if p.pos < len(p.s) && (p.s[p.pos] == '+' || p.s[p.pos] == '-') {
			p.pos++
		}
		if p.pos >= len(p.s) || p.s[p.pos] < '0' || p.s[p.pos] > '9' {
			p.fail("Invalid number in JSON")
			return nil
		}
		for p.pos < len(p.s) && p.s[p.pos] >= '0' && p.s[p.pos] <= '9' {
			p.pos++
		}
	}
	f, err := strconv.ParseFloat(p.s[start:p.pos], 64)
	if err != nil {
		// Out-of-range (Inf) and malformed numbers both surface as errors,
		// matching encoding/json's rejection of them.
		p.fail("Invalid number in JSON")
		return nil
	}
	return floatToValue(f)
}
