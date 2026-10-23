package imap

import (
	"bytes"
	"unicode/utf8"
)

// utf8Accept reports whether this session's backend supports UTF8=ACCEPT
// (RFC 6855 §3), the same test that puts the capability in CAPABILITY. Such a
// server takes UTF-8 in quoted-strings from then on and holds mailbox names to
// the Net-Unicode rules, whether or not the client has enabled the mode yet.
func (s *Session) utf8Accept() bool {
	_, ok := s.mailbox.(UTF8Mailbox)
	return ok
}

// validCommandUTF8 reports whether the command text a client typed, never a
// literal's octets, may carry its 8-bit octets. RFC 6855 §3 lets a UTF8=ACCEPT
// server's quoted-strings hold UTF8-2/3/4 and requires BAD for any octet
// sequence with the high bit set that is not well-formed UTF-8. Outside a
// quoted-string no 8-bit octet is legal at all, so the whole line is checked.
func validCommandUTF8(text string) bool {
	return utf8.ValidString(text)
}

// validUTF8MailboxName reports whether name is a mailbox name RFC 6855 §3
// allows: well-formed UTF-8 (RFC 5198 §2) with no C0 control (U+0000-U+001F),
// DELETE (U+007F), C1 control (U+0080-U+009F), LINE SEPARATOR (U+2028) or
// PARAGRAPH SEPARATOR (U+2029). Net-Unicode also asks for NFC (a SHOULD); this
// package has no normalisation tables, so that is left to the backend.
func validUTF8MailboxName(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	for _, r := range name {
		switch {
		case r <= 0x1f, r == 0x7f, r >= 0x80 && r <= 0x9f, r == 0x2028, r == 0x2029:
			return false
		}
	}
	return true
}

// headerHas8bit reports whether the header section of msg, everything before
// the first empty line, carries an octet with the high bit set. RFC 6855 §4
// requires a UTF8=ACCEPT server to answer NO to an APPEND of such a message
// until the client has issued ENABLE UTF8=ACCEPT.
func headerHas8bit(msg []byte) bool {
	for len(msg) > 0 {
		line := msg
		next := len(msg)
		if i := bytes.IndexByte(msg, '\n'); i >= 0 {
			line, next = msg[:i], i+1
		}
		if len(bytes.TrimSuffix(line, []byte("\r"))) == 0 {
			return false // the empty line that ends the header section
		}
		for _, c := range line {
			if c >= 0x80 {
				return true
			}
		}
		msg = msg[next:]
	}
	return false
}
