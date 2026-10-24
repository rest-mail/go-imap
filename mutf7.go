package imap

import (
	"encoding/base64"
	"errors"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// A Mailbox names its folders in UTF-8, and the Session converts at the wire.
// Until the client enables UTF8=ACCEPT, IMAP4rev1 mailbox names are 7-bit, with
// other characters in modified UTF-7 (RFC 3501 §5.1.3); after, they are UTF-8
// (RFC 6855 §3).

// mutf7Base64 is modified BASE64: base64 with "," for "/" and no padding.
var mutf7Base64 = base64.NewEncoding("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+,").WithPadding(base64.NoPadding).Strict()

// errInvalidMailboxName is a mailbox name a command carries that names no
// folder: not modified UTF-7, or 8-bit where UTF-8 is not accepted.
var errInvalidMailboxName = errors.New("imap: invalid mailbox name")

// encodeMUTF7 writes a UTF-8 mailbox name in modified UTF-7 (RFC 3501 §5.1.3):
// printable US-ASCII other than "&" stands for itself and "&" is "&-"; each
// run of other characters is "&", their UTF-16 in modified BASE64, then "-".
func encodeMUTF7(name string) string {
	var b strings.Builder
	var run []rune
	flush := func() {
		if len(run) == 0 {
			return
		}
		units := utf16.Encode(run)
		buf := make([]byte, 0, 2*len(units))
		for _, u := range units {
			buf = append(buf, byte(u>>8), byte(u))
		}
		b.WriteByte('&')
		b.WriteString(mutf7Base64.EncodeToString(buf))
		b.WriteByte('-')
		run = run[:0]
	}
	for _, r := range name {
		switch {
		case r == '&':
			flush()
			b.WriteString("&-")
		case r >= 0x20 && r <= 0x7e:
			flush()
			b.WriteRune(r)
		default:
			run = append(run, r)
		}
	}
	flush()
	return b.String()
}

// decodeMUTF7 reads a modified UTF-7 mailbox name as UTF-8. ok is false unless
// name is exactly what encodeMUTF7 writes for what it decodes to, so a name
// with an octet outside printable US-ASCII, a base64 run not closed by "-", a
// run encoding a character that can represent itself, a superfluous shift
// between runs, nonzero padding bits or an unpaired surrogate is refused
// (RFC 3501 §5.1.3). The base64 is compared exactly, so its case is kept.
func decodeMUTF7(name string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(name); {
		c := name[i]
		if c < 0x20 || c > 0x7e {
			return "", false
		}
		if c != '&' {
			b.WriteByte(c)
			i++
			continue
		}
		end := strings.IndexByte(name[i+1:], '-')
		if end < 0 {
			return "", false
		}
		run := name[i+1 : i+1+end]
		i += end + 2
		if run == "" {
			b.WriteByte('&')
			continue
		}
		buf, err := mutf7Base64.DecodeString(run)
		if err != nil || len(buf)%2 != 0 {
			return "", false
		}
		units := make([]uint16, len(buf)/2)
		for j := range units {
			units[j] = uint16(buf[2*j])<<8 | uint16(buf[2*j+1])
		}
		for _, r := range utf16.Decode(units) {
			b.WriteRune(r)
		}
	}
	decoded := b.String()
	if encodeMUTF7(decoded) != name {
		return "", false
	}
	return decoded, true
}

// mailboxName is the store's name for the mailbox name a command carries. Once
// the client has enabled UTF8=ACCEPT the name is already UTF-8. Before, it is
// modified UTF-7, decoded here; a UTF8=ACCEPT server also takes a UTF-8 name
// then, as RFC 6855 §3 requires. ok is false for a name that is neither, which
// names no folder.
func (s *Session) mailboxName(wire string) (string, bool) {
	switch {
	case s.utf8:
		return wire, true
	case !isASCII(wire):
		return wire, s.utf8Accept() && utf8.ValidString(wire)
	default:
		return decodeMUTF7(wire)
	}
}

// folderName is mailboxName with INBOX, in any case, folded to INBOX (RFC 3501
// §5.1).
func (s *Session) folderName(wire string) (string, bool) {
	name, ok := s.mailboxName(wire)
	if !ok {
		return "", false
	}
	return canonicalizeInbox(name), true
}

// wireName is how a response writes the store's folder name: unchanged once the
// client has enabled UTF8=ACCEPT, in modified UTF-7 before, since a server must
// not send UTF-8 to a client that has not (RFC 3501 §5.1.3, RFC 6855 §3).
func (s *Session) wireName(name string) string {
	if s.utf8 {
		return name
	}
	return encodeMUTF7(name)
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}
