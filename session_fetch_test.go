package imap

import (
	"strings"
	"testing"
	"time"
)

// parensBalanced reports whether every '(' in s is matched by a ')' and no ')'
// ever precedes its opener — a cheap well-formedness check for the parenthesized
// BODYSTRUCTURE / ENVELOPE structures RFC 3501 §7.4.2 requires.
func parensBalanced(s string) bool {
	depth := 0
	for _, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

// The FETCH macros ALL/FAST/FULL (RFC 3501 §6.4.5) must expand to their
// component data items. ALL = FLAGS INTERNALDATE RFC822.SIZE ENVELOPE.
func TestIMAP_FetchMacro_ALL(t *testing.T) {
	m := newMockBackend()
	seedThree(m)
	h := newIMAPHarness(t, m)
	h.login("m1")
	h.selectInbox("m2")

	untagged, status := h.command("m3", "FETCH 1 ALL")
	if !strings.Contains(status, " OK") {
		t.Fatalf("FETCH 1 ALL status = %q", status)
	}
	resp := strings.Join(untagged, "\n")
	for _, want := range []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE 100", "ENVELOPE ("} {
		if !strings.Contains(resp, want) {
			t.Errorf("FETCH 1 ALL missing %q; got: %q", want, resp)
		}
	}
	if strings.Contains(resp, "BODY") {
		t.Errorf("FETCH 1 ALL must not include BODY; got: %q", resp)
	}
}

// FAST = FLAGS INTERNALDATE RFC822.SIZE (no ENVELOPE, no BODY).
func TestIMAP_FetchMacro_FAST(t *testing.T) {
	m := newMockBackend()
	seedThree(m)
	h := newIMAPHarness(t, m)
	h.login("f1")
	h.selectInbox("f2")

	untagged, status := h.command("f3", "FETCH 1 FAST")
	if !strings.Contains(status, " OK") {
		t.Fatalf("FETCH 1 FAST status = %q", status)
	}
	resp := strings.Join(untagged, "\n")
	for _, want := range []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE 100"} {
		if !strings.Contains(resp, want) {
			t.Errorf("FETCH 1 FAST missing %q; got: %q", want, resp)
		}
	}
	if strings.Contains(resp, "ENVELOPE") {
		t.Errorf("FETCH 1 FAST must not include ENVELOPE; got: %q", resp)
	}
	if strings.Contains(resp, "BODY") {
		t.Errorf("FETCH 1 FAST must not include BODY; got: %q", resp)
	}
}

// FULL = FLAGS INTERNALDATE RFC822.SIZE ENVELOPE BODY (the non-extensible
// BODYSTRUCTURE).
func TestIMAP_FetchMacro_FULL(t *testing.T) {
	m := newMockBackend()
	seedThree(m)
	h := newIMAPHarness(t, m)
	h.login("l1")
	h.selectInbox("l2")

	untagged, status := h.command("l3", "FETCH 1 FULL")
	if !strings.Contains(status, " OK") {
		t.Fatalf("FETCH 1 FULL status = %q", status)
	}
	resp := strings.Join(untagged, "\n")
	for _, want := range []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE 100", "ENVELOPE (", "BODY ("} {
		if !strings.Contains(resp, want) {
			t.Errorf("FETCH 1 FULL missing %q; got: %q", want, resp)
		}
	}
	if !parensBalanced(resp) {
		t.Errorf("FETCH 1 FULL response has unbalanced parens: %q", resp)
	}
}

// FETCH BODYSTRUCTURE must answer with a correctly-formed parenthesized body
// structure — not silently fall through to FLAGS+UID — and must not set \Seen.
func TestIMAP_FetchBodystructure_WellFormed_NoSeen(t *testing.T) {
	m := newMockBackend()
	seedThree(m)
	h := newIMAPHarness(t, m)
	h.login("bs1")
	h.selectInbox("bs2")

	untagged, status := h.command("bs3", "FETCH 1 BODYSTRUCTURE")
	if !strings.Contains(status, " OK") {
		t.Fatalf("FETCH 1 BODYSTRUCTURE status = %q", status)
	}
	resp := strings.Join(untagged, "\n")
	if !strings.Contains(resp, `BODYSTRUCTURE ("TEXT" "PLAIN"`) {
		t.Errorf("BODYSTRUCTURE missing or malformed; got: %q", resp)
	}
	if !parensBalanced(resp) {
		t.Errorf("BODYSTRUCTURE response has unbalanced parens: %q", resp)
	}
	if _, st := h.command("bs4", "NOOP"); !strings.Contains(st, " OK") {
		t.Fatalf("NOOP status = %q", st)
	}
	if m.mbox.wasMarkedRead(5) {
		t.Errorf("FETCH BODYSTRUCTURE must NOT mark \\Seen, but UID 5 was marked read")
	}
}

// The bare BODY item is the non-extensible BODYSTRUCTURE and must likewise be a
// well-formed parenthesized structure that does not set \Seen (distinct from
// BODY[...] section fetches).
func TestIMAP_FetchBody_Structure_NoSeen(t *testing.T) {
	m := newMockBackend()
	seedThree(m)
	h := newIMAPHarness(t, m)
	h.login("bb1")
	h.selectInbox("bb2")

	untagged, status := h.command("bb3", "FETCH 1 BODY")
	if !strings.Contains(status, " OK") {
		t.Fatalf("FETCH 1 BODY status = %q", status)
	}
	resp := strings.Join(untagged, "\n")
	if !strings.Contains(resp, `BODY ("TEXT" "PLAIN"`) {
		t.Errorf("bare BODY structure missing or malformed; got: %q", resp)
	}
	if !parensBalanced(resp) {
		t.Errorf("BODY structure response has unbalanced parens: %q", resp)
	}
	if _, st := h.command("bb4", "NOOP"); !strings.Contains(st, " OK") {
		t.Fatalf("NOOP status = %q", st)
	}
	if m.mbox.wasMarkedRead(5) {
		t.Errorf("FETCH BODY (structure) must NOT mark \\Seen, but UID 5 was marked read")
	}
}

// buildEnvelope must populate the To field from Message.To (the 6th envelope
// field) rather than hardcoding it NIL. cc/bcc/in-reply-to/message-id have no
// source in the Message model and stay NIL (the 4 trailing fields).
func TestBuildEnvelope_ToAddress(t *testing.T) {
	msg := Message{
		Subject: "Hi",
		From:    Address{Name: "Alice", Email: "alice@example.com"},
		To:      "Bob <bob@example.org>",
		Date:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	result := buildEnvelope(msg)

	if !parensBalanced(result) {
		t.Fatalf("envelope has unbalanced parens: %s", result)
	}
	for _, want := range []string{`"Bob"`, `"bob"`, `"example.org"`} {
		if !strings.Contains(result, want) {
			t.Errorf("envelope To not populated with %s: %s", want, result)
		}
	}
	// To is present, so the response must end in exactly 4 NILs
	// (cc bcc in-reply-to message-id), not 5.
	if !strings.HasSuffix(result, "NIL NIL NIL NIL)") {
		t.Errorf("envelope tail should be 4 NILs: %s", result)
	}
	if strings.HasSuffix(result, "NIL NIL NIL NIL NIL)") {
		t.Errorf("To field must not be NIL when Message.To is set: %s", result)
	}
}

// buildAddressList must render every address in a multi-recipient header as its
// own address structure within a single outer list.
func TestBuildAddressList_Multiple(t *testing.T) {
	got := buildAddressList("Bob <bob@example.org>, carol@example.com")
	want := `(("Bob" NIL "bob" "example.org")(NIL NIL "carol" "example.com"))`
	if got != want {
		t.Errorf("buildAddressList = %q, want %q", got, want)
	}
	if buildAddressList("") != "NIL" {
		t.Errorf("empty address list must be NIL")
	}
	if buildAddressList("not a valid @@ address") != "NIL" {
		t.Errorf("unparseable address list must be NIL")
	}
}

// A single text/plain part must yield the exact non-extensible (BODY) and
// extensible (BODYSTRUCTURE) forms of RFC 3501 §7.4.2.
func TestBuildBodyStructure_SingleTextExact(t *testing.T) {
	raw := "Subject: Hi\r\n\r\nbody 5\r\n" // no Content-Type → text/plain; charset US-ASCII
	if got, want := buildBodyStructure(raw, false),
		`("TEXT" "PLAIN" ("CHARSET" "US-ASCII") NIL NIL "7BIT" 8 1)`; got != want {
		t.Errorf("BODY = %q, want %q", got, want)
	}
	if got, want := buildBodyStructure(raw, true),
		`("TEXT" "PLAIN" ("CHARSET" "US-ASCII") NIL NIL "7BIT" 8 1 NIL NIL NIL NIL)`; got != want {
		t.Errorf("BODYSTRUCTURE = %q, want %q", got, want)
	}
}

// A non-text single part carries no line count, and its declared encoding and
// parameters are reported.
func TestBuildBodyStructure_NonText(t *testing.T) {
	raw := "Content-Type: application/pdf; name=doc.pdf\r\n" +
		"Content-Transfer-Encoding: base64\r\n\r\nAAAA\r\n"
	got := buildBodyStructure(raw, false)
	want := `("APPLICATION" "PDF" ("NAME" "doc.pdf") NIL NIL "BASE64" 6)`
	if got != want {
		t.Errorf("BODY = %q, want %q", got, want)
	}
}

// A multipart entity nests its parts, then subtype, then extension data. The
// parts are adjacent (no separator) and the whole structure is well-formed.
func TestBuildBodyStructure_Multipart(t *testing.T) {
	raw := "Content-Type: multipart/alternative; boundary=BB\r\n\r\n" +
		"--BB\r\nContent-Type: text/plain\r\n\r\nhi\r\n" +
		"--BB\r\nContent-Type: text/html; charset=utf-8\r\n\r\n<b>hi</b>\r\n" +
		"--BB--\r\n"
	got := buildBodyStructure(raw, true)
	if !parensBalanced(got) {
		t.Fatalf("multipart structure unbalanced: %s", got)
	}
	if !strings.Contains(got, `"ALTERNATIVE"`) {
		t.Errorf("multipart subtype missing: %s", got)
	}
	if !strings.Contains(got, `"TEXT" "PLAIN"`) || !strings.Contains(got, `"TEXT" "HTML"`) {
		t.Errorf("multipart parts missing: %s", got)
	}
	// Nested parts are adjacent: a closing paren immediately followed by an open.
	if !strings.Contains(got, ")(") {
		t.Errorf("multipart parts should be adjacent with no separator: %s", got)
	}
}
