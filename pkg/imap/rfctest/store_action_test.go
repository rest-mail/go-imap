package rfctest

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestStoreInvalidAction proves store-att-flags names are ASCII ABNF terminals,
// with no surrounding whitespace, rather than Unicode-folded or trimmed names.
// Invalid actions must fail before writes, session changes or untagged replies.
// checklist: IMAP-STORE-13
func TestStoreInvalidAction(t *testing.T) {
	actions := []string{"FLAGſ", "+FLAGſ", "-FLAGſ", "FLAGS.ſILENT", "FLAGS.SILENſ", "FLAGS.SILENT.SILENT", "FLAGSx", "+FLAGSx", "-FLAGSx", "FLAGS\t", "\tFLAGS", " FLAGS", "FLAGS\u00a0", "\u00a0FLAGS", "FLAGS\v", "\fFLAGS"}
	for _, target := range []string{"STORE 2", "UID STORE 4002"} {
		for _, action := range actions {
			t.Run(fmt.Sprintf("%s/%q", target, action), func(t *testing.T) {
				box := newStoreMailbox()
				c := storeClient(t, box)
				_, tagged := c.command("P1", `STORE 2 +FLAGS.SILENT (\Deleted)`)
				requireOK(t, tagged, "P1")
				before, tagged := c.command("P2", "FETCH 1:* FLAGS")
				requireOK(t, tagged, "P2")
				clear(box.updates)
				lines, tagged := c.command("A1", `%s %s (\Flagged \Deleted)`, target, action)
				if !strings.HasPrefix(tagged, "A1 BAD ") || len(lines) != 0 {
					t.Errorf("invalid action: replies=%q tagged=%q, want only BAD", lines, tagged)
				}
				if len(box.updates) != 0 {
					t.Errorf("invalid action wrote flags: %+v", box.updates)
				}
				after, tagged := c.command("F1", "FETCH 1:* FLAGS")
				requireOK(t, tagged, "F1")
				if !slices.Equal(after, before) {
					t.Errorf("invalid action changed flags: got %q want %q", after, before)
				}
				_, tagged = c.command("N1", `STORE 1 +FLAGS.SILENT (\Draft)`)
				requireOK(t, tagged, "N1")
				if len(box.updates[4001]) != 1 {
					t.Errorf("recovery write: %+v", box.updates)
				}
			})
		}
	}
}

// TestStoreActionCaseVariants proves ASCII case-insensitivity for all six
// actions through both STORE forms, including actual flags and silent replies.
// checklist: IMAP-STORE-13
func TestStoreActionCaseVariants(t *testing.T) {
	for _, target := range []string{"STORE 4", "UID STORE 4004"} {
		for _, action := range []string{"fLaGs", "+fLaGs", "-fLaGs", "fLaGs.sIlEnT", "+fLaGs.sIlEnT", "-fLaGs.sIlEnT"} {
			t.Run(target+"/"+action, func(t *testing.T) {
				box := newStoreMailbox()
				c := storeClient(t, box)
				lines, tagged := c.command("A1", `%s %s (\Flagged)`, target, action)
				requireOK(t, tagged, "A1")
				want := `\Flagged`
				switch action[0] {
				case '+':
					want = `\Seen \Flagged`
				case '-':
					want = `\Seen`
				}
				uid := ""
				if strings.HasPrefix(target, "UID ") {
					uid = "UID 4004 "
				}
				if strings.HasSuffix(strings.ToUpper(action), ".SILENT") {
					requireExactly(t, lines)
				} else {
					requireExactly(t, lines, fmt.Sprintf("* 4 FETCH (%sFLAGS (%s))", uid, want))
				}
				lines, tagged = c.command("F1", "FETCH 4 FLAGS")
				requireOK(t, tagged, "F1")
				requireExactly(t, lines, fmt.Sprintf("* 4 FETCH (UID 4004 FLAGS (%s))", want))
				if len(box.updates[4004]) != 1 {
					t.Errorf("backend updates: %+v", box.updates)
				}
			})
		}
	}
}
