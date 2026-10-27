package rfctest

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/rest-mail/go-imap/pkg/imap"
)

// Reporting supplies a host's analysis pipeline with a structured failure
// event, never the password, encoded SASL payload or backend failure detail.
// Account blocking, password strength and spraying analysis remain host duties.
// checklist: IMAP-SECURITY-09
func TestAuthenticationFailureReporting(t *testing.T) {
	var log bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	const password = "fixture-password-not-for-logging"
	const detail = "fixture-private-backend-diagnostic"
	payload := plainResponse(testUser, password)
	b := &privateFailureBackend{reason: detail}
	c := securityClient(t, b, imap.NopLimiter{})
	_, tagged := c.command("A1", "LOGIN %s %s", testUser, password)
	if tagged != "A1 NO [AUTHENTICATIONFAILED] Invalid credentials" {
		t.Fatalf("LOGIN: %q", tagged)
	}
	for _, initial := range []bool{false, true} {
		var reply string
		if initial {
			_, reply = c.command("A2", "AUTHENTICATE PLAIN %s", payload)
		} else {
			reply = c.authenticate("A2", "PLAIN", payload)
		}
		if reply != "A2 NO [AUTHENTICATIONFAILED] Invalid credentials" {
			t.Fatalf("AUTHENTICATE initial=%v: %q", initial, reply)
		}
	}
	_ = c.conn.Close()
	<-c.done // Join the writer before reading the captured structured events.
	for _, secret := range []string{password, payload, detail} {
		if strings.Contains(log.String(), secret) {
			t.Fatal("credential or private backend diagnostic reached the log")
		}
	}
	decoder := json.NewDecoder(&log)
	failures := 0
	for {
		var event map[string]any
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event["event"] != "imap_auth_failed" {
			continue
		}
		if event["user"] != testUser || event["remote"] == nil || event["ip"] == nil {
			t.Fatalf("failure event lacks analysis context: %v", event)
		}
		failures++
	}
	if failures != 3 || b.calls != 3 {
		t.Fatalf("reported=%d backend=%d, want3 each", failures, b.calls)
	}
}
