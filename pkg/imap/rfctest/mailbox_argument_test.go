package rfctest

import (
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/rest-mail/go-imap/pkg/imap"
)

type mailboxArgumentSpy struct {
	mailbox
	mu       sync.Mutex
	name     string
	activity []string
}

func (m *mailboxArgumentSpy) record(call string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activity = append(m.activity, call)
}

func (m *mailboxArgumentSpy) takeActivity() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	got := append([]string(nil), m.activity...)
	m.activity = nil
	slices.Sort(got)
	return got
}

func (m *mailboxArgumentSpy) Folders() ([]imap.Folder, error) {
	m.record("folders")
	return []imap.Folder{{Name: "INBOX"}, {Name: m.name}, {Name: m.name + "/child"}, {Name: m.name + "side"}}, nil
}

func (m *mailboxArgumentSpy) Messages(folder string) ([]imap.Message, error) {
	m.record("messages:" + folder)
	uid := map[string]uint32{"INBOX": 1, m.name: 7, m.name + "/child": 8, m.name + "side": 9}[folder]
	if uid == 0 {
		return nil, nil
	}
	return []imap.Message{{UID: uid, Size: 24}}, nil
}

func (m *mailboxArgumentSpy) Move(uid uint32, name string) error {
	m.record(fmt.Sprintf("move:%d>%s", uid, name))
	return nil
}

func mailboxArgumentClient(t *testing.T, name string) (*client, *mailboxArgumentSpy) {
	t.Helper()
	box := &mailboxArgumentSpy{name: name}
	c := dialMailbox(t, box)
	c.line()
	c.login()
	return c, box
}

// Each grammar consumes exactly its mailbox arguments before touching the
// backend. A BAD command keeps the selected mailbox usable for a later FETCH.
//
// checklist: IMAP-MBOX-ARGS-01
func TestMailboxManagementArgumentGrammar(t *testing.T) {
	single := []string{"Work extra", `"Work" extra`, " Work", "Work ", "Work  ", `"Work"x`, `"Work`, "Work)", "Work%", "(Work)", "Work\t", "Work\x7f", `"" ""`, `"bad\q"`}
	for _, command := range []string{"CREATE", "DELETE", "SUBSCRIBE", "UNSUBSCRIBE", "RENAME"} {
		t.Run(command, func(t *testing.T) {
			c, box := mailboxArgumentClient(t, "Work")
			_, tagged := c.command("S1", "SELECT INBOX")
			requireOK(t, tagged, "S1")
			box.takeActivity()
			bad := single
			if command == "RENAME" {
				bad = []string{"Work", "Work New extra", `"Work"x New`, `Work "New"x`, `"Work" "New" extra`, " Work New", "Work  New", "Work New ", "Work (New)", "(Work) New", `Work "bad\q"`}
			}
			for _, args := range bad {
				lines, tagged := c.command("B1", "%s %s", command, args)
				if len(lines) != 0 || !strings.HasPrefix(tagged, "B1 BAD ") {
					t.Errorf("%s %q: %q %q, want BAD without data", command, args, lines, tagged)
				}
				if calls := box.takeActivity(); len(calls) != 0 {
					t.Errorf("malformed arguments reached backend: %v", calls)
				}
			}
			lines, tagged := c.command("F1", "FETCH 1 UID")
			requireOK(t, tagged, "F1")
			requireExactly(t, lines, "* 1 FETCH (UID 1 FLAGS ())")
		})
	}
}

// RENAME has two complete astrings. Spaces inside either quoted or counted
// argument are data, as are escaped quotes and backslashes. Inferiors follow
// exactly the decoded source; a near name sharing only its prefix stays put.
//
// checklist: IMAP-MBOX-ARGS-02
func TestRenameMailboxAstringIdentity(t *testing.T) {
	for _, form := range []string{"atom", "quoted", "literal source", "literal destination", "two literals", "non-synchronizing literals"} {
		t.Run(form, func(t *testing.T) {
			old, next := `Old "quoted"\box`, `New "quoted"\box`
			if form == "atom" {
				old, next = "Work", "Play"
			}
			c, box := mailboxArgumentClient(t, old)
			switch form {
			case "atom", "quoted":
				from, to := strconv.Quote(old), strconv.Quote(next)
				if form == "atom" {
					from, to = old, next
				}
				_, tagged := c.command("R1", "RENAME %s %s", from, to)
				requireOK(t, tagged, "R1")
			case "literal source":
				sendLiteral(t, c, "R1 RENAME ", old, " "+strconv.Quote(next))
				requireOK(t, c.line(), "R1")
			case "literal destination":
				sendLiteral(t, c, "R1 RENAME "+strconv.Quote(old)+" ", next, "")
				requireOK(t, c.line(), "R1")
			case "two literals":
				if _, err := fmt.Fprintf(c.conn, "R1 RENAME {%d}\r\n", len(old)); err != nil {
					t.Fatal(err)
				}
				if got := c.line(); !strings.HasPrefix(got, "+ ") {
					t.Fatalf("source continuation: %q", got)
				}
				if _, err := fmt.Fprintf(c.conn, "%s {%d}\r\n", old, len(next)); err != nil {
					t.Fatal(err)
				}
				if got := c.line(); !strings.HasPrefix(got, "+ ") {
					t.Fatalf("destination continuation: %q", got)
				}
				if _, err := io.WriteString(c.conn, next+"\r\n"); err != nil {
					t.Fatal(err)
				}
				requireOK(t, c.line(), "R1")
			case "non-synchronizing literals":
				writeLiteralWire(t, c, fmt.Sprintf("R1 RENAME {%d+}\r\n%s {%d+}\r\n%s\r\n", len(old), old, len(next), next))
				requireOK(t, c.line(), "R1")
			}
			want := []string{"folders", "messages:" + old, "messages:" + old + "/child", "move:7>" + next, "move:8>" + next + "/child"}
			slices.Sort(want)
			if got := box.takeActivity(); !slices.Equal(got, want) {
				t.Fatalf("decoded rename activity = %q, want %q", got, want)
			}
			_, tagged := c.command("N1", "NOOP")
			requireOK(t, tagged, "N1")
		})
	}
}

// Single-name commands accept both wire string forms without trimming spaces
// inside the name. CREATE/subscription persistence remains a backend limitation;
// DELETE's actual lookup and Move prove that it receives the decoded identity.
//
// checklist: IMAP-MBOX-ARGS-03
func TestMailboxManagementStringForms(t *testing.T) {
	for _, command := range []string{"CREATE", "DELETE", "SUBSCRIBE", "UNSUBSCRIBE"} {
		for _, form := range []string{"quoted", "literal", "non-synchronizing literal"} {
			t.Run(command+"/"+form, func(t *testing.T) {
				name := ` Work "quoted"\box `
				c, box := mailboxArgumentClient(t, name)
				switch form {
				case "quoted":
					_, tagged := c.command("A1", "%s %s", command, strconv.Quote(name))
					requireOK(t, tagged, "A1")
				case "literal":
					sendLiteral(t, c, "A1 "+command+" ", name, "")
					requireOK(t, c.line(), "A1")
				default:
					writeLiteralWire(t, c, fmt.Sprintf("A1 %s {%d+}\r\n%s\r\n", command, len(name), name))
					requireOK(t, c.line(), "A1")
				}
				want := []string(nil)
				if command == "DELETE" {
					want = []string{"messages:" + name, "move:7>Trash"}
				}
				if got := box.takeActivity(); !slices.Equal(got, want) {
					t.Fatalf("%s string identity: %q, want %q", command, got, want)
				}
			})
		}
	}
}

// RFC3501 section9's RENAME production explicitly forbids INBOX as the
// destination. Its case-insensitive identity applies to every astring form.
//
// checklist: IMAP-RENAME-05
func TestRenameIntoInboxIsRejected(t *testing.T) {
	for _, name := range []string{"INBOX", "inbox", "InBoX"} {
		for _, form := range []string{"atom", "quoted", "literal"} {
			t.Run(name+"/"+form, func(t *testing.T) {
				c, box := mailboxArgumentClient(t, "Work")
				var tagged string
				if form == "literal" {
					sendLiteral(t, c, "I1 RENAME Work ", name, "")
					tagged = c.line()
				} else {
					arg := name
					if form == "quoted" {
						arg = strconv.Quote(name)
					}
					_, tagged = c.command("I1", "RENAME Work %s", arg)
				}
				if !strings.HasPrefix(tagged, "I1 NO ") || len(box.takeActivity()) != 0 {
					t.Fatalf("renaming to INBOX must fail before backend work: %q", tagged)
				}
				_, tagged = c.command("N1", "NOOP")
				requireOK(t, tagged, "N1")
			})
		}
	}
}
