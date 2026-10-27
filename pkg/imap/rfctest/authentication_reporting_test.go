package rfctest

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
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
	slog.SetDefault(slog.New(slog.NewJSONHandler(&log, &slog.HandlerOptions{Level: slog.LevelDebug})))
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
	// A counted password is not logged while the literal is materialized.
	sendLiteral(t, c, "A3 LOGIN "+testUser+" ", password, "")
	if reply := c.line(); reply != "A3 NO [AUTHENTICATIONFAILED] Invalid credentials" {
		t.Fatalf("literal LOGIN: %q", reply)
	}
	for _, command := range []string{
		"LOGIN " + testUser + " " + password + " extra",
		"AUTHENTICATE PLAIN " + payload + " extra",
		"UNKNOWN " + password,
	} {
		if _, reply := c.command("B1", "%s", command); !strings.HasPrefix(reply, "B1 BAD ") {
			t.Fatalf("malformed command not refused: %q", reply)
		}
	}
	// Logging occurs before tag/command validation: even an unparseable input
	// or a credential placed in a tag/command position must not be copied.
	for _, line := range []string{password, password + "+ NOOP", "B2 " + password} {
		if _, err := fmt.Fprint(c.conn, line+"\r\n"); err != nil {
			t.Fatal(err)
		}
		if reply := c.line(); !strings.Contains(reply, " BAD ") {
			t.Fatalf("invalid command not refused: %q", reply)
		}
	}
	_ = c.conn.Close()
	<-c.done

	// Refused pre-TLS credentials are sensitive too; refusal must precede the
	// backend call while preserving the existing PRIVACYREQUIRED response.
	protected := dialTLS(t, nil, nil, &tls.Config{})
	protected.line()
	if _, reply := protected.command("P1", "LOGIN %s %s", testUser, password); reply != "P1 NO [PRIVACYREQUIRED] STARTTLS required" || protected.backend.calls != 0 {
		t.Fatalf("pre-TLS LOGIN: %q, backend calls=%d", reply, protected.backend.calls)
	}
	_ = protected.conn.Close()
	<-protected.done

	// Successful LOGIN and both PLAIN exchanges retain their ordinary outcome;
	// a later LOGIN is refused without authenticating again or logging its data.
	for _, form := range []string{"LOGIN", "PLAIN initial", "PLAIN continuation"} {
		success := dial(t)
		success.line()
		var reply string
		switch form {
		case "LOGIN":
			_, reply = success.command("S1", "LOGIN %s %s", testUser, testPass)
		case "PLAIN initial":
			_, reply = success.command("S1", "AUTHENTICATE PLAIN %s", plainResponse(testUser, testPass))
		default:
			reply = success.authenticate("S1", "PLAIN", plainResponse(testUser, testPass))
		}
		requireOK(t, reply, "S1")
		if _, reply = success.command("S2", "LOGIN %s %s", testUser, password); !strings.HasPrefix(reply, "S2 BAD ") || success.backend.calls != 1 {
			t.Fatalf("reauthentication: %q, backend calls=%d", reply, success.backend.calls)
		}
		_, reply = success.command("S3", "NOOP")
		requireOK(t, reply, "S3")
		_ = success.conn.Close()
		<-success.done
	}
	// All session writers have joined before reading the captured log buffer.
	for _, secret := range []string{password, payload, detail, testPass, plainResponse(testUser, testPass)} {
		if strings.Contains(log.String(), secret) {
			t.Fatal("credential or private backend diagnostic reached the log")
		}
	}
	decoder := json.NewDecoder(&log)
	failures, traces, successes := 0, 0, 0
	for {
		var event map[string]any
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if event["msg"] == "imap: recv" {
			traces++
			if n, ok := event["octets"].(float64); !ok || n < 1 || event["level"] != "DEBUG" {
				t.Fatal("command trace lacks a positive byte count at debug level")
			}
			for _, field := range []string{"cmd", "args", "tag"} {
				if _, ok := event[field]; ok {
					t.Fatal("command trace contains raw client input")
				}
			}
		}
		if event["msg"] == "imap: authenticated" {
			if event["user"] != testUser || event["remote"] == nil {
				t.Fatal("successful authentication lost identity/source context")
			}
			successes++
		}
		if event["event"] != "imap_auth_failed" {
			continue
		}
		if event["user"] != testUser || event["remote"] == nil || event["ip"] == nil {
			t.Fatalf("failure event lacks analysis context: %v", event)
		}
		failures++
	}
	if failures != 4 || b.calls != 4 || traces == 0 || successes != 3 {
		t.Fatalf("reported=%d backend=%d traces=%d successes=%d, want4 failures/3 successes and positive traces", failures, b.calls, traces, successes)
	}
}
