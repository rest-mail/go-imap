package imap

import (
	"math/big"
	"regexp"
	"strings"
	"testing"
)

// FuzzSectionSpec compares numeric section-part parsing with the RFC 3501 §9
// grammar expressed independently as a regexp plus arbitrary-precision bounds,
// and resolves the section against an arbitrary message, which must never
// panic and must return octets of that message.
func FuzzSectionSpec(f *testing.F) {
	nested := "Content-Type: multipart/mixed; boundary=b\r\n\r\n--b\r\n\r\none\r\n" +
		"--b\r\nContent-Type: message/rfc822\r\n\r\nSubject: in\r\n\r\ntwo\r\n--b--\r\n"
	for _, section := range []string{
		"", "1", "1.2.3", "1.MIME", "2.HEADER", "2.TEXT", "2.1", "2.HEADER.FIELDS (Subject)",
		"0", "01", "1.", "1..2", "4294967295", "4294967296", "MIME", "1.FOO", "1HEADER",
	} {
		f.Add(section, nested)
	}
	f.Add("1.1", "Content-Type: multipart/mixed; boundary=x\r\n\r\n--x  \r\n\r\na\r\n--xy\r\n--x--")
	grammar := regexp.MustCompile(`(?s)^((?:[1-9][0-9]*\.)*[1-9][0-9]*)?(?:\.?)(.*)$`)
	f.Fuzz(func(t *testing.T, section, raw string) {
		if len(section) > 1024 || len(raw) > 1<<16 {
			t.Skip()
		}
		path, text, ok := splitSectionPart(section)
		groups := grammar.FindStringSubmatch(section)
		wantOK := groups != nil
		if wantOK && groups[1] != "" {
			// A part path must be followed by nothing or "." and section-text.
			rest := section[len(groups[1]):]
			wantOK = rest == "" || (rest[0] == '.' && len(rest) > 1 && (rest[1] < '0' || rest[1] > '9'))
			for _, n := range strings.Split(groups[1], ".") {
				var v big.Int
				v.SetString(n, 10)
				wantOK = wantOK && v.BitLen() <= 32
			}
		} else if wantOK && section != "" && section[0] >= '0' && section[0] <= '9' {
			wantOK = false // starts with a digit but is not an nz-number path
		}
		if ok != wantOK {
			t.Fatalf("splitSectionPart(%q) = %v, %q, %t; grammar accepts %t", section, path, text, ok, wantOK)
		}
		if !ok || len(path) == 0 {
			return
		}
		_, payload, valid := numericSection(raw, path, text)
		if valid && payload != "" && !strings.Contains(raw, payload) {
			t.Fatalf("numericSection(%q) payload %q is not part of the message", section, payload)
		}
	})
}
