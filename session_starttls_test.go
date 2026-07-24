package imap

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"net"
	"strings"
	"testing"
)

// plainCredential builds the base64 SASL PLAIN payload (authzid\0authcid\0passwd).
func plainCredential(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte("\x00" + user + "\x00" + pass))
}

// newIMAPHarnessTLS drives a Session whose *tls.Config is non-nil (STARTTLS
// available). usingTLS selects whether the connection is already protected
// (implicit-TLS / post-STARTTLS) or still cleartext. The config carries no real
// certificate because these tests never run a handshake — the pre-auth gate only
// inspects whether tlsConfig is set and whether the link is already TLS.
func newIMAPHarnessTLS(t *testing.T, backend Backend, usingTLS bool) *imapHarness {
	t.Helper()
	client, server := net.Pipe()
	sess := NewSession(server, backend, "imap.test", &tls.Config{}, NopLimiter{})
	sess.usingTLS = usingTLS

	done := make(chan struct{})
	go func() {
		defer close(done)
		sess.Handle()
	}()

	h := &imapHarness{
		t:    t,
		conn: client,
		cr:   bufio.NewReader(client),
		cw:   bufio.NewWriter(client),
		done: done,
	}
	t.Cleanup(func() {
		_ = client.Close()
		<-h.done
	})

	if g := h.readLine(); !strings.HasPrefix(g, "* OK") {
		t.Fatalf("greeting = %q, want * OK...", g)
	}
	return h
}

// AUTHENTICATE PLAIN on a cleartext connection, when TLS is configured, must be
// refused with [PRIVACYREQUIRED] BEFORE any credential is solicited — the server
// must never emit the "+" continuation that would put the base64 password on the
// wire. This mirrors the LOGIN pre-TLS gate; withholding AUTH=PLAIN from the
// advertised CAPABILITY list is not enforcement on its own.
func TestIMAP_AuthenticateRefusedBeforeSTARTTLS(t *testing.T) {
	m := newMockBackend()
	h := newIMAPHarnessTLS(t, m, false)

	// Read the response directly (not via command()) so we can assert the server
	// answered with a tagged refusal and did NOT send a "+" continuation.
	h.send("a1 AUTHENTICATE PLAIN")
	resp := h.readLine()

	if strings.HasPrefix(resp, "+") {
		t.Fatalf("server solicited credentials over cleartext (got continuation %q); "+
			"the base64 password would go out in the clear", resp)
	}
	if !strings.HasPrefix(resp, "a1 NO") || !strings.Contains(resp, "[PRIVACYREQUIRED]") {
		t.Fatalf("cleartext AUTHENTICATE = %q, want \"a1 NO [PRIVACYREQUIRED] ...\"", resp)
	}

	// No credential was accepted: a command requiring auth is still refused.
	_, status := h.command("a2", "SELECT INBOX")
	if !strings.Contains(status, " NO") {
		t.Fatalf("SELECT after refused AUTHENTICATE = %q, want NO (not authenticated)", status)
	}
}

// With no TLS config at all (development mode) AUTHENTICATE PLAIN still works:
// the gate keys off a configured-but-inactive TLS, not off AUTHENTICATE itself.
func TestIMAP_AuthenticateWorksWithoutTLSConfig(t *testing.T) {
	m := newMockBackend()
	h := newIMAPHarness(t, m) // nil tlsConfig

	h.send("a1 AUTHENTICATE PLAIN")
	if got := h.readLine(); got != "+" {
		t.Fatalf("continuation = %q, want %q", got, "+")
	}
	h.send("%s", plainCredential(m.user, m.pass))
	if status := h.readLine(); !strings.Contains(status, "a1 OK") {
		t.Fatalf("AUTHENTICATE PLAIN (no TLS config) = %q, want a1 OK", status)
	}
}

// With TLS configured AND the connection already protected (implicit-TLS or
// post-STARTTLS), AUTHENTICATE PLAIN works: the pre-TLS gate is bypassed once
// usingTLS is true, so the fix does not lock out legitimate protected sessions.
func TestIMAP_AuthenticateWorksAfterTLS(t *testing.T) {
	m := newMockBackend()
	h := newIMAPHarnessTLS(t, m, true) // TLS configured and active

	h.send("a1 AUTHENTICATE PLAIN")
	if got := h.readLine(); got != "+" {
		t.Fatalf("continuation = %q, want %q", got, "+")
	}
	h.send("%s", plainCredential(m.user, m.pass))
	if status := h.readLine(); !strings.Contains(status, "a1 OK") {
		t.Fatalf("AUTHENTICATE PLAIN over TLS = %q, want a1 OK", status)
	}
}
