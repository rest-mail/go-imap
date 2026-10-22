package rfctest

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/rest-mail/go-imap"
)

type specialUseMailbox struct {
	mailbox
	folders []imap.Folder
	err     error
}

func (m *specialUseMailbox) Folders() ([]imap.Folder, error) {
	return slices.Clone(m.folders), m.err
}

// RFC 6154 permits special-use attributes on ordinary LIST without advertising
// the separate extended-LIST capability. Each fixture is a host-owned role;
// these tests prove the engine's name-based mapping, not storage policy.
//
// checklist: IMAP-SPECIALUSE-01 IMAP-SPECIALUSE-02 IMAP-SPECIALUSE-03 IMAP-SPECIALUSE-04
func TestSpecialUseOrdinaryList(t *testing.T) {
	for _, selected := range []bool{false, true} {
		m := &specialUseMailbox{folders: []imap.Folder{
			{Name: "INBOX"}, {Name: "Sent"}, {Name: "Drafts"},
			{Name: "Trash"}, {Name: "Junk"}, {Name: "Projects"},
		}}
		c := selectionClient(t, m)
		if selected {
			_, st := c.command("s", "SELECT INBOX")
			requireOK(t, st, "s")
		}
		lines, st := c.command("l", `LIST "" "*"`)
		requireOK(t, st, "l")
		requireExactly(t, lines,
			`* LIST () "/" "INBOX"`,
			`* LIST (\Sent) "/" "Sent"`,
			`* LIST (\Drafts) "/" "Drafts"`,
			`* LIST (\Trash) "/" "Trash"`,
			`* LIST (\Junk) "/" "Junk"`,
			`* LIST () "/" "Projects"`)
		// An exact-name pattern retains the attribute; no other role leaks in.
		for _, name := range []string{"Sent", "Drafts", "Trash", "Junk"} {
			lines, st = c.command("p", `LIST "" "%s"`, name)
			requireOK(t, st, "p")
			requireExactly(t, lines, `* LIST (\`+name+`) "/" "`+name+`"`)
		}
	}
}

// checklist: IMAP-SPECIALUSE-05
func TestSpecialUseCapabilityBoundary(t *testing.T) {
	c := dial(t)
	c.line()
	for _, state := range []string{"unauthenticated", "authenticated", "selected"} {
		if state == "authenticated" {
			c.login()
		}
		if state == "selected" {
			_, st := c.command("s", "SELECT INBOX")
			requireOK(t, st, "s")
		}
		caps := c.capabilities("c")
		for _, unsupported := range []string{"SPECIAL-USE", "CREATE-SPECIAL-USE"} {
			if slices.Contains(caps, unsupported) {
				t.Fatalf("%s advertises unsupported %s: %q", state, unsupported, caps)
			}
		}
	}
}

// checklist: IMAP-SPECIALUSE-06
func TestSpecialUseDoesNotInventFolders(t *testing.T) {
	m := &specialUseMailbox{}
	c := selectionClient(t, m)
	for _, pattern := range []string{"*", "Sent", "Drafts", "Trash", "Junk"} {
		lines, st := c.command("l", `LIST "" "%s"`, pattern)
		requireOK(t, st, "l")
		requireExactly(t, lines)
	}
	// Names alone do not create virtual/role mailboxes or map near matches.
	m.folders = []imap.Folder{{Name: "sent"}, {Name: "Work/Sent"}, {Name: "Archive"}, {Name: "All"}, {Name: "Flagged"}, {Name: `Sent "old"`}}
	lines, st := c.command("l", `LIST "" "*"`)
	requireOK(t, st, "l")
	requireExactly(t, lines,
		`* LIST () "/" "sent"`, `* LIST () "/" "Work/Sent"`,
		`* LIST () "/" "Archive"`, `* LIST () "/" "All"`,
		`* LIST () "/" "Flagged"`, `* LIST () "/" "Sent \"old\""`)
	m.err = errors.New("private folder failure")
	lines, st = c.command("l", `LIST "" "*"`)
	if !strings.HasPrefix(st, "l NO ") {
		t.Fatalf("folder failure: %q", st)
	}
	requireExactly(t, lines)
	m.err = nil
	lines, st = c.command("l", `LIST "" "Sent"`)
	requireOK(t, st, "l")
	requireExactly(t, lines)
}
