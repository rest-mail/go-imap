package rfctest

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/rest-mail/go-imap/pkg/imap"
)

// checklist: IMAP-RESP-AUDIT-01
func TestEmptyCommandBADTranscript(t *testing.T) {
	for _, state := range []string{"not authenticated", "authenticated", "selected"} {
		t.Run(state, func(t *testing.T) {
			c := dial(t)
			c.line()
			if state != "not authenticated" {
				c.login()
			}
			if state == "selected" {
				if _, done := c.command("S1", "SELECT INBOX"); !strings.HasPrefix(done, "S1 OK ") {
					t.Fatal(done)
				}
			}
			// Pipeline a following command so a silently ignored blank cannot hang
			// this proof or be mistaken for an acceptable lack of response.
			if _, err := fmt.Fprint(c.conn, "\r\nN1 NOOP\r\n"); err != nil {
				t.Fatal(err)
			}
			if got := c.line(); !strings.HasPrefix(got, "* BAD ") {
				t.Fatalf("blank command: %q, want untagged BAD", got)
			}
			if got := c.line(); !strings.HasPrefix(got, "N1 OK ") {
				t.Fatalf("next command: %q", got)
			}
			if state == "selected" {
				if _, done := c.command("F1", "FETCH 1 (UID)"); !strings.HasPrefix(done, "F1 OK ") {
					t.Fatalf("selection lost: %q", done)
				}
			}
			if state == "authenticated" {
				if _, done := c.command("B1", `LIST "" *`); !strings.HasPrefix(done, "B1 OK ") {
					t.Fatalf("authentication lost: %q", done)
				}
			}
			if state == "not authenticated" && c.backend.calls != 0 {
				t.Fatal("blank command authenticated")
			}
		})
	}
}

type responseFailureMailbox struct{ mailbox }

func (responseFailureMailbox) Fetch(uint32) ([]byte, error) {
	return nil, errors.New("synthetic backend response failure")
}

// checklist: IMAP-RESP-AUDIT-02
func TestStatusCompletionTranscript(t *testing.T) {
	c := dialMailbox(t, responseFailureMailbox{})
	c.line()
	c.login()
	if _, done := c.command("S1", "SELECT INBOX"); !strings.HasPrefix(done, "S1 OK ") {
		t.Fatal(done)
	}
	for _, step := range []struct{ tag, command, status string }{
		{"B1", "UNKNOWN", "BAD"}, {"F1", "FETCH 1 BODY[]", "NO"}, {"N1", "NOOP", "OK"},
	} {
		lines, done := c.command(step.tag, "%s", step.command)
		if !strings.HasPrefix(done, step.tag+" "+step.status+" ") {
			t.Fatalf("%s: %q", step.command, done)
		}
		if strings.Contains(strings.Join(append(lines, done), "\n"), "synthetic backend response failure") {
			t.Fatal("backend diagnostic in response")
		}
	}
	if _, done := c.command("U1", "FETCH 1 (UID)"); !strings.HasPrefix(done, "U1 OK ") {
		t.Fatalf("operational failure lost selection: %q", done)
	}
}

type responsePanicMailbox struct{ mailbox }

func (responsePanicMailbox) Folders() ([]imap.Folder, error) {
	panic("synthetic response panic context")
}

// checklist: IMAP-RESP-14
func TestPanicByeTranscript(t *testing.T) {
	c := dialMailbox(t, responsePanicMailbox{})
	c.line()
	c.login()
	if _, err := fmt.Fprint(c.conn, "P1 LIST \"\" *\r\n"); err != nil {
		t.Fatal(err)
	}
	got := c.line()
	if !strings.HasPrefix(got, "* BYE ") || strings.Contains(got, "synthetic response panic context") {
		t.Fatalf("panic response: %q", got)
	}
	if !c.closed() {
		t.Fatal("panic BYE was not followed immediately by EOF")
	}
}

type responseDeadlineConn struct{ net.Conn }

func (c responseDeadlineConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if string(p[:n]) == "T1 timeout\r\n" {
		return 0, os.ErrDeadlineExceeded
	}
	return n, err
}

// The transport seam returns the same deadline error as an inactivity expiry.
// This proves wire shutdown handling, not that a real 30-minute timer elapsed.
// checklist: IMAP-RESP-14
func TestAutologoutByeTranscript(t *testing.T) {
	c := dialConn(t, nil, func(conn net.Conn) net.Conn { return responseDeadlineConn{conn} })
	c.line()
	c.login()
	if _, err := fmt.Fprint(c.conn, "T1 timeout\r\n"); err != nil {
		t.Fatal(err)
	}
	if got := c.line(); !strings.HasPrefix(got, "* BYE ") {
		t.Fatalf("autologout response: %q", got)
	}
	if !c.closed() {
		t.Fatal("autologout BYE was not followed immediately by EOF")
	}
}
