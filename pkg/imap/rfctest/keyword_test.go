package rfctest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rest-mail/go-imap/pkg/imap"
)

// keywordMessages has every system flag combination the keyword keys must
// ignore, with UIDs that differ from sequence numbers.
func keywordMessages() []imap.Message {
	return []imap.Message{
		{UID: 10, Seen: true, Flagged: true},
		{UID: 40, Answered: true, Draft: true},
		{UID: 90},
	}
}

// TestKeywordsNotAdvertised proves the session never claims keywords it
// cannot store: PERMANENTFLAGS lists no \*, FLAGS lists only system flags,
// and a keyword sent with STORE never comes back in FETCH FLAGS, in the
// untagged STORE reply or after reselecting the mailbox.
//
// checklist: IMAP-STORE-06
func TestKeywordsNotAdvertised(t *testing.T) {
	box := newFetchMailbox()
	box.messages = keywordMessages()
	c := selectionClient(t, box)
	lines, tagged := c.command("S1", "SELECT INBOX")
	requireOK(t, tagged, "S1")
	requireLine(t, lines, `* FLAGS (\Seen \Answered \Flagged \Deleted \Draft)`)
	requireLine(t, lines, `* OK [PERMANENTFLAGS (\Seen \Answered \Flagged \Deleted \Draft)] Limited`)
	for _, l := range lines {
		if strings.Contains(l, `\*`) {
			t.Fatalf("selection advertises keyword creation: %q", l)
		}
	}

	for i, cmd := range []string{
		`STORE 1 +FLAGS ($Forwarded)`,
		`STORE 2 FLAGS (\Answered NonJunk \Draft)`,
		`UID STORE 90 +FLAGS ($Label1 Work)`,
	} {
		tag := fmt.Sprintf("K%d", i)
		lines, tagged := c.command(tag, "%s", cmd)
		requireOK(t, tagged, tag)
		for _, l := range lines {
			for _, kw := range []string{"$Forwarded", "NonJunk", "$Label1", "Work"} {
				if strings.Contains(l, kw) {
					t.Fatalf("%s reported unstored keyword: %q", cmd, l)
				}
			}
		}
	}
	want := []string{
		`* 1 FETCH (UID 10 FLAGS (\Seen \Flagged))`,
		`* 2 FETCH (UID 40 FLAGS (\Answered \Draft))`,
		`* 3 FETCH (UID 90 FLAGS ())`,
	}
	lines, tagged = c.command("F1", "FETCH 1:* FLAGS")
	requireOK(t, tagged, "F1")
	requireExactly(t, lines, want...)

	lines, tagged = c.command("S2", "SELECT INBOX")
	requireOK(t, tagged, "S2")
	requireLine(t, lines, `* OK [PERMANENTFLAGS (\Seen \Answered \Flagged \Deleted \Draft)] Limited`)
	lines, tagged = c.command("F2", "FETCH 1:* FLAGS")
	requireOK(t, tagged, "F2")
	requireExactly(t, lines, want...)
}

// TestSearchKeywordKeys covers RFC 3501 §6.4.4 KEYWORD and UNKEYWORD. No
// keyword can be stored, so KEYWORD matches nothing and UNKEYWORD matches
// every message, even after a client STOREs that keyword. The operand must be
// a flag-keyword atom; anything else is BAD and leaves the session usable.
//
// checklist: IMAP-SEARCH-11
func TestSearchKeywordKeys(t *testing.T) {
	for _, selection := range []string{"SELECT", "EXAMINE"} {
		for _, empty := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/empty=%t", selection, empty), func(t *testing.T) {
				box := newFetchMailbox()
				box.messages = keywordMessages()
				if empty {
					box.messages = nil
				}
				c := fetchClient(t, box, selection)
				if selection == "SELECT" && !empty {
					_, tagged := c.command("K1", "STORE 1:* +FLAGS ($Forwarded Work)")
					requireOK(t, tagged, "K1")
				}
				for _, tc := range []struct{ query, seqs, uids string }{
					{"KEYWORD $Forwarded", "", ""},
					{"KEYWORD Work", "", ""},
					{"kEyWoRd work", "", ""},
					{"UNKEYWORD $Forwarded", "1 2 3", "10 40 90"},
					{"uNkEyWoRd Junk", "1 2 3", "10 40 90"},
					{"NOT KEYWORD Work", "1 2 3", "10 40 90"},
					{"NOT UNKEYWORD Work", "", ""},
					{"OR KEYWORD Work SEEN", "1", "10"},
					{"UNKEYWORD Work ANSWERED", "2", "40"},
					{"(UNKEYWORD a UNKEYWORD b) UID 40:*", "2 3", "40 90"},
					{"KEYWORD !#$&'+,-./:;<=>?@^_`|~", "", ""},
					{"UNKEYWORD !#$&'+,-./:;<=>?@^_`|~ 3", "3", "90"},
				} {
					t.Run(tc.query, func(t *testing.T) {
						seqs, uids := tc.seqs, tc.uids
						if empty {
							seqs, uids = "", ""
						}
						requireSearch(t, c, "SEARCH "+tc.query, strings.TrimSpace("* SEARCH "+seqs))
						requireSearch(t, c, "UID SEARCH "+tc.query, strings.TrimSpace("* SEARCH "+uids))
					})
				}
				for i, bad := range []string{
					"KEYWORD",
					"UNKEYWORD",
					`KEYWORD "Work"`,
					`KEYWORD \Seen`,
					`UNKEYWORD \Flagged`,
					"KEYWORD Wo]rk",
					"KEYWORD Wo*rk",
					"KEYWORD Wo%rk",
					"KEYWORD Wo{rk",
					"KEYWORD (Work)",
					"UNKEYWORD )",
					"KEYWORD caf\xc3\xa9",
				} {
					for _, prefix := range []string{"", "UID "} {
						tag := fmt.Sprintf("B%d", i)
						lines, tagged := c.command(tag, "%sSEARCH %s", prefix, bad)
						if !strings.HasPrefix(tagged, tag+" BAD") {
							t.Fatalf("%sSEARCH %s: %q, want BAD", prefix, bad, tagged)
						}
						requireExactly(t, lines)
					}
				}
				requireSearch(t, c, "SEARCH ALL", strings.TrimSpace("* SEARCH "+map[bool]string{false: "1 2 3", true: ""}[empty]))
				if len(box.fetches) != 0 {
					t.Fatalf("keyword search fetched content: %v", box.fetches)
				}
			})
		}
	}
}
