package imap

import (
	"strings"
	"testing"
)

// TestCommandLineLengthLimit proves an unauthenticated client cannot force
// unbounded buffering by sending an arbitrarily long command line. Before the
// fix, ReadString('\n') accumulated the whole line and it was parsed as a normal
// command (here CAPABILITY, whose trailing args are ignored) — so the server
// answered OK and had buffered the entire payload. After the fix, a line over
// MaxCommandLineLength is rejected with a BAD "too long" response without the
// whole line ever being retained.
func TestCommandLineLengthLimit(t *testing.T) {
	mock := newMockBackend()
	h := newIMAPHarness(t, mock) // consumes the greeting

	// 64 KiB of padding on an otherwise valid command line — far over any
	// sane cap. Literal is hardcoded (not MaxCommandLineLength) so this test
	// compiles and demonstrates RED against the pre-fix code too.
	huge := strings.Repeat("A", 64*1024)
	h.send("a1 CAPABILITY %s", huge)

	line := h.readLine()
	if !strings.Contains(line, "BAD") || !strings.Contains(strings.ToLower(line), "too long") {
		t.Fatalf("over-long command line: got %q, want a BAD ... too long rejection", line)
	}

	// The connection must remain usable: a normal-length command still works.
	if _, status := h.command("a2", "CAPABILITY"); !strings.Contains(status, "OK") {
		t.Fatalf("CAPABILITY after over-long line: status = %q, want OK", status)
	}
}

// TestCommandLineWithinLimitAccepted guards against over-restriction: a command
// line comfortably under the cap is processed normally.
func TestCommandLineWithinLimitAccepted(t *testing.T) {
	mock := newMockBackend()
	h := newIMAPHarness(t, mock)

	pad := strings.Repeat("A", 1024) // ~1 KiB, well under the 8 KiB default
	untagged, status := h.command("a1", "CAPABILITY %s", pad)
	if !strings.Contains(status, "OK") {
		t.Fatalf("under-limit CAPABILITY: status = %q, want OK", status)
	}
	if len(untagged) == 0 || !strings.HasPrefix(untagged[0], "* CAPABILITY") {
		t.Fatalf("under-limit CAPABILITY: untagged = %v, want a * CAPABILITY line", untagged)
	}
}
