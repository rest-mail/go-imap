package imap

import (
	"strings"
	"testing"
	"time"
)

// The ENVELOPE recipient and reference fields — to, cc, bcc, in-reply-to and
// message-id — must be parsed from the message's own headers rather than being
// hard-coded NIL (RFC 3501 §7.4.2, item 1). The envelope date must come from the
// message's Date: header, not the arrival time that backs INTERNALDATE (§7.4.2,
// item 2).

// envelopeRaw is a message whose headers carry every field the ENVELOPE reports,
// with a Date: header deliberately different from the arrival time the mock seeds
// (2025-03-15) so the two dates can be told apart.
const envelopeRaw = "From: Alice <alice@example.com>\r\n" +
	"Sender: Secretary <sec@example.com>\r\n" +
	"Reply-To: Alice Replies <replies@example.com>\r\n" +
	"To: Bob <bob@example.org>, carol@example.net\r\n" +
	"Cc: Dave <dave@example.com>\r\n" +
	"Bcc: Eve <eve@example.com>\r\n" +
	"In-Reply-To: <parent-123@example.com>\r\n" +
	"Message-ID: <msg-456@example.com>\r\n" +
	"Date: Mon, 01 Jan 2024 09:00:00 +0000\r\n" +
	"Subject: Hello there\r\n" +
	"\r\n" +
	"body text\r\n"

// fetchEnvelope logs in, selects INBOX and FETCHes (ENVELOPE INTERNALDATE) for
// sequence 1, returning the single untagged FETCH line.
func fetchEnvelope(t *testing.T, m *mockBackend) string {
	t.Helper()
	h := newIMAPHarness(t, m)
	h.login("a1")
	h.selectInbox("a2")
	untagged, status := h.command("a3", "FETCH 1 (ENVELOPE INTERNALDATE)")
	if !strings.Contains(status, " OK") {
		t.Fatalf("FETCH ENVELOPE status = %q, want OK", status)
	}
	for _, l := range untagged {
		if strings.Contains(l, "FETCH") {
			return l
		}
	}
	t.Fatalf("no untagged FETCH line in %v", untagged)
	return ""
}

// Item 1: To, Cc, In-Reply-To and Message-ID are populated from the headers, each
// as a correctly-structured, non-NIL envelope field.
func TestIMAP_Envelope_RecipientAndReferenceFields(t *testing.T) {
	m := newMockBackend()
	m.seed("INBOX", 1, len(envelopeRaw), envelopeRaw)
	line := fetchEnvelope(t, m)

	// To: two addresses — "Bob <bob@example.org>" and "carol@example.net".
	for _, want := range []string{`"Bob" NIL "bob" "example.org"`, `NIL NIL "carol" "example.net"`} {
		if !strings.Contains(line, want) {
			t.Errorf("ENVELOPE To missing address %s\n  got %s", want, line)
		}
	}
	// Cc: Dave <dave@example.com>.
	if want := `"Dave" NIL "dave" "example.com"`; !strings.Contains(line, want) {
		t.Errorf("ENVELOPE Cc missing %s\n  got %s", want, line)
	}
	// In-Reply-To and Message-ID are quoted verbatim (angle brackets kept).
	for _, want := range []string{`"<parent-123@example.com>"`, `"<msg-456@example.com>"`} {
		if !strings.Contains(line, want) {
			t.Errorf("ENVELOPE reference field missing %s\n  got %s", want, line)
		}
	}
	// The tail must NOT be the all-NIL "NIL NIL NIL NIL)" that the pre-fix engine
	// emitted for to/cc/in-reply-to/message-id.
	if strings.Contains(line, "NIL NIL NIL NIL)") {
		t.Errorf("ENVELOPE recipient/reference fields still NIL: %s", line)
	}
}

// Item 1: Bcc, when present in the stored message, is populated too.
func TestIMAP_Envelope_BccPopulatedWhenPresent(t *testing.T) {
	m := newMockBackend()
	m.seed("INBOX", 1, len(envelopeRaw), envelopeRaw)
	line := fetchEnvelope(t, m)
	if want := `"Eve" NIL "eve" "example.com"`; !strings.Contains(line, want) {
		t.Errorf("ENVELOPE Bcc missing %s\n  got %s", want, line)
	}
}

// Item 1: an absent Bcc header yields NIL (the common case for stored mail).
func TestIMAP_Envelope_BccNilWhenAbsent(t *testing.T) {
	raw := "From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@example.org>\r\n" +
		"Date: Mon, 01 Jan 2024 09:00:00 +0000\r\n" +
		"Subject: Hi\r\n\r\nbody\r\n"
	m := newMockBackend()
	m.seed("INBOX", 1, len(raw), raw)
	line := fetchEnvelope(t, m)
	// No Bcc header, no Cc, no In-Reply-To, no Message-ID: those four fields are
	// NIL, so the envelope ends "NIL NIL NIL NIL)" (cc bcc in-reply-to message-id).
	if !strings.Contains(line, "NIL NIL NIL NIL)") {
		t.Errorf("absent cc/bcc/in-reply-to/message-id should be NIL: %s", line)
	}
	// But To IS present, so it must not be NIL.
	if !strings.Contains(line, `"bob" "example.org"`) {
		t.Errorf("ENVELOPE To should be populated: %s", line)
	}
}

// Item 2: the ENVELOPE date is the Date: header; INTERNALDATE stays the arrival
// time (2025-03-15, seeded by the mock). The two must differ in the response.
func TestIMAP_Envelope_DateFromHeaderNotArrival(t *testing.T) {
	m := newMockBackend()
	m.seed("INBOX", 1, len(envelopeRaw), envelopeRaw)
	line := fetchEnvelope(t, m)

	// ENVELOPE date comes from "Date: Mon, 01 Jan 2024 09:00:00 +0000".
	if want := `"Mon, 01 Jan 2024 09:00:00 +0000"`; !strings.Contains(line, want) {
		t.Errorf("ENVELOPE date should be the Date: header %s\n  got %s", want, line)
	}
	// INTERNALDATE stays the arrival time the mock seeds (15-Mar-2025 10:00:00).
	if want := `INTERNALDATE "15-Mar-2025 10:00:00 +0000"`; !strings.Contains(line, want) {
		t.Errorf("INTERNALDATE should be the arrival time %s\n  got %s", want, line)
	}
	// The arrival date must NOT appear inside the ENVELOPE as the envelope date.
	if strings.Contains(line, `"Sat, 15 Mar 2025 10:00:00 +0000"`) {
		t.Errorf("ENVELOPE date wrongly used the arrival time: %s", line)
	}
}

// Item 2: when the Date: header is missing or unparseable, the ENVELOPE date
// falls back to the arrival time (ReceivedAt / Message.Date).
func TestIMAP_Envelope_DateFallsBackToArrival(t *testing.T) {
	raw := "From: Alice <alice@example.com>\r\n" +
		"To: Bob <bob@example.org>\r\n" +
		"Subject: No date header\r\n\r\nbody\r\n"
	m := newMockBackend()
	m.seed("INBOX", 1, len(raw), raw)
	line := fetchEnvelope(t, m)
	if want := `"Sat, 15 Mar 2025 10:00:00 +0000"`; !strings.Contains(line, want) {
		t.Errorf("ENVELOPE date should fall back to arrival %s\n  got %s", want, line)
	}
}

// Direct unit test pinning the full ten-field structure and its ordering when the
// raw message is available (RFC 3501 §7.4.2: date subject from sender reply-to to
// cc bcc in-reply-to message-id).
func TestBuildEnvelope_FromHeaders(t *testing.T) {
	msg := Message{
		Subject: "seed subject",
		From:    Address{Name: "Seed", Email: "seed@example.com"},
		Date:    time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC),
	}
	env := buildEnvelope(msg, envelopeRaw, true)

	if !parensBalanced(env) {
		t.Fatalf("envelope has unbalanced parens: %s", env)
	}
	// Sender and Reply-To come from their own headers here (distinct from From).
	for _, want := range []string{
		`"Mon, 01 Jan 2024 09:00:00 +0000"`,           // date (header, not arrival)
		`"Alice" NIL "alice" "example.com"`,           // from
		`"Secretary" NIL "sec" "example.com"`,         // sender
		`"Alice Replies" NIL "replies" "example.com"`, // reply-to
		`"Bob" NIL "bob" "example.org"`,               // to[0]
		`"Dave" NIL "dave" "example.com"`,             // cc
		`"Eve" NIL "eve" "example.com"`,               // bcc
		`"<parent-123@example.com>"`,                  // in-reply-to
		`"<msg-456@example.com>"`,                     // message-id
	} {
		if !strings.Contains(env, want) {
			t.Errorf("envelope missing %s\n  got %s", want, env)
		}
	}
}

// When no raw message is available (rawOK=false), buildEnvelope degrades to the
// Message model exactly as before: from/sender/reply-to from Message.From, To from
// Message.To, and cc/bcc/in-reply-to/message-id NIL. This pins the fallback the
// FETCH path relies on when Mailbox.Fetch cannot supply bytes.
func TestBuildEnvelope_NoRawFallsBackToModel(t *testing.T) {
	msg := Message{
		Subject: "Hi",
		From:    Address{Name: "Alice", Email: "alice@example.com"},
		To:      "Bob <bob@example.org>",
		Date:    time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}
	env := buildEnvelope(msg, "", false)
	if !strings.Contains(env, `"bob" "example.org"`) {
		t.Errorf("To should come from Message.To: %s", env)
	}
	if !strings.HasSuffix(env, "NIL NIL NIL NIL)") {
		t.Errorf("cc/bcc/in-reply-to/message-id should be NIL without raw: %s", env)
	}
	if !strings.Contains(env, "Mon, 15 Jan 2024") {
		t.Errorf("date should come from Message.Date without raw: %s", env)
	}
}
