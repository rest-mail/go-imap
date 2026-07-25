package imap

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"net/textproto"
	"sort"
	"strconv"
	"strings"
)

// parseIMAPCommand parses an IMAP command line into tag, command, and arguments.
// IMAP format: <tag> <command> [<args>]
func parseIMAPCommand(line string) (tag, cmd, args string) {
	parts := strings.SplitN(line, " ", 3)
	if len(parts) < 2 {
		return "", "", ""
	}
	tag = parts[0]
	cmd = parts[1]
	if len(parts) > 2 {
		args = parts[2]
	}
	return
}

// parseIMAPArgs splits IMAP arguments respecting quoted strings and parenthesized lists.
func parseIMAPArgs(args string) []string {
	var result []string
	args = strings.TrimSpace(args)
	i := 0

	for i < len(args) {
		// Skip whitespace
		for i < len(args) && args[i] == ' ' {
			i++
		}
		if i >= len(args) {
			break
		}

		switch args[i] {
		case '"':
			// Quoted string — find the closing quote, skipping any
			// backslash-escaped character so an escaped \" does not terminate
			// the string early (RFC 3501 §4.3). The token is kept with its
			// surrounding quotes and escapes intact; unquote decodes it.
			end := quotedStringEnd(args[i:])
			if end == -1 {
				// Unterminated quoted string — take the remainder.
				result = append(result, args[i:])
				i = len(args)
			} else {
				result = append(result, args[i:i+end+1])
				i = i + end + 1
			}
		case '(':
			// Parenthesized list — find closing paren
			depth := 1
			j := i + 1
			for j < len(args) && depth > 0 {
				switch args[j] {
				case '(':
					depth++
				case ')':
					depth--
				}
				j++
			}
			result = append(result, args[i:j])
			i = j
		default:
			// Unquoted token — read until space or special char
			j := i
			for j < len(args) && args[j] != ' ' && args[j] != '(' && args[j] != ')' {
				j++
			}
			result = append(result, args[i:j])
			i = j
		}
	}

	return result
}

// parseStatusItems parses the parenthesized status-item list of a STATUS command
// (RFC 3501 §6.3.10), e.g. "(MESSAGES UIDNEXT UIDVALIDITY)", returning the item
// names upper-cased (status data items are atoms, so case-insensitive). ok is
// false when tok is not a parenthesized list — a bare "MESSAGES" or a missing
// list — which the caller rejects as a BAD syntax error. An empty list "()"
// yields ok true with no items; the caller likewise rejects that, since the
// ABNF requires at least one item.
func parseStatusItems(tok string) (items []string, ok bool) {
	if len(tok) < 2 || tok[0] != '(' || tok[len(tok)-1] != ')' {
		return nil, false
	}
	for _, f := range strings.Fields(tok[1 : len(tok)-1]) {
		items = append(items, strings.ToUpper(f))
	}
	return items, true
}

// quotedStringEnd returns the index, within s, of the closing double quote of
// the IMAP quoted-string that s begins with (s[0] must be '"'). It skips
// backslash-escaped characters so an escaped \" does not close the string
// (RFC 3501 §4.3). It returns -1 when the string is unterminated.
func quotedStringEnd(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			// Skip the escaped character; a trailing backslash escapes nothing.
			if i+1 < len(s) {
				i++
			}
		case '"':
			return i
		}
	}
	return -1
}

// unescapeQuoted decodes the backslash escapes of an IMAP quoted-string body
// (the text between the surrounding quotes). Per RFC 3501 §4.3 the only two
// escapes are \" -> " and \\ -> \; a backslash before any other character, or a
// trailing backslash, is preserved as a literal backslash.
func unescapeQuoted(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
			i++ // drop the backslash, emit the escaped character below
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// unquote removes the surrounding double quotes from an IMAP quoted-string and
// decodes its backslash escapes (\" -> ", \\ -> \; RFC 3501 §4.3). A value that
// is not a quoted-string (an atom) is returned unchanged — atoms carry no
// escapes. This is the counterpart to quoteString, which escapes on output.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return unescapeQuoted(s[1 : len(s)-1])
	}
	return s
}

// decodeBase64 decodes a base64-encoded string.
func decodeBase64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}

// buildFlags returns the IMAP flag string for a message.
func buildFlags(msg Message) string {
	var flags []string
	if msg.Seen {
		flags = append(flags, `\Seen`)
	}
	if msg.Answered {
		flags = append(flags, `\Answered`)
	}
	if msg.Flagged {
		flags = append(flags, `\Flagged`)
	}
	if msg.Draft {
		flags = append(flags, `\Draft`)
	}
	return strings.Join(flags, " ")
}

// buildEnvelope constructs an IMAP ENVELOPE response from a message, per
// RFC 3501 §7.4.2. The ten fields, in order, are: date, subject, from, sender,
// reply-to, to, cc, bcc, in-reply-to and message-id. Per the RFC, the sender and
// reply-to fields default to the from address when no distinct value is available.
//
// When the raw message is available (rawOK), the recipient and reference fields
// (to, cc, bcc, in-reply-to, message-id) and the envelope date are read from the
// message's own headers — the date from the Date: header specifically, which is
// distinct from INTERNALDATE (the arrival time, Message.Date). Without the raw
// bytes the envelope degrades to the neutral Message model: from/sender/reply-to
// from Message.From, to from Message.To, the date from Message.Date, and cc, bcc,
// in-reply-to and message-id NIL.
func buildEnvelope(msg Message, raw string, rawOK bool) string {
	// Parse the header block once when the raw message is available. A parse
	// failure leaves hdr nil, so every field falls back to the Message model.
	var hdr mail.Header
	if rawOK {
		if m, err := mail.ReadMessage(strings.NewReader(raw)); err == nil {
			hdr = m.Header
		}
	}

	// date: the message's Date: header when present and parseable; otherwise the
	// arrival time (Message.Date). INTERNALDATE is a separate concept and is not
	// derived here — the two must not be conflated (RFC 3501 §7.4.2 vs §2.3.3).
	date := msg.Date
	if hdr != nil {
		if d, err := hdr.Date(); err == nil {
			date = d
		}
	}
	dateField := quoteString(date.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	subject := quoteString(msg.Subject)

	// from/sender/reply-to are single-address fields; sender and reply-to default
	// to from when their own header is absent, and from itself falls back to the
	// Message model's From.
	from := headerAddressList(hdr, "From", buildAddress(msg.From.Name, msg.From.Email))
	sender := headerAddressList(hdr, "Sender", from)
	replyTo := headerAddressList(hdr, "Reply-To", from)

	// to/cc/bcc are recipient lists. to falls back to the Message model's To
	// string; cc and bcc have no model source, so an absent header is NIL. Bcc is
	// usually absent in stored messages (correctly reported NIL) but is populated
	// when the header is present.
	to := headerAddressList(hdr, "To", buildAddressList(msg.To))
	cc := headerAddressList(hdr, "Cc", "NIL")
	bcc := headerAddressList(hdr, "Bcc", "NIL")

	// in-reply-to and message-id are message-id references, reported as the raw
	// header value (angle brackets preserved); an absent header is NIL.
	inReplyTo := nilOrQuote(headerValue(hdr, "In-Reply-To"))
	messageID := nilOrQuote(headerValue(hdr, "Message-Id"))

	// date subject from sender reply-to to cc bcc in-reply-to message-id
	return fmt.Sprintf("(%s %s %s %s %s %s %s %s %s %s)",
		dateField, subject, from, sender, replyTo, to, cc, bcc, inReplyTo, messageID)
}

// headerAddressList renders the named address-list header as an IMAP envelope
// address field, parsing it with the same buildAddressList that backs the To
// field. It returns fallback when the header is absent (or hdr is nil because the
// raw message was unavailable), and also when the header is present but parses to
// no address — so a From carrying an unparseable value degrades to the Message
// model rather than to NIL.
func headerAddressList(hdr mail.Header, name, fallback string) string {
	if hdr == nil {
		return fallback
	}
	v := strings.TrimSpace(hdr.Get(name))
	if v == "" {
		return fallback
	}
	if list := buildAddressList(v); list != "NIL" {
		return list
	}
	return fallback
}

// headerValue returns the trimmed value of the named header, or "" when it is
// absent or the raw message was unavailable (hdr is nil).
func headerValue(hdr mail.Header, name string) string {
	if hdr == nil {
		return ""
	}
	return strings.TrimSpace(hdr.Get(name))
}

// addressPart constructs a single IMAP address structure "(name NIL mailbox
// host)" — personal name, source-route (always NIL), mailbox name and host name
// (RFC 3501 §7.4.2). It is the inner element of the parenthesized address lists
// that make up the from/sender/reply-to/to/cc/bcc envelope fields.
func addressPart(name, email string) string {
	parts := strings.SplitN(email, "@", 2)
	user := parts[0]
	host := ""
	if len(parts) > 1 {
		host = parts[1]
	}
	return fmt.Sprintf("(%s NIL %s %s)", quoteString(name), quoteString(user), quoteString(host))
}

// buildAddress wraps a single address in the parenthesized address-list form an
// envelope field takes, e.g. ((name NIL mailbox host)). An empty address is NIL.
func buildAddress(name, email string) string {
	if email == "" {
		return "NIL"
	}
	return "(" + addressPart(name, email) + ")"
}

// buildAddressList parses an RFC 5322 address-list header value (e.g.
// "Bob <bob@example.org>, carol@example.com") into the parenthesized list of
// address structures an envelope field requires. An empty or unparseable value
// yields NIL, matching the RFC's treatment of an absent header.
func buildAddressList(list string) string {
	list = strings.TrimSpace(list)
	if list == "" {
		return "NIL"
	}
	addrs, err := mail.ParseAddressList(list)
	if err != nil || len(addrs) == 0 {
		return "NIL"
	}
	var b strings.Builder
	b.WriteByte('(')
	for _, a := range addrs {
		b.WriteString(addressPart(a.Name, a.Address))
	}
	b.WriteByte(')')
	return b.String()
}

// quoteString wraps a string in IMAP quotes, or returns NIL for empty.
func quoteString(s string) string {
	if s == "" {
		return "NIL"
	}
	// Escape backslashes and double quotes
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// encodeMailboxName encodes a mailbox name for output in a response as an IMAP
// astring (RFC 3501 §4.3, §9 — a mailbox name is an astring, and §7.2.2/§7.2.3
// render it that way in LIST and STATUS). A name a quoted-string can carry is
// emitted quoted, with the quoted-specials '"' and '\' backslash-escaped so an
// embedded quote or backslash cannot terminate the string early or corrupt the
// line. A name containing CR, LF, NUL, or an 8-bit octet — none of which a
// quoted-string may hold (its content is 7-bit TEXT-CHAR excluding CR/LF) — is
// emitted as a synchronizing literal, so those octets travel verbatim and,
// critically, a CR/LF in a backend-supplied folder name cannot inject additional
// response lines. An empty name encodes as "" (never NIL): a mailbox name is
// never absent. This is the encoder EVERY mailbox name emitted in a response must
// pass through; interpolating a raw name would break client parsing and, on
// CR/LF, allow response injection.
func encodeMailboxName(name string) string {
	if mailboxNeedsLiteral(name) {
		return fmt.Sprintf("{%d}\r\n%s", len(name), name)
	}
	name = strings.ReplaceAll(name, `\`, `\\`)
	name = strings.ReplaceAll(name, `"`, `\"`)
	return `"` + name + `"`
}

// mailboxNeedsLiteral reports whether name contains an octet a quoted-string
// cannot represent — CR, LF, NUL, or any 8-bit octet — and so must be sent as a
// literal instead (RFC 3501 §4.3).
func mailboxNeedsLiteral(name string) bool {
	for i := 0; i < len(name); i++ {
		if c := name[i]; c == '\r' || c == '\n' || c == 0x00 || c >= 0x80 {
			return true
		}
	}
	return false
}

// parseSequenceSet parses an IMAP sequence set like "1", "1:5", "1,3,5", "1:*".
func parseSequenceSet(seqStr string, total int) []int {
	if total == 0 {
		return nil
	}

	var result []int
	seen := make(map[int]bool)

	for _, part := range strings.Split(seqStr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, ":") {
			// Range
			rangeParts := strings.SplitN(part, ":", 2)
			start := resolveSeqNum(rangeParts[0], total)
			end := resolveSeqNum(rangeParts[1], total)

			if start > end {
				start, end = end, start
			}
			// Clamp to [1, total] BEFORE iterating. A crafted set such as
			// "1:4294967295" against a 3-message mailbox would otherwise loop
			// ~4.3 billion times to yield the same three sequence numbers — an
			// authenticated CPU DoS (issue #8). Clamping bounds the loop to the
			// messages that can actually exist.
			if start < 1 {
				start = 1
			}
			if end > total {
				end = total
			}
			for i := start; i <= end; i++ {
				if !seen[i] {
					result = append(result, i)
					seen[i] = true
				}
			}
		} else {
			// Single number
			n := resolveSeqNum(part, total)
			if n >= 1 && n <= total && !seen[n] {
				result = append(result, n)
				seen[n] = true
			}
		}
	}

	return result
}

// resolveSeqNum resolves a sequence number, handling "*" as the total count.
func resolveSeqNum(s string, total int) int {
	s = strings.TrimSpace(s)
	if s == "*" {
		return total
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}

// seqRange is a normalized, inclusive [start, end] span from an IMAP sequence
// set. It records the bounds only — it is never expanded into individual
// numbers, so membership testing is O(1) regardless of how wide the declared
// span is.
type seqRange struct {
	start, end uint32
}

// parseUIDRanges parses an IMAP UID set (e.g. "1,3:5,100:*") into normalized
// inclusive ranges WITHOUT expanding them. A pathological span such as
// "1:4294967295" therefore costs O(number of comma parts) rather than
// O(span) — the fix for the quadratic UID SEARCH DoS in issue #8, where the set
// was previously re-expanded once per message. "*" resolves to maxUID (the
// largest UID currently in the mailbox); a part whose "*" endpoint has no UID to
// resolve to (empty mailbox) or that fails to parse is dropped.
func parseUIDRanges(setStr string, maxUID uint32) []seqRange {
	var ranges []seqRange
	for _, part := range strings.Split(setStr, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, ":") {
			rangeParts := strings.SplitN(part, ":", 2)
			start, ok1 := parseUIDBound(rangeParts[0], maxUID)
			end, ok2 := parseUIDBound(rangeParts[1], maxUID)
			if !ok1 || !ok2 {
				continue
			}
			if start > end {
				start, end = end, start
			}
			ranges = append(ranges, seqRange{start: start, end: end})
		} else {
			v, ok := parseUIDBound(part, maxUID)
			if !ok {
				continue
			}
			ranges = append(ranges, seqRange{start: v, end: v})
		}
	}
	return ranges
}

// parseUIDBound resolves one endpoint of a UID range. "*" becomes maxUID (valid
// only when the mailbox holds at least one message); a non-numeric or
// out-of-uint32 token is rejected.
func parseUIDBound(s string, maxUID uint32) (uint32, bool) {
	s = strings.TrimSpace(s)
	if s == "*" {
		return maxUID, maxUID > 0
	}
	v, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false
	}
	return uint32(v), true
}

// uidInRanges reports whether uid falls within any of the given ranges. It is
// the membership test that replaces per-message range expansion.
func uidInRanges(uid uint32, ranges []seqRange) bool {
	for _, r := range ranges {
		if uid >= r.start && uid <= r.end {
			return true
		}
	}
	return false
}

// parseAstring consumes a leading IMAP astring — a quoted-string or an atom
// (RFC 3501 §4.3 / §9) — from s, returning the decoded value and the remainder
// of s after the token. ok is false when s does not begin with a well-formed
// astring (e.g. an unterminated quoted-string). Leading spaces are skipped.
//
// Literal astrings ({n}...) are intentionally not handled here: APPEND's target
// mailbox is taken from the command line, where clients send it either quoted or
// as a bare atom. An atom runs up to the first space, '(' or '{' — the grammar's
// separators between the mailbox and an optional flag-list or the message
// literal — so it is decoded even without surrounding quotes.
func parseAstring(s string) (val, rest string, ok bool) {
	s = strings.TrimLeft(s, " ")
	if s == "" {
		return "", "", false
	}
	if s[0] == '"' {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			switch c := s[i]; c {
			case '\\': // quoted-specials are backslash-escaped inside a quoted-string
				if i+1 >= len(s) {
					return "", "", false
				}
				i++
				b.WriteByte(s[i])
			case '"':
				return b.String(), s[i+1:], true
			default:
				b.WriteByte(c)
			}
		}
		return "", "", false // unterminated quoted-string
	}
	if end := strings.IndexAny(s, " ({"); end >= 0 {
		return s[:end], s[end:], true
	}
	return s, "", true
}

// splitTrailingLiteral inspects the tail of a command line for a literal-length
// marker: a synchronizing literal "{n}" or a LITERAL+ non-synchronizing literal
// "{n+}" (RFC 3501 §4.3, RFC 2088). A literal marker is only ever the final token
// of a command line — the octets follow on the next physical line — so only a
// line ending in '}' can carry one. On a match it returns the text preceding the
// marker, the declared octet count, and sync (true for "{n}", false for "{n+}").
// isLit is false when the tail is not a well-formed marker — a line not ending in
// '}', no '{', an empty or non-numeric count, or a signed/garbage count — in
// which case the line has no trailing literal and is parsed verbatim.
//
// The count is parsed with ParseUint (base 10, no sign), so "{+5}" or "{5x}" are
// rejected rather than misread; the caller separately bounds the value against
// MaxLiteralSize.
func splitTrailingLiteral(line string) (prefix string, size int, sync, isLit bool) {
	if !strings.HasSuffix(line, "}") {
		return "", 0, false, false
	}
	open := strings.LastIndexByte(line, '{')
	if open < 0 {
		return "", 0, false, false
	}
	inner := line[open+1 : len(line)-1]
	sync = true
	if strings.HasSuffix(inner, "+") { // LITERAL+ non-synchronizing form
		sync = false
		inner = inner[:len(inner)-1]
	}
	n, err := strconv.ParseUint(inner, 10, 32)
	if err != nil {
		return "", 0, false, false
	}
	return line[:open], int(n), sync, true
}

// quoteLiteral re-encodes literal octets as an IMAP quoted-string so they can be
// spliced back into the command line in place of the "{n}" marker and consumed by
// the existing astring parsers (unquote / parseIMAPArgs). Any embedded '"' or
// '\' is backslash-escaped (RFC 3501 §4.3) so it cannot terminate the string
// early. Unlike quoteString it emits "" (never NIL) for empty input, so a {0}
// literal becomes an explicit empty-string argument rather than a NIL token.
func quoteLiteral(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b) + 2)
	sb.WriteByte('"')
	for _, c := range b {
		if c == '"' || c == '\\' {
			sb.WriteByte('\\')
		}
		sb.WriteByte(c)
	}
	sb.WriteByte('"')
	return sb.String()
}

// parseFlags extracts IMAP flags from a parenthesized list like "(\Seen \Flagged)".
func parseFlags(s string) []string {
	s = strings.TrimSpace(s)
	// Remove surrounding parentheses
	s = strings.TrimPrefix(s, "(")
	s = strings.TrimSuffix(s, ")")

	var flags []string
	for _, f := range strings.Fields(s) {
		if f != "" {
			flags = append(flags, f)
		}
	}
	return flags
}

// extractHeaderFieldNames extracts the header field names from BODY[HEADER.FIELDS (...)].
func extractHeaderFieldNames(dataItems string) []string {
	// Find the parenthesized list after HEADER.FIELDS
	start := strings.Index(strings.ToUpper(dataItems), "HEADER.FIELDS")
	if start < 0 {
		return nil
	}
	rest := dataItems[start:]
	parenStart := strings.Index(rest, "(")
	parenEnd := strings.Index(rest, ")")
	if parenStart < 0 || parenEnd < 0 {
		return nil
	}
	fieldStr := rest[parenStart+1 : parenEnd]
	var fields []string
	for _, f := range strings.Fields(fieldStr) {
		fields = append(fields, strings.TrimSpace(f))
	}
	return fields
}

// filterHeaders extracts only the requested headers from a raw RFC 2822 message
// (the BODY[HEADER.FIELDS (...)] section), terminated by a blank line.
func filterHeaders(raw string, fields []string) string {
	return selectHeaders(raw, fields, false)
}

// selectHeaders returns header lines from raw's RFC 5322 header block, terminated
// by the blank line that ends a header section. Field-name matching is
// case-insensitive. With exclude=false only headers whose name appears in fields
// are kept (HEADER.FIELDS); with exclude=true every header EXCEPT those in fields
// is kept (HEADER.FIELDS.NOT, RFC 3501 §6.4.5). A folded header — one whose value
// continues on following lines beginning with SP or HT (RFC 5322 §2.2.3) — is kept
// or dropped as a whole together with its parent header, so folded values are
// returned in full rather than truncated to their first line.
func selectHeaders(raw string, fields []string, exclude bool) string {
	headerEnd := strings.Index(raw, "\r\n\r\n")
	headerSection := raw
	if headerEnd >= 0 {
		headerSection = raw[:headerEnd]
	}

	// Build a set of field names to match (case-insensitive).
	set := make(map[string]bool, len(fields))
	for _, f := range fields {
		set[strings.ToLower(strings.TrimSpace(f))] = true
	}

	var result strings.Builder
	keep := false // whether the header currently being read is selected
	for _, line := range strings.Split(headerSection, "\r\n") {
		if line == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			// Continuation of the previous (folded) header value: keep or drop
			// it with the header it belongs to.
			if keep {
				result.WriteString(line + "\r\n")
			}
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			keep = false
			continue
		}
		name := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
		// exclude=false: keep when listed; exclude=true: keep when NOT listed.
		keep = set[name] != exclude
		if keep {
			result.WriteString(line + "\r\n")
		}
	}
	result.WriteString("\r\n") // blank line to end headers
	return result.String()
}

// fetchItemTokens splits a FETCH data-item argument into individual item tokens,
// respecting bracketed sections and nested parenthesized lists so that, e.g.,
// "(FLAGS RFC822.SIZE BODY.PEEK[HEADER.FIELDS (DATE FROM)])" yields
// ["FLAGS", "RFC822.SIZE", "BODY.PEEK[HEADER.FIELDS (DATE FROM)]"]. A single
// unparenthesized item such as "RFC822.SIZE" is returned as one token. Splitting
// on exact items (rather than substring matching) is what keeps RFC822.SIZE from
// being mistaken for an RFC822 body fetch.
func fetchItemTokens(dataItems string) []string {
	s := strings.TrimSpace(dataItems)
	// Strip one layer of the outer "( ... )" list wrapper, if present.
	if strings.HasPrefix(s, "(") && strings.HasSuffix(s, ")") {
		s = s[1 : len(s)-1]
	}

	var tokens []string
	var cur strings.Builder
	depth := 0 // nesting depth of [] and ()
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '[', '(':
			depth++
			cur.WriteByte(c)
		case ']', ')':
			if depth > 0 {
				depth--
			}
			cur.WriteByte(c)
		case ' ':
			if depth == 0 {
				if cur.Len() > 0 {
					tokens = append(tokens, cur.String())
					cur.Reset()
				}
			} else {
				cur.WriteByte(c)
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return expandFetchMacros(tokens)
}

// expandFetchMacros replaces an RFC 3501 §6.4.5 FETCH macro with its component
// data items. A macro (ALL, FAST, FULL) is only meaningful as the sole data item
// — the grammar makes it an alternative to a data item or a parenthesized list,
// not a member of one — so expansion applies only when tokens is exactly one
// macro word; any other token list is returned unchanged.
//
//	ALL  = FLAGS INTERNALDATE RFC822.SIZE ENVELOPE
//	FAST = FLAGS INTERNALDATE RFC822.SIZE
//	FULL = FLAGS INTERNALDATE RFC822.SIZE ENVELOPE BODY
func expandFetchMacros(tokens []string) []string {
	if len(tokens) != 1 {
		return tokens
	}
	switch strings.ToUpper(tokens[0]) {
	case "ALL":
		return []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE"}
	case "FAST":
		return []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE"}
	case "FULL":
		return []string{"FLAGS", "INTERNALDATE", "RFC822.SIZE", "ENVELOPE", "BODY"}
	default:
		return tokens
	}
}

// headerSection returns the RFC 2822 header block of raw, including the blank
// line that terminates it.
func headerSection(raw string) string {
	if end := strings.Index(raw, "\r\n\r\n"); end >= 0 {
		return raw[:end+4]
	}
	return raw
}

// textSection returns the body of raw (everything after the header-terminating
// blank line), or "" if there is none.
func textSection(raw string) string {
	if end := strings.Index(raw, "\r\n\r\n"); end >= 0 && end+4 < len(raw) {
		return raw[end+4:]
	}
	return ""
}

// bodySection answers a BODY[...] / BODY.PEEK[...] fetch item. It returns the
// response item name (always the non-PEEK BODY[...] form, per RFC 3501 §7.4.2),
// the octet payload, and whether the request was a peek (a peek must not set
// \Seen). ok is false when the body could not be loaded or the item is
// malformed. loadRaw fetches the raw message lazily, so callers pay for the body
// only when a section actually needs it.
func bodySection(item string, loadRaw func() (string, bool)) (name, payload string, peek, ok bool) {
	up := strings.ToUpper(item)
	peek = strings.HasPrefix(up, "BODY.PEEK[")
	lb := strings.IndexByte(item, '[')
	rb := strings.LastIndexByte(item, ']')
	if lb < 0 || rb < 0 || rb < lb {
		return "", "", peek, false
	}
	section := strings.ToUpper(item[lb+1 : rb])

	// A body item may carry a "<start.count>" partial specifier after the section
	// bracket (RFC 3501 §6.4.5). Parse it before loading the body so a malformed
	// specifier is rejected without paying for a fetch.
	partial, pok := parseBodyPartial(item[rb+1:])
	if !pok {
		return "", "", peek, false
	}

	raw, rok := loadRaw()
	if !rok {
		return "", "", peek, false
	}

	switch {
	case section == "":
		name, payload = "BODY[]", raw
	case section == "HEADER":
		name, payload = "BODY[HEADER]", headerSection(raw)
	case section == "TEXT":
		name, payload = "BODY[TEXT]", textSection(raw)
	case strings.HasPrefix(section, "HEADER.FIELDS.NOT"):
		// HEADER.FIELDS.NOT (f...) returns every header EXCEPT the listed ones
		// (RFC 3501 §6.4.5). Checked before the HEADER.FIELDS prefix below, which
		// it would otherwise match.
		fields := extractHeaderFieldNames(item)
		name, payload = "BODY[HEADER.FIELDS.NOT ("+strings.Join(fields, " ")+")]", selectHeaders(raw, fields, true)
	case strings.HasPrefix(section, "HEADER.FIELDS"):
		fields := extractHeaderFieldNames(item)
		name, payload = "BODY[HEADER.FIELDS ("+strings.Join(fields, " ")+")]", filterHeaders(raw, fields)
	default:
		// Unsupported section spec (e.g. a MIME part number) — fall back to the
		// full message rather than nothing, preserving prior lenient behavior.
		name, payload = "BODY["+section+"]", raw
	}

	// A partial fetch returns only the requested octets and labels the response
	// item with the origin octet — "BODY[section]<start>", the start ONLY, never
	// the count (RFC 3501 §7.4.2).
	if partial != nil {
		payload = partialOctets(payload, partial.start, partial.count)
		name = fmt.Sprintf("%s<%d>", name, partial.start)
	}

	return name, payload, peek, true
}

// bodyPartial is the parsed "<start.count>" partial specifier of a FETCH body
// item (RFC 3501 §6.4.5). count is -1 when only a start was given (accepted
// leniently — the request grammar normally requires both), meaning "return to
// the end of the section".
type bodyPartial struct {
	start int
	count int
}

// parseBodyPartial parses the text following a body section's closing ']'. An
// empty rem means no partial was requested (nil, true — not an error). A
// well-formed "<start>" or "<start.count>" yields the parsed spec. Anything else
// is malformed (nil, false).
func parseBodyPartial(rem string) (*bodyPartial, bool) {
	if rem == "" {
		return nil, true
	}
	if len(rem) < 2 || rem[0] != '<' || rem[len(rem)-1] != '>' {
		return nil, false
	}
	startStr, countStr, hasCount := strings.Cut(rem[1:len(rem)-1], ".")
	start, err := strconv.Atoi(startStr)
	if err != nil || start < 0 {
		return nil, false
	}
	count := -1
	if hasCount {
		c, err := strconv.Atoi(countStr)
		if err != nil || c < 0 {
			return nil, false
		}
		count = c
	}
	return &bodyPartial{start: start, count: count}, true
}

// partialOctets returns at most count octets of s beginning at zero-based octet
// start (RFC 3501 §6.4.5): fewer if the section is shorter, empty if start is at
// or past the end. count < 0 means "to the end of the section".
func partialOctets(s string, start, count int) string {
	if start >= len(s) {
		return ""
	}
	s = s[start:]
	if count >= 0 && count < len(s) {
		s = s[:count]
	}
	return s
}

// buildBodyStructure parses a raw RFC 5322 message and returns its IMAP body
// structure as a correctly-formed parenthesized structure (RFC 3501 §7.4.2).
// With extension=false it produces the BODY (non-extensible) form; with
// extension=true it produces BODYSTRUCTURE, appending the extension fields
// (MD5/disposition/language/location for single parts; parameters/disposition/
// language/location for multiparts) as NIL where the Message model has no source.
// The whole message body is always described — never a NIL or empty structure —
// so the response is well-formed even for unusual or unparseable input.
func buildBodyStructure(raw string, extension bool) string {
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		// Unparseable header block: describe the entire input as one text/plain
		// part so the structure is still well-formed.
		return singlePartStructure("text", "plain", nil, textproto.MIMEHeader{}, []byte(raw), extension)
	}
	body, _ := io.ReadAll(msg.Body)
	return partStructure(textproto.MIMEHeader(msg.Header), body, extension)
}

// partStructure builds the body structure for one MIME entity given its header
// and (transfer-encoded) body octets, recursing into multipart entities.
func partStructure(header textproto.MIMEHeader, body []byte, extension bool) string {
	mediatype, params, err := mime.ParseMediaType(header.Get("Content-Type"))
	if err != nil || mediatype == "" {
		// Absent or malformed Content-Type defaults to text/plain (RFC 2045 §5.2).
		mediatype, params = "text/plain", map[string]string{}
	}
	maintype, subtype := mediatype, ""
	if slash := strings.IndexByte(mediatype, '/'); slash >= 0 {
		maintype, subtype = mediatype[:slash], mediatype[slash+1:]
	}

	if maintype == "multipart" && params["boundary"] != "" {
		return multipartStructure(subtype, params, header, body, extension)
	}
	return singlePartStructure(maintype, subtype, params, header, body, extension)
}

// multipartStructure builds the parenthesized structure for a multipart entity:
// the nested part structures concatenated with no separator, then the subtype,
// then (for BODYSTRUCTURE) the extension fields (RFC 3501 §7.4.2). header is the
// multipart's own header, the source of its body-ext-mpart disposition field.
func multipartStructure(subtype string, params map[string]string, header textproto.MIMEHeader, body []byte, extension bool) string {
	mr := multipart.NewReader(bytes.NewReader(body), params["boundary"])
	var parts strings.Builder
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		pb, _ := io.ReadAll(p)
		parts.WriteString(partStructure(p.Header, pb, extension))
	}
	// A multipart with no decodable parts is still described as containing one
	// empty text/plain part, keeping the structure well-formed.
	if parts.Len() == 0 {
		parts.WriteString(singlePartStructure("text", "plain", nil, textproto.MIMEHeader{}, nil, extension))
	}

	var b strings.Builder
	b.WriteByte('(')
	b.WriteString(parts.String())
	b.WriteByte(' ')
	b.WriteString(quoteUpper(subtype, "MIXED"))
	if extension {
		// body-ext-mpart: parameters, disposition, language, location. The
		// content-type parameters (including boundary) are reported; disposition is
		// read from the multipart's Content-Disposition when present; language and
		// location have no source.
		b.WriteByte(' ')
		b.WriteString(paramList(params))
		b.WriteByte(' ')
		b.WriteString(dispositionField(header))
		b.WriteString(" NIL NIL")
	}
	b.WriteByte(')')
	return b.String()
}

// singlePartStructure builds the parenthesized structure for a non-multipart
// entity (RFC 3501 §7.4.2): type, subtype, parameters, id, description,
// encoding, size — plus a line count for text types, plus (for BODYSTRUCTURE)
// the single-part extension fields MD5/disposition/language/location.
func singlePartStructure(maintype, subtype string, params map[string]string, header textproto.MIMEHeader, body []byte, extension bool) string {
	if maintype == "" {
		maintype = "text"
	}
	if subtype == "" {
		subtype = "plain"
	}
	// A text part with no explicit charset defaults to US-ASCII (RFC 2045 §5.2).
	if strings.EqualFold(maintype, "text") {
		hasCharset := false
		for k := range params {
			if strings.EqualFold(k, "charset") {
				hasCharset = true
				break
			}
		}
		if !hasCharset {
			merged := map[string]string{"charset": "US-ASCII"}
			for k, v := range params {
				merged[k] = v
			}
			params = merged
		}
	}

	encoding := header.Get("Content-Transfer-Encoding")
	if encoding == "" {
		encoding = "7BIT"
	}

	var b strings.Builder
	b.WriteByte('(')
	b.WriteString(quoteUpper(maintype, "TEXT"))
	b.WriteByte(' ')
	b.WriteString(quoteUpper(subtype, "PLAIN"))
	b.WriteByte(' ')
	b.WriteString(paramList(params))
	fmt.Fprintf(&b, " %s %s %s %d",
		nilOrQuote(header.Get("Content-Id")),
		nilOrQuote(header.Get("Content-Description")),
		quoteString(strings.ToUpper(encoding)),
		len(body))
	switch {
	case strings.EqualFold(maintype, "message") && strings.EqualFold(subtype, "rfc822"):
		// body-type-msg: after the basic fields, the envelope, body structure and
		// line count of the encapsulated message. The part body IS that message,
		// so it is parsed and described recursively (RFC 3501 §7.4.2).
		enc := string(body)
		fmt.Fprintf(&b, " %s %s %d",
			buildEncapsulatedEnvelope(enc),
			buildBodyStructure(enc, extension),
			countLines(body))
	case strings.EqualFold(maintype, "text"):
		// body-type-text adds a line count after the octet size.
		fmt.Fprintf(&b, " %d", countLines(body))
	}
	if extension {
		// body-ext-1part: MD5, disposition, language, location. MD5, language and
		// location have no source in the stored message; disposition is read from
		// the part's Content-Disposition when present.
		fmt.Fprintf(&b, " NIL %s NIL NIL", dispositionField(header))
	}
	b.WriteByte(')')
	return b.String()
}

// dispositionField renders a part's Content-Disposition as an IMAP body-fld-dsp:
// "(" disp-type SP body-fld-param ")" (RFC 3501 §7.4.2), e.g.
// ("ATTACHMENT" ("FILENAME" "doc.pdf")), with the disposition type and parameter
// names uppercased for consistency with the rest of the structure and parameter
// values preserved verbatim. An absent or unparseable Content-Disposition yields
// NIL.
func dispositionField(header textproto.MIMEHeader) string {
	cd := header.Get("Content-Disposition")
	if cd == "" {
		return "NIL"
	}
	disptype, params, err := mime.ParseMediaType(cd)
	if err != nil || disptype == "" {
		return "NIL"
	}
	return "(" + quoteString(strings.ToUpper(disptype)) + " " + paramList(params) + ")"
}

// buildEncapsulatedEnvelope constructs the IMAP ENVELOPE (RFC 3501 §7.4.2) of a
// MESSAGE/RFC822 part's encapsulated message directly from its own headers — date,
// subject, from, sender, reply-to, to, cc, bcc, in-reply-to and message-id — with
// sender and reply-to defaulting to from. It differs from buildEnvelope, which
// draws subject and the fallback addresses from the stored Message model; an
// encapsulated message has no such model, so every field comes from the raw header
// block. A header that cannot be parsed yields NIL, keeping the structure
// well-formed.
func buildEncapsulatedEnvelope(raw string) string {
	m, err := mail.ReadMessage(strings.NewReader(raw))
	if err != nil {
		return "NIL"
	}
	hdr := m.Header

	dateField := "NIL"
	if d, err := hdr.Date(); err == nil {
		dateField = quoteString(d.Format("Mon, 02 Jan 2006 15:04:05 -0700"))
	} else if v := strings.TrimSpace(hdr.Get("Date")); v != "" {
		dateField = quoteString(v)
	}

	subject := nilOrQuote(strings.TrimSpace(hdr.Get("Subject")))
	from := headerAddressList(hdr, "From", "NIL")
	sender := headerAddressList(hdr, "Sender", from)
	replyTo := headerAddressList(hdr, "Reply-To", from)
	to := headerAddressList(hdr, "To", "NIL")
	cc := headerAddressList(hdr, "Cc", "NIL")
	bcc := headerAddressList(hdr, "Bcc", "NIL")
	inReplyTo := nilOrQuote(headerValue(hdr, "In-Reply-To"))
	messageID := nilOrQuote(headerValue(hdr, "Message-Id"))

	return fmt.Sprintf("(%s %s %s %s %s %s %s %s %s %s)",
		dateField, subject, from, sender, replyTo, to, cc, bcc, inReplyTo, messageID)
}

// paramList renders a MIME parameter map as an IMAP parenthesized attribute/value
// list, e.g. ("CHARSET" "US-ASCII"), with attribute names uppercased and keys in
// sorted order for a deterministic result. An empty map yields NIL.
func paramList(params map[string]string) string {
	if len(params) == 0 {
		return "NIL"
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteByte('(')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(quoteString(strings.ToUpper(k)))
		b.WriteByte(' ')
		b.WriteString(quoteString(params[k]))
	}
	b.WriteByte(')')
	return b.String()
}

// quoteUpper returns s uppercased and IMAP-quoted, or fallback (already a bare
// word) quoted when s is empty.
func quoteUpper(s, fallback string) string {
	if s == "" {
		s = fallback
	}
	return quoteString(strings.ToUpper(s))
}

// nilOrQuote quotes s, or returns NIL when s is empty (an absent header field).
func nilOrQuote(s string) string {
	if s == "" {
		return "NIL"
	}
	return quoteString(s)
}

// countLines returns the number of text lines in body — the count RFC 3501's
// body-type-text requires. It counts line terminators, adding one for a final
// unterminated line.
func countLines(body []byte) int {
	if len(body) == 0 {
		return 0
	}
	n := bytes.Count(body, []byte("\n"))
	if body[len(body)-1] != '\n' {
		n++
	}
	return n
}

// canonicalizeInbox folds any-case spelling of the reserved mailbox name INBOX
// to its canonical form "INBOX", and returns every other name unchanged. Per
// RFC 3501 §5.1 only the special name "INBOX" is case-insensitive — "inbox",
// "InBoX" and "INBOX" all denote the one real INBOX — so folder guards and
// backend lookups must treat those spellings identically. All other mailbox
// names remain case-sensitive ("Work" and "work" are distinct mailboxes).
func canonicalizeInbox(name string) string {
	if strings.EqualFold(name, "INBOX") {
		return "INBOX"
	}
	return name
}

// matchIMAPPattern matches a folder name against an IMAP LIST pattern.
// '*' matches any characters including hierarchy separator.
// '%' matches any characters except hierarchy separator '/'.
func matchIMAPPattern(pattern, name string) bool {
	// INBOX is the sole mailbox name matched case-insensitively (RFC 3501 §5.1);
	// every other name is case-sensitive. Fold an INBOX name and an INBOX-spelled
	// pattern to the canonical spelling so "inbox" still lists INBOX, WITHOUT
	// lower-casing everything (which made unrelated names — "sent" vs "Sent" —
	// match case-insensitively too).
	name = canonicalizeInbox(name)
	if strings.EqualFold(pattern, "INBOX") {
		pattern = "INBOX"
	}

	// Simple cases
	if pattern == "*" {
		return true
	}
	if pattern == "%" {
		return !strings.Contains(name, "/")
	}

	return matchPatternGreedy(pattern, name)
}

// matchPatternGreedy matches name against an IMAP LIST pattern in linear time
// using an iterative two-pointer scan with backtrack anchors, instead of the
// naive recursive backtracker whose cost is exponential in the number of
// wildcards (an authenticated CPU DoS). The matched set is identical to the
// recursive definition:
//
//	'*' matches zero or more of any character, including the '/' separator.
//	'%' matches zero or more characters other than the '/' separator.
//
// Two anchors are tracked because the wildcards are not equivalent: when a '%'
// cannot extend across a '/', the match must fall back to the most recent '*'
// (which can), so a single anchor is insufficient. The '%' anchor is discarded
// whenever we fall back to and re-expand the '*', since matching restarts from
// just after the star. Every anchor advances monotonically through the name, so
// the total work is polynomial (no exponential blowup regardless of wildcard
// count).
func matchPatternGreedy(pattern, name string) bool {
	lenP, lenN := len(pattern), len(name)
	p, n := 0, 0

	// Last '*' seen: resume pattern index and the name index it may re-expand to.
	starP, starN := -1, 0
	// Last '%' seen since that '*': resume pattern index and next name index it
	// may consume (only if that char is not the '/' separator). -1 when inactive.
	pctP, pctN := -1, 0

	for n < lenN {
		switch {
		case p < lenP && pattern[p] == '*':
			// Star matches empty for now; record it and forget any earlier '%'.
			starP, starN = p+1, n
			pctP = -1
			p++
		case p < lenP && pattern[p] == '%':
			// Percent matches empty for now; record it.
			pctP, pctN = p+1, n
			p++
		case p < lenP && pattern[p] == name[n]:
			// Literal match.
			p++
			n++
		case pctP != -1 && name[pctN] != '/':
			// Extend the most recent '%' by one non-separator char.
			pctN++
			p, n = pctP, pctN
		case starP != -1:
			// Extend the most recent '*' by one char (any char). Restart from
			// just after the star, discarding stale '%' state.
			starN++
			p, n = starP, starN
			pctP = -1
		default:
			return false
		}
	}

	// Name consumed; only trailing wildcards may remain in the pattern.
	for p < lenP && (pattern[p] == '*' || pattern[p] == '%') {
		p++
	}
	return p == lenP
}
