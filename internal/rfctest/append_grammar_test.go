package rfctest

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestAppendGrammarCombinations composes all four optional-argument layouts
// with atom, quoted and both counted mailbox forms, both message literal
// forms, empty strings and command-looking/8-bit message octets. The store
// receives the decoded name unchanged, including empty names; mailbox naming
// policy is a backend decision, not a command syntax error.
//
// checklist: IMAP-APPEND-23
func TestAppendGrammarCombinations(t *testing.T) {
	for _, name := range []struct {
		wire, decoded string
		literal, sync bool
	}{
		{wire: "Archive", decoded: "Archive"},
		{wire: `"a\"b\\c"`, decoded: `a"b\c`},
		{wire: `""`},
		{decoded: "", literal: true, sync: true},
		{decoded: "", literal: true},
		{decoded: "a\r\nb\x80", literal: true, sync: true},
		{decoded: "a\r\nb\x80", literal: true},
	} {
		for _, optional := range []string{"", "() ", `"17-Jul-1996 02:44:25 -0700" `, `(\Seen \Draft) "17-Jul-1996 02:44:25 -0700" `} {
			for _, raw := range []string{"", "a\x80\r\nN1 LOGOUT\r\n"} {
				for _, sync := range []bool{false, true} {
					t.Run(fmt.Sprintf("%q/%t/%t/%q/%d/%t", name.decoded, name.literal, name.sync, optional, len(raw), sync), func(t *testing.T) {
						box := &datedAppendMailbox{&appendMailbox{}}
						c := selectionClient(t, box)
						wire := "A1 APPEND "
						if name.literal {
							marker := fmt.Sprintf("{%d+}\r\n", len(name.decoded))
							if name.sync {
								marker = fmt.Sprintf("{%d}\r\n", len(name.decoded))
							}
							wire += marker
							if name.sync {
								writeLiteralWire(t, c, wire)
								if got := c.line(); !strings.HasPrefix(got, "+ ") {
									t.Fatalf("mailbox continuation: %q", got)
								}
								wire = ""
							}
							wire += name.decoded
						} else {
							wire += name.wire
						}
						wire += " " + optional
						marker := fmt.Sprintf("{%d+}\r\n", len(raw))
						if sync {
							marker = fmt.Sprintf("{%d}\r\n", len(raw))
						}
						wire += marker
						if sync {
							writeLiteralWire(t, c, wire)
							if got := c.line(); !strings.HasPrefix(got, "+ ") {
								t.Fatalf("message continuation: %q", got)
							}
							wire = ""
						}
						writeLiteralWire(t, c, wire+raw+"\r\nN1 NOOP\r\n")
						lines, tagged := appendCompletion(t, c)
						requireOK(t, tagged, "A1")
						requireExactly(t, lines)
						requireOK(t, c.line(), "N1")
						if len(box.records) != 1 {
							t.Fatalf("records: %+v", box.records)
						}
						got := box.records[0]
						if got.folder != name.decoded || got.raw != raw {
							t.Fatalf("record %+v, want folder %q raw %q", got, name.decoded, raw)
						}
						wantFlags := strings.HasPrefix(optional, `(\Seen`)
						if (got.flags.Seen != nil && *got.flags.Seen) != wantFlags || (got.flags.Draft != nil && *got.flags.Draft) != wantFlags {
							t.Fatalf("flags: %+v", got.flags)
						}
						if strings.Contains(optional, "1996") {
							want := time.Date(1996, time.July, 17, 2, 44, 25, 0, time.FixedZone("", -7*3600))
							if !got.date.Equal(want) {
								t.Fatalf("date %v, want %v", got.date, want)
							}
						} else if !got.date.IsZero() {
							t.Fatalf("unexpected date %v", got.date)
						}
					})
				}
			}
		}
	}
}

// TestAppendQuotedMailboxOctets independently expands RFC 3501's QUOTED-CHAR
// grammar, including permitted ASCII controls and escaped quoted-specials.
// CR and LF are framing and are covered by the command-line/literal tests.
//
// checklist: IMAP-APPEND-23
func TestAppendQuotedMailboxOctets(t *testing.T) {
	for octet := 0; octet <= 255; octet++ {
		if octet == '\r' || octet == '\n' {
			continue
		}
		t.Run(fmt.Sprintf("%02x", octet), func(t *testing.T) {
			content := string([]byte{byte(octet)})
			if octet == '"' || octet == '\\' {
				content = "\\" + content
			}
			wire := `"a` + content + `b"`
			box := &appendMailbox{}
			c := selectionClient(t, box)
			valid := octet > 0 && octet < 128
			if !valid {
				lines, tagged := c.command("A1", "APPEND %s {3}", wire)
				requireExactly(t, lines)
				if !strings.HasPrefix(tagged, "A1 BAD ") {
					t.Fatalf("invalid octet: %q", tagged)
				}
			} else {
				_, tagged := appendCommand(t, c, wire, "msg")
				requireOK(t, tagged, "A1")
				want := "a" + string([]byte{byte(octet)}) + "b"
				if len(box.records) != 1 || box.records[0].folder != want {
					t.Fatalf("records: %+v, want %q", box.records, want)
				}
			}
			if !valid && len(box.records) != 0 {
				t.Fatalf("invalid name stored: %+v", box.records)
			}
			_, tagged := c.command("N1", "NOOP")
			requireOK(t, tagged, "N1")
		})
	}
}
