package imap

import (
	"encoding/base64"
	"math/rand"
	"strings"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// parseIMAPCommand
// ---------------------------------------------------------------------------

func TestParseIMAPCommand_Basic(t *testing.T) {
	tag, cmd, args := parseIMAPCommand("A001 LOGIN user pass")
	if tag != "A001" {
		t.Errorf("tag = %q, want %q", tag, "A001")
	}
	if cmd != "LOGIN" {
		t.Errorf("cmd = %q, want %q", cmd, "LOGIN")
	}
	if args != "user pass" {
		t.Errorf("args = %q, want %q", args, "user pass")
	}
}

func TestParseIMAPCommand_NoArgs(t *testing.T) {
	tag, cmd, args := parseIMAPCommand("A002 NOOP")
	if tag != "A002" {
		t.Errorf("tag = %q, want %q", tag, "A002")
	}
	if cmd != "NOOP" {
		t.Errorf("cmd = %q, want %q", cmd, "NOOP")
	}
	if args != "" {
		t.Errorf("args = %q, want empty", args)
	}
}

func TestParseIMAPCommand_EmptyLine(t *testing.T) {
	tag, cmd, args := parseIMAPCommand("")
	if tag != "" || cmd != "" || args != "" {
		t.Errorf("expected all empty for empty input, got tag=%q cmd=%q args=%q", tag, cmd, args)
	}
}

func TestParseIMAPCommand_SingleWord(t *testing.T) {
	tag, cmd, args := parseIMAPCommand("LOGOUT")
	if tag != "" || cmd != "" || args != "" {
		t.Errorf("expected all empty for single word, got tag=%q cmd=%q args=%q", tag, cmd, args)
	}
}

func TestParseIMAPCommand_ArgsWithSpaces(t *testing.T) {
	tag, cmd, args := parseIMAPCommand(`A003 FETCH 1:* (FLAGS BODY[HEADER.FIELDS (Subject From)])`)
	if tag != "A003" {
		t.Errorf("tag = %q, want %q", tag, "A003")
	}
	if cmd != "FETCH" {
		t.Errorf("cmd = %q, want %q", cmd, "FETCH")
	}
	expected := `1:* (FLAGS BODY[HEADER.FIELDS (Subject From)])`
	if args != expected {
		t.Errorf("args = %q, want %q", args, expected)
	}
}

// ---------------------------------------------------------------------------
// parseIMAPArgs
// ---------------------------------------------------------------------------

func TestParseIMAPArgs_SimpleTokens(t *testing.T) {
	result := parseIMAPArgs("FLAGS BODY ENVELOPE")
	if len(result) != 3 {
		t.Fatalf("got %d tokens, want 3: %v", len(result), result)
	}
	if result[0] != "FLAGS" || result[1] != "BODY" || result[2] != "ENVELOPE" {
		t.Errorf("got %v, want [FLAGS BODY ENVELOPE]", result)
	}
}

func TestParseIMAPArgs_QuotedString(t *testing.T) {
	result := parseIMAPArgs(`"hello world" foo`)
	if len(result) != 2 {
		t.Fatalf("got %d tokens, want 2: %v", len(result), result)
	}
	if result[0] != `"hello world"` {
		t.Errorf("result[0] = %q, want %q", result[0], `"hello world"`)
	}
	if result[1] != "foo" {
		t.Errorf("result[1] = %q, want %q", result[1], "foo")
	}
}

func TestParseIMAPArgs_UnterminatedQuote(t *testing.T) {
	result := parseIMAPArgs(`"unterminated`)
	if len(result) != 1 {
		t.Fatalf("got %d tokens, want 1: %v", len(result), result)
	}
	if result[0] != `"unterminated` {
		t.Errorf("result[0] = %q, want %q", result[0], `"unterminated`)
	}
}

func TestParseIMAPArgs_Parenthesized(t *testing.T) {
	result := parseIMAPArgs(`(FLAGS BODY) ENVELOPE`)
	if len(result) != 2 {
		t.Fatalf("got %d tokens, want 2: %v", len(result), result)
	}
	if result[0] != "(FLAGS BODY)" {
		t.Errorf("result[0] = %q, want %q", result[0], "(FLAGS BODY)")
	}
	if result[1] != "ENVELOPE" {
		t.Errorf("result[1] = %q, want %q", result[1], "ENVELOPE")
	}
}

func TestParseIMAPArgs_NestedParens(t *testing.T) {
	result := parseIMAPArgs(`(BODY[HEADER.FIELDS (From Subject)])`)
	if len(result) != 1 {
		t.Fatalf("got %d tokens, want 1: %v", len(result), result)
	}
	if result[0] != "(BODY[HEADER.FIELDS (From Subject)])" {
		t.Errorf("result[0] = %q, want %q", result[0], "(BODY[HEADER.FIELDS (From Subject)])")
	}
}

func TestParseIMAPArgs_Empty(t *testing.T) {
	result := parseIMAPArgs("")
	if len(result) != 0 {
		t.Errorf("expected empty result for empty input, got %v", result)
	}
}

func TestParseIMAPArgs_OnlySpaces(t *testing.T) {
	result := parseIMAPArgs("   ")
	if len(result) != 0 {
		t.Errorf("expected empty result for whitespace input, got %v", result)
	}
}

func TestParseIMAPArgs_MixedQuotesAndParens(t *testing.T) {
	result := parseIMAPArgs(`"INBOX" (\\Seen) 1:*`)
	if len(result) != 3 {
		t.Fatalf("got %d tokens, want 3: %v", len(result), result)
	}
	if result[0] != `"INBOX"` {
		t.Errorf("result[0] = %q, want %q", result[0], `"INBOX"`)
	}
	if result[1] != `(\\Seen)` {
		t.Errorf("result[1] = %q, want %q", result[1], `(\\Seen)`)
	}
	if result[2] != "1:*" {
		t.Errorf("result[2] = %q, want %q", result[2], "1:*")
	}
}

// ---------------------------------------------------------------------------
// unquote
// ---------------------------------------------------------------------------

func TestUnquote_QuotedString(t *testing.T) {
	if result := unquote(`"hello"`); result != "hello" {
		t.Errorf("got %q, want %q", result, "hello")
	}
}

func TestUnquote_NoQuotes(t *testing.T) {
	if result := unquote("hello"); result != "hello" {
		t.Errorf("got %q, want %q", result, "hello")
	}
}

func TestUnquote_EmptyQuotes(t *testing.T) {
	if result := unquote(`""`); result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestUnquote_WhitespaceAround(t *testing.T) {
	if result := unquote(`  "hello"  `); result != "hello" {
		t.Errorf("got %q, want %q", result, "hello")
	}
}

func TestUnquote_SingleChar(t *testing.T) {
	if result := unquote("a"); result != "a" {
		t.Errorf("got %q, want %q", result, "a")
	}
}

func TestUnquote_EmptyString(t *testing.T) {
	if result := unquote(""); result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestUnquote_OnlyOpenQuote(t *testing.T) {
	if result := unquote(`"hello`); result != `"hello` {
		t.Errorf("got %q, want %q", result, `"hello`)
	}
}

// A quoted-string body decodes its two RFC 3501 §4.3 escapes: \" -> " and
// \\ -> \. Passwords and mailbox names carrying either character depend on this.
func TestUnquote_EscapedQuote(t *testing.T) {
	if result := unquote(`"pa\"ss"`); result != `pa"ss` {
		t.Errorf("got %q, want %q", result, `pa"ss`)
	}
}

func TestUnquote_EscapedBackslash(t *testing.T) {
	if result := unquote(`"a\\b"`); result != `a\b` {
		t.Errorf("got %q, want %q", result, `a\b`)
	}
}

func TestUnquote_MixedEscapes(t *testing.T) {
	// "\\\"" is the wire form of the two-character value \" (backslash, quote).
	if result := unquote(`"\\\""`); result != `\"` {
		t.Errorf("got %q, want %q", result, `\"`)
	}
}

// A backslash before an ordinary character is not an escape (only \" and \\
// are defined), so it is preserved literally.
func TestUnquote_NonEscapeBackslashPreserved(t *testing.T) {
	if result := unquote(`"a\b"`); result != `a\b` {
		t.Errorf("got %q, want %q", result, `a\b`)
	}
}

// An escaped quote inside a quoted argument must not terminate the token early:
// the whole `"pa\"ss"` is one argument, kept with its quotes and escape intact.
func TestParseIMAPArgs_EscapedQuoteInString(t *testing.T) {
	result := parseIMAPArgs(`"pa\"ss" foo`)
	if len(result) != 2 {
		t.Fatalf("got %d tokens, want 2: %v", len(result), result)
	}
	if result[0] != `"pa\"ss"` {
		t.Errorf("result[0] = %q, want %q", result[0], `"pa\"ss"`)
	}
	if result[1] != "foo" {
		t.Errorf("result[1] = %q, want %q", result[1], "foo")
	}
}

// A quoted-string may end with an escaped backslash; the following quote still
// closes it.
func TestParseIMAPArgs_TrailingEscapedBackslash(t *testing.T) {
	result := parseIMAPArgs(`"foo\\" bar`)
	if len(result) != 2 {
		t.Fatalf("got %d tokens, want 2: %v", len(result), result)
	}
	if result[0] != `"foo\\"` {
		t.Errorf("result[0] = %q, want %q", result[0], `"foo\\"`)
	}
	if result[1] != "bar" {
		t.Errorf("result[1] = %q, want %q", result[1], "bar")
	}
}

// A string whose only quote is escaped is unterminated: it does not close, so
// the whole remainder is taken as one (unterminated) token.
func TestParseIMAPArgs_EscapedQuoteUnterminated(t *testing.T) {
	result := parseIMAPArgs(`"foo\"`)
	if len(result) != 1 {
		t.Fatalf("got %d tokens, want 1: %v", len(result), result)
	}
	if result[0] != `"foo\"` {
		t.Errorf("result[0] = %q, want %q", result[0], `"foo\"`)
	}
}

// ---------------------------------------------------------------------------
// decodeBase64
// ---------------------------------------------------------------------------

func TestDecodeBase64_Valid(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte("hello world"))
	decoded, err := decodeBase64(encoded)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(decoded) != "hello world" {
		t.Errorf("got %q, want %q", string(decoded), "hello world")
	}
}

func TestDecodeBase64_Empty(t *testing.T) {
	decoded, err := decodeBase64("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(decoded) != 0 {
		t.Errorf("expected empty slice, got %v", decoded)
	}
}

func TestDecodeBase64_Invalid(t *testing.T) {
	if _, err := decodeBase64("!!!not-valid-base64!!!"); err == nil {
		t.Error("expected error for invalid base64, got nil")
	}
}

// ---------------------------------------------------------------------------
// buildFlags
// ---------------------------------------------------------------------------

func TestBuildFlags_NoFlags(t *testing.T) {
	if result := buildFlags(Message{}); result != "" {
		t.Errorf("got %q, want empty", result)
	}
}

func TestBuildFlags_Seen(t *testing.T) {
	if result := buildFlags(Message{Seen: true}); result != `\Seen` {
		t.Errorf("got %q, want %q", result, `\Seen`)
	}
}

func TestBuildFlags_Flagged(t *testing.T) {
	if result := buildFlags(Message{Flagged: true}); result != `\Flagged` {
		t.Errorf("got %q, want %q", result, `\Flagged`)
	}
}

func TestBuildFlags_Draft(t *testing.T) {
	if result := buildFlags(Message{Draft: true}); result != `\Draft` {
		t.Errorf("got %q, want %q", result, `\Draft`)
	}
}

func TestBuildFlags_AllFlags(t *testing.T) {
	result := buildFlags(Message{Seen: true, Flagged: true, Draft: true})
	if !strings.Contains(result, `\Seen`) {
		t.Errorf("result %q missing \\Seen", result)
	}
	if !strings.Contains(result, `\Flagged`) {
		t.Errorf("result %q missing \\Flagged", result)
	}
	if !strings.Contains(result, `\Draft`) {
		t.Errorf("result %q missing \\Draft", result)
	}
}

// ---------------------------------------------------------------------------
// quoteString
// ---------------------------------------------------------------------------

func TestQuoteString_Empty(t *testing.T) {
	if result := quoteString(""); result != "NIL" {
		t.Errorf("got %q, want %q", result, "NIL")
	}
}

func TestQuoteString_Simple(t *testing.T) {
	if result := quoteString("hello"); result != `"hello"` {
		t.Errorf("got %q, want %q", result, `"hello"`)
	}
}

func TestQuoteString_WithBackslash(t *testing.T) {
	if result := quoteString(`back\slash`); result != `"back\\slash"` {
		t.Errorf("got %q, want %q", result, `"back\\slash"`)
	}
}

func TestQuoteString_WithDoubleQuote(t *testing.T) {
	if result := quoteString(`say "hello"`); result != `"say \"hello\""` {
		t.Errorf("got %q, want %q", result, `"say \"hello\""`)
	}
}

func TestQuoteString_WithBothSpecials(t *testing.T) {
	result := quoteString(`a\"b`)
	expected := `"a\\\"b"`
	if result != expected {
		t.Errorf("got %q, want %q", result, expected)
	}
}

// ---------------------------------------------------------------------------
// buildAddress
// ---------------------------------------------------------------------------

func TestBuildAddress_EmptyEmail(t *testing.T) {
	if result := buildAddress("John", ""); result != "NIL" {
		t.Errorf("got %q, want %q", result, "NIL")
	}
}

func TestBuildAddress_FullAddress(t *testing.T) {
	result := buildAddress("John Doe", "john@example.com")
	if !strings.Contains(result, `"John Doe"`) {
		t.Errorf("result %q missing name", result)
	}
	if !strings.Contains(result, `"john"`) {
		t.Errorf("result %q missing user part", result)
	}
	if !strings.Contains(result, `"example.com"`) {
		t.Errorf("result %q missing host part", result)
	}
	if !strings.HasPrefix(result, "((") || !strings.HasSuffix(result, "))") {
		t.Errorf("result %q should be wrapped in double parens", result)
	}
}

func TestBuildAddress_NoAtSign(t *testing.T) {
	result := buildAddress("Local", "localonly")
	if !strings.Contains(result, `"localonly"`) {
		t.Errorf("result %q missing user part", result)
	}
	if !strings.Contains(result, "NIL") {
		t.Errorf("result %q should have NIL for empty host", result)
	}
}

func TestBuildAddress_EmptyName(t *testing.T) {
	result := buildAddress("", "user@host.com")
	if !strings.Contains(result, "NIL NIL") {
		t.Errorf("result %q should have NIL for empty name", result)
	}
}

// ---------------------------------------------------------------------------
// buildEnvelope
// ---------------------------------------------------------------------------

func TestBuildEnvelope_Basic(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	msg := Message{
		Subject: "Test Subject",
		From:    Address{Name: "Alice", Email: "alice@example.com"},
		Date:    fixedTime,
	}
	result := buildEnvelope(msg)

	if !strings.HasPrefix(result, "(") || !strings.HasSuffix(result, ")") {
		t.Errorf("envelope should be wrapped in parens: %s", result)
	}
	if !strings.Contains(result, "Mon, 15 Jan 2024") {
		t.Errorf("envelope missing date: %s", result)
	}
	if !strings.Contains(result, `"Test Subject"`) {
		t.Errorf("envelope missing subject: %s", result)
	}
	if !strings.Contains(result, "example.com") {
		t.Errorf("envelope missing sender host: %s", result)
	}
	if !strings.Contains(result, "Alice") {
		t.Errorf("envelope missing sender name: %s", result)
	}
	if !strings.HasSuffix(result, "NIL NIL NIL NIL NIL)") {
		t.Errorf("envelope should end with 5 NILs: %s", result)
	}
}

func TestBuildEnvelope_EmptySubject(t *testing.T) {
	fixedTime := time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC)
	msg := Message{
		Subject: "",
		From:    Address{Name: "Bob", Email: "bob@test.com"},
		Date:    fixedTime,
	}
	result := buildEnvelope(msg)
	if !strings.Contains(result, " NIL ") {
		t.Errorf("envelope should have NIL for empty subject: %s", result)
	}
}

// ---------------------------------------------------------------------------
// parseSequenceSet
// ---------------------------------------------------------------------------

func TestParseSequenceSet_Single(t *testing.T) {
	result := parseSequenceSet("3", 10)
	if len(result) != 1 || result[0] != 3 {
		t.Errorf("got %v, want [3]", result)
	}
}

func TestParseSequenceSet_Range(t *testing.T) {
	result := parseSequenceSet("2:5", 10)
	expected := []int{2, 3, 4, 5}
	assertIntSlice(t, result, expected)
}

func TestParseSequenceSet_StarInRange(t *testing.T) {
	result := parseSequenceSet("3:*", 5)
	assertIntSlice(t, result, []int{3, 4, 5})
}

func TestParseSequenceSet_StarAlone(t *testing.T) {
	result := parseSequenceSet("*", 7)
	if len(result) != 1 || result[0] != 7 {
		t.Errorf("got %v, want [7]", result)
	}
}

func TestParseSequenceSet_CommaSeparated(t *testing.T) {
	result := parseSequenceSet("1,3,5", 10)
	assertIntSlice(t, result, []int{1, 3, 5})
}

func TestParseSequenceSet_MixedRangesAndSingles(t *testing.T) {
	result := parseSequenceSet("1,3:5,7", 10)
	assertIntSlice(t, result, []int{1, 3, 4, 5, 7})
}

func TestParseSequenceSet_ReverseRange(t *testing.T) {
	result := parseSequenceSet("5:2", 10)
	assertIntSlice(t, result, []int{2, 3, 4, 5})
}

func TestParseSequenceSet_ZeroTotal(t *testing.T) {
	if result := parseSequenceSet("1:5", 0); result != nil {
		t.Errorf("got %v, want nil for zero total", result)
	}
}

func TestParseSequenceSet_OutOfRange(t *testing.T) {
	if result := parseSequenceSet("15", 10); len(result) != 0 {
		t.Errorf("got %v, want empty for out of range", result)
	}
}

func TestParseSequenceSet_ZeroSeqNum(t *testing.T) {
	if result := parseSequenceSet("0", 10); len(result) != 0 {
		t.Errorf("got %v, want empty for seq 0", result)
	}
}

func TestParseSequenceSet_NoDuplicates(t *testing.T) {
	if result := parseSequenceSet("1,1,1", 10); len(result) != 1 {
		t.Errorf("got %v, want [1] (no duplicates)", result)
	}
}

func TestParseSequenceSet_OverlappingRanges(t *testing.T) {
	result := parseSequenceSet("1:3,2:4", 10)
	assertIntSlice(t, result, []int{1, 2, 3, 4})
}

func TestParseSequenceSet_OneToStar(t *testing.T) {
	result := parseSequenceSet("1:*", 3)
	assertIntSlice(t, result, []int{1, 2, 3})
}

func TestParseSequenceSet_EmptyString(t *testing.T) {
	if result := parseSequenceSet("", 10); len(result) != 0 {
		t.Errorf("got %v, want empty for empty string", result)
	}
}

func assertIntSlice(t *testing.T, got, want []int) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i, v := range want {
		if got[i] != v {
			t.Errorf("result[%d] = %d, want %d", i, got[i], v)
		}
	}
}

// ---------------------------------------------------------------------------
// Sequence/UID range DoS (issue #8): a set such as "1:4294967295" must NOT be
// expanded by iterating the literal declared span. These regression tests fail
// (time out) against unbounded expansion and pass once the work is bounded to
// the messages that can actually exist.
// ---------------------------------------------------------------------------

// A pathological range must not drive an O(span) loop. We use a span far larger
// than the issue's 1:4294967295 example so an unbounded loop blows past the
// timeout deterministically on any machine, while a clamped implementation
// returns instantly.
func TestParseSequenceSet_HugeRangeIsBounded(t *testing.T) {
	done := make(chan []int, 1)
	go func() {
		done <- parseSequenceSet("1:4000000000000000000", 3)
	}()
	select {
	case result := <-done:
		// Only sequence numbers that can exist may be produced.
		assertIntSlice(t, result, []int{1, 2, 3})
	case <-time.After(2 * time.Second):
		t.Fatal("parseSequenceSet did not return within 2s: unbounded range expansion (CPU DoS)")
	}
}

// The reverse form of the same pathological input (huge start clamps to total,
// so the range collapses to empty) must also be bounded.
func TestParseSequenceSet_HugeReverseRangeIsBounded(t *testing.T) {
	done := make(chan []int, 1)
	go func() {
		done <- parseSequenceSet("4000000000000000000:1", 3)
	}()
	select {
	case result := <-done:
		assertIntSlice(t, result, []int{1, 2, 3})
	case <-time.After(2 * time.Second):
		t.Fatal("parseSequenceSet did not return within 2s: unbounded range expansion (CPU DoS)")
	}
}

// UID SEARCH must not expand a giant UID range per message (the quadratic path
// in matchOne). It must complete quickly and still match every UID inside the
// range. 4294967295 is the issue's example and a valid 32-bit UID upper bound.
func TestUIDSearch_HugeRangeIsBounded(t *testing.T) {
	s := &Session{
		messages: []Message{{UID: 1}, {UID: 2}, {UID: 3}},
		deleted:  map[uint32]bool{},
	}
	type result struct{ matched []uint32 }
	done := make(chan result, 1)
	go func() {
		criteria := s.parseSearchCriteria("UID 1:4294967295")
		var r result
		for _, msg := range s.messages {
			if s.matchesCriteria(msg, criteria) {
				r.matched = append(r.matched, msg.UID)
			}
		}
		done <- r
	}()
	select {
	case r := <-done:
		if len(r.matched) != 3 {
			t.Fatalf("UID 1:4294967295 matched %v, want all of [1 2 3]", r.matched)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("UID SEARCH did not return within 2s: per-message range expansion (CPU DoS)")
	}
}

// ---------------------------------------------------------------------------
// parseUIDRanges / uidInRanges (issue #8 membership, no expansion)
// ---------------------------------------------------------------------------

func TestParseUIDRanges_SingleAndRangeAndStar(t *testing.T) {
	// "100:*" with maxUID 42 resolves * to 42, then normalizes 100 > 42 to {42,100}.
	ranges := parseUIDRanges("2,5:7,100:*", 42)
	want := []seqRange{{2, 2}, {5, 7}, {42, 100}}
	if len(ranges) != len(want) {
		t.Fatalf("got %v, want %v", ranges, want)
	}
	for i, r := range ranges {
		if r != want[i] {
			t.Errorf("range[%d] = %v, want %v", i, r, want[i])
		}
	}
}

func TestParseUIDRanges_StarOnEmptyMailboxDropped(t *testing.T) {
	if ranges := parseUIDRanges("*", 0); len(ranges) != 0 {
		t.Errorf("got %v, want empty ('*' unresolvable on empty mailbox)", ranges)
	}
	if ranges := parseUIDRanges("1:*", 0); len(ranges) != 0 {
		t.Errorf("got %v, want empty ('*' unresolvable on empty mailbox)", ranges)
	}
}

func TestParseUIDRanges_OutOfUint32Dropped(t *testing.T) {
	// An endpoint beyond the 32-bit UID space cannot name a real message.
	if ranges := parseUIDRanges("1:99999999999", 3); len(ranges) != 0 {
		t.Errorf("got %v, want empty (endpoint exceeds uint32)", ranges)
	}
}

func TestParseUIDRanges_InvalidTokenDropped(t *testing.T) {
	ranges := parseUIDRanges("abc,3:5", 10)
	if len(ranges) != 1 || ranges[0] != (seqRange{3, 5}) {
		t.Errorf("got %v, want [{3 5}]", ranges)
	}
}

func TestUIDInRanges(t *testing.T) {
	ranges := []seqRange{{2, 2}, {5, 7}}
	cases := map[uint32]bool{1: false, 2: true, 3: false, 5: true, 6: true, 7: true, 8: false}
	for uid, want := range cases {
		if got := uidInRanges(uid, ranges); got != want {
			t.Errorf("uidInRanges(%d) = %v, want %v", uid, got, want)
		}
	}
	if uidInRanges(5, nil) {
		t.Error("uidInRanges against nil ranges should be false")
	}
}

// UID "*" must resolve to the mailbox high-water mark, so "UID *" matches only
// the highest-UID message. (The previous implementation matched nothing.)
func TestUIDSearch_StarResolvesToHighestUID(t *testing.T) {
	s := &Session{
		messages: []Message{{UID: 10}, {UID: 20}, {UID: 30}},
		deleted:  map[uint32]bool{},
	}
	criteria := s.parseSearchCriteria("UID *")
	var matched []uint32
	for _, msg := range s.messages {
		if s.matchesCriteria(msg, criteria) {
			matched = append(matched, msg.UID)
		}
	}
	if len(matched) != 1 || matched[0] != 30 {
		t.Errorf("UID * matched %v, want [30]", matched)
	}
}

// ---------------------------------------------------------------------------
// resolveSeqNum
// ---------------------------------------------------------------------------

func TestResolveSeqNum_Star(t *testing.T) {
	if result := resolveSeqNum("*", 10); result != 10 {
		t.Errorf("got %d, want 10", result)
	}
}

func TestResolveSeqNum_Number(t *testing.T) {
	if result := resolveSeqNum("5", 10); result != 5 {
		t.Errorf("got %d, want 5", result)
	}
}

func TestResolveSeqNum_Invalid(t *testing.T) {
	if result := resolveSeqNum("abc", 10); result != 0 {
		t.Errorf("got %d, want 0 for invalid input", result)
	}
}

func TestResolveSeqNum_Whitespace(t *testing.T) {
	if result := resolveSeqNum("  3  ", 10); result != 3 {
		t.Errorf("got %d, want 3 (trimmed)", result)
	}
}

func TestResolveSeqNum_StarWithWhitespace(t *testing.T) {
	if result := resolveSeqNum("  *  ", 10); result != 10 {
		t.Errorf("got %d, want 10 (trimmed star)", result)
	}
}

// ---------------------------------------------------------------------------
// parseFlags
// ---------------------------------------------------------------------------

func TestParseFlags_Standard(t *testing.T) {
	result := parseFlags(`(\Seen \Flagged)`)
	if len(result) != 2 {
		t.Fatalf("got %v, want 2 flags", result)
	}
	if result[0] != `\Seen` || result[1] != `\Flagged` {
		t.Errorf("got %v, want [\\Seen \\Flagged]", result)
	}
}

func TestParseFlags_SingleFlag(t *testing.T) {
	result := parseFlags(`(\Draft)`)
	if len(result) != 1 || result[0] != `\Draft` {
		t.Errorf("got %v, want [\\Draft]", result)
	}
}

func TestParseFlags_EmptyParens(t *testing.T) {
	if result := parseFlags("()"); len(result) != 0 {
		t.Errorf("got %v, want empty", result)
	}
}

func TestParseFlags_NoParens(t *testing.T) {
	result := parseFlags(`\Seen \Flagged`)
	if len(result) != 2 {
		t.Fatalf("got %v, want 2 flags", result)
	}
	if result[0] != `\Seen` || result[1] != `\Flagged` {
		t.Errorf("got %v, want [\\Seen \\Flagged]", result)
	}
}

func TestParseFlags_WithWhitespace(t *testing.T) {
	result := parseFlags("  (  \\Seen  )  ")
	if len(result) != 1 || result[0] != `\Seen` {
		t.Errorf("got %v, want [\\Seen]", result)
	}
}

func TestParseFlags_EmptyString(t *testing.T) {
	if result := parseFlags(""); len(result) != 0 {
		t.Errorf("got %v, want empty for empty input", result)
	}
}

// ---------------------------------------------------------------------------
// extractHeaderFieldNames
// ---------------------------------------------------------------------------

func TestExtractHeaderFieldNames_Standard(t *testing.T) {
	result := extractHeaderFieldNames("BODY[HEADER.FIELDS (From Subject Date)]")
	if len(result) != 3 {
		t.Fatalf("got %v, want 3 fields", result)
	}
	if result[0] != "From" || result[1] != "Subject" || result[2] != "Date" {
		t.Errorf("got %v, want [From Subject Date]", result)
	}
}

func TestExtractHeaderFieldNames_CaseInsensitive(t *testing.T) {
	result := extractHeaderFieldNames("body[header.fields (From To)]")
	if len(result) != 2 {
		t.Fatalf("got %v, want 2 fields", result)
	}
	if result[0] != "From" || result[1] != "To" {
		t.Errorf("got %v, want [From To]", result)
	}
}

func TestExtractHeaderFieldNames_NoHeaderFields(t *testing.T) {
	if result := extractHeaderFieldNames("BODY[]"); result != nil {
		t.Errorf("got %v, want nil", result)
	}
}

func TestExtractHeaderFieldNames_NoParens(t *testing.T) {
	if result := extractHeaderFieldNames("BODY[HEADER.FIELDS]"); result != nil {
		t.Errorf("got %v, want nil", result)
	}
}

func TestExtractHeaderFieldNames_SingleField(t *testing.T) {
	result := extractHeaderFieldNames("BODY[HEADER.FIELDS (Subject)]")
	if len(result) != 1 || result[0] != "Subject" {
		t.Errorf("got %v, want [Subject]", result)
	}
}

func TestExtractHeaderFieldNames_EmptyParens(t *testing.T) {
	if result := extractHeaderFieldNames("BODY[HEADER.FIELDS ()]"); len(result) != 0 {
		t.Errorf("got %v, want empty", result)
	}
}

// ---------------------------------------------------------------------------
// filterHeaders
// ---------------------------------------------------------------------------

func TestFilterHeaders_Basic(t *testing.T) {
	raw := "From: alice@example.com\r\nSubject: Test\r\nDate: Mon, 01 Jan 2024\r\n\r\nBody text"
	result := filterHeaders(raw, []string{"From", "Subject"})

	if !strings.Contains(result, "From: alice@example.com\r\n") {
		t.Errorf("missing From header in: %q", result)
	}
	if !strings.Contains(result, "Subject: Test\r\n") {
		t.Errorf("missing Subject header in: %q", result)
	}
	if strings.Contains(result, "Date:") {
		t.Errorf("should not contain Date header in: %q", result)
	}
	if !strings.HasSuffix(result, "\r\n") {
		t.Errorf("should end with CRLF: %q", result)
	}
}

func TestFilterHeaders_CaseInsensitive(t *testing.T) {
	raw := "From: alice@example.com\r\nSUBJECT: Test\r\n\r\nBody"
	result := filterHeaders(raw, []string{"subject"})
	if !strings.Contains(result, "SUBJECT: Test\r\n") {
		t.Errorf("case-insensitive match failed in: %q", result)
	}
}

func TestFilterHeaders_NoMatch(t *testing.T) {
	raw := "From: alice@example.com\r\n\r\nBody"
	result := filterHeaders(raw, []string{"Subject"})
	if result != "\r\n" {
		t.Errorf("got %q, want just terminating CRLF", result)
	}
}

func TestFilterHeaders_NoBodySeparator(t *testing.T) {
	raw := "From: alice@example.com\r\nSubject: Test"
	result := filterHeaders(raw, []string{"From"})
	if !strings.Contains(result, "From: alice@example.com\r\n") {
		t.Errorf("missing From header in: %q", result)
	}
}

func TestFilterHeaders_EmptyFields(t *testing.T) {
	raw := "From: alice@example.com\r\nSubject: Test\r\n\r\nBody"
	result := filterHeaders(raw, []string{})
	if result != "\r\n" {
		t.Errorf("got %q, want just terminating CRLF for no requested fields", result)
	}
}

func TestFilterHeaders_AllFields(t *testing.T) {
	raw := "From: alice@example.com\r\nSubject: Test\r\n\r\nBody"
	result := filterHeaders(raw, []string{"From", "Subject"})
	if !strings.Contains(result, "From: alice@example.com") {
		t.Errorf("missing From in: %q", result)
	}
	if !strings.Contains(result, "Subject: Test") {
		t.Errorf("missing Subject in: %q", result)
	}
}

// ---------------------------------------------------------------------------
// matchIMAPPattern
// ---------------------------------------------------------------------------

func TestMatchIMAPPattern_StarMatchesAll(t *testing.T) {
	if !matchIMAPPattern("*", "INBOX") {
		t.Error("* should match INBOX")
	}
	if !matchIMAPPattern("*", "Sent/Subfolder") {
		t.Error("* should match Sent/Subfolder")
	}
	if !matchIMAPPattern("*", "") {
		t.Error("* should match empty string")
	}
}

func TestMatchIMAPPattern_PercentMatchesWithoutSlash(t *testing.T) {
	if !matchIMAPPattern("%", "INBOX") {
		t.Error("% should match INBOX")
	}
	if matchIMAPPattern("%", "Sent/Subfolder") {
		t.Error("% should not match Sent/Subfolder")
	}
	if !matchIMAPPattern("%", "") {
		t.Error("% should match empty string")
	}
}

func TestMatchIMAPPattern_ExactMatch(t *testing.T) {
	if !matchIMAPPattern("INBOX", "INBOX") {
		t.Error("exact match should work")
	}
	if matchIMAPPattern("INBOX", "Sent") {
		t.Error("non-matching names should not match")
	}
}

func TestMatchIMAPPattern_CaseInsensitive(t *testing.T) {
	if !matchIMAPPattern("inbox", "INBOX") {
		t.Error("case insensitive match should work")
	}
	if !matchIMAPPattern("INBOX", "inbox") {
		t.Error("case insensitive match should work")
	}
}

func TestMatchIMAPPattern_StarPrefix(t *testing.T) {
	if !matchIMAPPattern("INBOX*", "INBOX") {
		t.Error("INBOX* should match INBOX")
	}
	if !matchIMAPPattern("INBOX*", "INBOX/Sent") {
		t.Error("INBOX* should match INBOX/Sent")
	}
	if matchIMAPPattern("INBOX*", "Drafts") {
		t.Error("INBOX* should not match Drafts")
	}
}

func TestMatchIMAPPattern_PercentPrefix(t *testing.T) {
	if !matchIMAPPattern("INBOX%", "INBOX") {
		t.Error("INBOX% should match INBOX")
	}
	if matchIMAPPattern("INBOX%", "INBOX/Sent") {
		t.Error("INBOX% should not match INBOX/Sent (% does not cross /)")
	}
	if !matchIMAPPattern("INBOX/%", "INBOX/Sent") {
		t.Error("INBOX/% should match INBOX/Sent")
	}
}

func TestMatchIMAPPattern_PercentDoesNotCrossHierarchy(t *testing.T) {
	if matchIMAPPattern("INBOX/%", "INBOX/Sent/Subfolder") {
		t.Error("INBOX/% should not match INBOX/Sent/Subfolder")
	}
	if !matchIMAPPattern("INBOX/*", "INBOX/Sent/Subfolder") {
		t.Error("INBOX/* should match INBOX/Sent/Subfolder")
	}
}

func TestMatchIMAPPattern_StarInMiddle(t *testing.T) {
	if !matchIMAPPattern("I*X", "INBOX") {
		t.Error("I*X should match INBOX")
	}
	if !matchIMAPPattern("I*X", "IX") {
		t.Error("I*X should match IX")
	}
}

func TestMatchIMAPPattern_EmptyPattern(t *testing.T) {
	if !matchIMAPPattern("", "") {
		t.Error("empty pattern should match empty name")
	}
	if matchIMAPPattern("", "INBOX") {
		t.Error("empty pattern should not match non-empty name")
	}
}

func TestMatchIMAPPattern_ComplexPattern(t *testing.T) {
	if !matchIMAPPattern("Sent/*", "Sent/2024/January") {
		t.Error("Sent/* should match nested folders")
	}
	if matchIMAPPattern("Sent/%", "Sent/2024/January") {
		t.Error("Sent/% should not match nested folders")
	}
	if !matchIMAPPattern("Sent/%", "Sent/2024") {
		t.Error("Sent/% should match direct children")
	}
}

// ---------------------------------------------------------------------------
// matchIMAPPattern — wildcard semantics and DoS resistance (issue #31)
// ---------------------------------------------------------------------------

// matchPatternReference is a straightforward recursive backtracker used only as
// a correctness oracle in tests. Its RESULTS define the intended matched set
// ('*' spans any characters including '/'; '%' spans any run of non-'/'); the
// production matcher must agree with it on every input. It must not be used on
// pathological inputs (it is exponential in wildcard count — the very bug #31
// fixes) so the differential test below keeps inputs small.
func matchPatternReference(pattern, name string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchPatternReference(pattern, name[i:]) {
					return true
				}
			}
			return false
		case '%':
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return !strings.Contains(name, "/")
			}
			for i := 0; i <= len(name); i++ {
				if i > 0 && name[i-1] == '/' {
					break
				}
				if matchPatternReference(pattern, name[i:]) {
					return true
				}
			}
			return false
		default:
			if len(name) == 0 || pattern[0] != name[0] {
				return false
			}
			pattern = pattern[1:]
			name = name[1:]
		}
	}
	return len(name) == 0
}

// referenceMatchIMAPPattern mirrors matchIMAPPattern's wrapper (case folding +
// trivial special cases) around the reference backtracker, so the differential
// test compares full, equivalent behaviour.
func referenceMatchIMAPPattern(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if pattern == "%" {
		return !strings.Contains(name, "/")
	}
	return matchPatternReference(strings.ToLower(pattern), strings.ToLower(name))
}

// TestMatchIMAPPattern_Semantics is a correctness table proving that '*', '%'
// and separator handling are unchanged by the linear-time rewrite. It includes
// cases that require falling back from a stuck '%' to an earlier '*' (a naive
// single-anchor greedy would get these wrong).
func TestMatchIMAPPattern_Semantics(t *testing.T) {
	cases := []struct {
		pattern string
		name    string
		want    bool
	}{
		// '*' spans the hierarchy separator, '%' does not.
		{"*", "a/b/c", true},
		{"%", "a/b/c", false},
		{"%", "abc", true},
		{"a/*", "a/b/c", true},
		{"a/%", "a/b", true},
		{"a/%", "a/b/c", false},
		{"a/%/c", "a/b/c", true},
		// Both wildcards match zero characters.
		{"a*", "a", true},
		{"a%", "a", true},
		{"*a", "a", true},
		{"%a", "a", true},
		// '%' matches a run of non-separator chars but not across '/'.
		{"a%c", "abc", true},
		{"a%c", "ab/c", false},
		{"a*c", "ab/c", true},
		// Trailing wildcards vs. a remaining separator.
		{"a%", "a/b", false},
		{"a*", "a/b", true},
		// Multiple stars (the shape that made the old matcher explode) still
		// produce the correct answer.
		{"*a*b*c*", "xaybzc", true},
		{"*a*b*c*d", "xaybzc", false},
		// A stuck '%' must fall back to an earlier '*' that CAN cross '/'.
		{"*a%b", "a/axb", true},
		{"*x%y", "a/x_/y", false},
		{"*/%", "a/b/c", true},
		{"%/*", "a/b/c", true},
		// Exact and case-insensitive.
		{"INBOX", "inbox", true},
		{"inbox/sent", "INBOX/Sent", true},
		{"", "", true},
		{"", "x", false},
	}
	for _, c := range cases {
		if got := matchIMAPPattern(c.pattern, c.name); got != c.want {
			t.Errorf("matchIMAPPattern(%q, %q) = %v, want %v", c.pattern, c.name, got, c.want)
		}
		// The oracle must agree with the expected value too (guards the table).
		if ref := referenceMatchIMAPPattern(c.pattern, c.name); ref != c.want {
			t.Errorf("reference(%q, %q) = %v, want %v (bad table entry)", c.pattern, c.name, ref, c.want)
		}
	}
}

// TestMatchIMAPPattern_DifferentialFuzz asserts the linear matcher agrees with
// the reference backtracker across many randomized small inputs, proving the
// matched set is unchanged. Inputs are kept small and low-wildcard so the
// exponential reference stays fast.
func TestMatchIMAPPattern_DifferentialFuzz(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	const alphabet = "ab/%*" // small alphabet rich in separators and wildcards
	randStr := func(maxLen int) string {
		var b strings.Builder
		for l := rng.Intn(maxLen + 1); l > 0; l-- {
			b.WriteByte(alphabet[rng.Intn(len(alphabet))])
		}
		return b.String()
	}
	for i := 0; i < 20000; i++ {
		pattern := randStr(8) // bounded wildcard count keeps the oracle cheap
		name := strings.NewReplacer("*", "a", "%", "b").Replace(randStr(10))
		got := matchIMAPPattern(pattern, name)
		want := referenceMatchIMAPPattern(pattern, name)
		if got != want {
			t.Fatalf("mismatch: matchIMAPPattern(%q, %q) = %v, reference = %v", pattern, name, got, want)
		}
	}
}

// TestMatchIMAPPattern_NoExponentialBlowup is the pathological case from issue
// #31: a pattern with many '*' against a long non-matching name drove the old
// recursive matcher into exponential (effectively unbounded) work — an
// authenticated CPU DoS. The linear matcher completes in microseconds. This
// test is RED (never completes within the budget) on the old code and GREEN
// afterwards.
func TestMatchIMAPPattern_NoExponentialBlowup(t *testing.T) {
	pattern := strings.Repeat("*", 20) + "b"
	name := strings.Repeat("a", 30) // no trailing 'b' — forces worst-case work

	const budget = 2 * time.Second
	done := make(chan bool, 1)
	start := time.Now()
	go func() { done <- matchIMAPPattern(pattern, name) }()

	select {
	case got := <-done:
		if got {
			t.Fatalf("pattern %q should NOT match %q", pattern, name)
		}
		if elapsed := time.Since(start); elapsed > budget {
			t.Fatalf("match took %v, exceeding budget %v (exponential blowup)", elapsed, budget)
		}
	case <-time.After(budget):
		t.Fatalf("matchIMAPPattern did not complete within %v — exponential blowup (issue #31)", budget)
	}
}

// ---------------------------------------------------------------------------
// BODY[HEADER.FIELDS.NOT (...)] exclusion and folded-header handling (issue #19)
// ---------------------------------------------------------------------------

// HEADER.FIELDS.NOT (RFC 3501 §6.4.5) returns every header EXCEPT the listed
// ones. Previously it was matched by the HEADER.FIELDS prefix and returned only
// the listed header — the exact inverse — leaking a header the client asked to
// omit.
func TestBodySection_HeaderFieldsNot_ExcludesListed(t *testing.T) {
	raw := "From: alice@example.com\r\nSubject: Hello\r\nDate: Mon, 01 Jan 2024\r\n\r\nBody"
	load := func() (string, bool) { return raw, true }

	name, payload, _, ok := bodySection("BODY[HEADER.FIELDS.NOT (Subject)]", load)
	if !ok {
		t.Fatalf("bodySection ok = false")
	}
	if name != "BODY[HEADER.FIELDS.NOT (Subject)]" {
		t.Errorf("response item name = %q, want BODY[HEADER.FIELDS.NOT (Subject)]", name)
	}
	if strings.Contains(payload, "Subject:") {
		t.Errorf(".NOT (Subject) must EXCLUDE the Subject header, but payload contained it: %q", payload)
	}
	if !strings.Contains(payload, "From: alice@example.com") {
		t.Errorf(".NOT (Subject) must include From; got: %q", payload)
	}
	if !strings.Contains(payload, "Date: Mon, 01 Jan 2024") {
		t.Errorf(".NOT (Subject) must include Date; got: %q", payload)
	}
}

// HEADER.FIELDS (the non-NOT form) must keep returning ONLY the listed header —
// the fix for .NOT must not disturb this.
func TestBodySection_HeaderFields_OnlyListed(t *testing.T) {
	raw := "From: alice@example.com\r\nSubject: Hello\r\nDate: Mon, 01 Jan 2024\r\n\r\nBody"
	load := func() (string, bool) { return raw, true }

	name, payload, _, ok := bodySection("BODY[HEADER.FIELDS (Subject)]", load)
	if !ok {
		t.Fatalf("bodySection ok = false")
	}
	if name != "BODY[HEADER.FIELDS (Subject)]" {
		t.Errorf("response item name = %q, want BODY[HEADER.FIELDS (Subject)]", name)
	}
	if !strings.Contains(payload, "Subject: Hello") {
		t.Errorf("HEADER.FIELDS (Subject) must include Subject; got: %q", payload)
	}
	if strings.Contains(payload, "From:") || strings.Contains(payload, "Date:") {
		t.Errorf("HEADER.FIELDS (Subject) must return ONLY Subject; got: %q", payload)
	}
}

// A folded header value (RFC 5322 §2.2.3 — continuation lines begin with SP/HT)
// must be returned in full, not truncated to its first line.
func TestFilterHeaders_FoldedValueIncluded(t *testing.T) {
	raw := "From: alice@example.com\r\n" +
		"Subject: This is a very long subject line that has been\r\n" +
		" folded onto a second continuation line\r\n" +
		"Date: Mon, 01 Jan 2024\r\n\r\nBody"

	result := filterHeaders(raw, []string{"Subject"})

	if !strings.Contains(result, "Subject: This is a very long subject line that has been") {
		t.Errorf("missing first Subject line; got: %q", result)
	}
	if !strings.Contains(result, " folded onto a second continuation line") {
		t.Errorf("folded continuation line dropped (truncated header); got: %q", result)
	}
	// The excluded headers must still be excluded.
	if strings.Contains(result, "From:") || strings.Contains(result, "Date:") {
		t.Errorf("HEADER.FIELDS (Subject) leaked another header; got: %q", result)
	}
}

// Under .NOT, a non-excluded header that is folded must be kept in full, and a
// folded EXCLUDED header must be dropped entirely (including its continuations).
func TestBodySection_HeaderFieldsNot_FoldedHandling(t *testing.T) {
	raw := "Subject: line one\r\n" +
		" continued line two\r\n" +
		"From: alice@example.com\r\n" +
		" name continuation\r\n" +
		"\r\nBody"
	load := func() (string, bool) { return raw, true }

	_, payload, _, ok := bodySection("BODY[HEADER.FIELDS.NOT (From)]", load)
	if !ok {
		t.Fatalf("bodySection ok = false")
	}
	if !strings.Contains(payload, "Subject: line one") || !strings.Contains(payload, " continued line two") {
		t.Errorf(".NOT (From) must include the full folded Subject; got: %q", payload)
	}
	if strings.Contains(payload, "From:") || strings.Contains(payload, " name continuation") {
		t.Errorf(".NOT (From) must drop From and its folded continuation; got: %q", payload)
	}
}
