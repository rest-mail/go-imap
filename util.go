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
			// Quoted string — find closing quote
			end := strings.Index(args[i+1:], `"`)
			if end == -1 {
				result = append(result, args[i:])
				i = len(args)
			} else {
				result = append(result, args[i:i+end+2])
				i = i + end + 2
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

// unquote removes surrounding double quotes from a string.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
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
// reply-to fields default to the from address when no distinct value is
// available. The Message model carries only From and To, so cc, bcc, in-reply-to
// and message-id are reported NIL (their headers are treated as absent).
func buildEnvelope(msg Message) string {
	date := msg.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700")
	subject := quoteString(msg.Subject)

	fromAddr := buildAddress(msg.From.Name, msg.From.Email)
	toAddr := buildAddressList(msg.To)

	// date subject from sender reply-to to cc bcc in-reply-to message-id
	return fmt.Sprintf("(%s %s %s %s %s %s NIL NIL NIL NIL)",
		quoteString(date), subject, fromAddr, fromAddr, fromAddr, toAddr)
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

// filterHeaders extracts only the requested headers from a raw RFC 2822 message.
func filterHeaders(raw string, fields []string) string {
	headerEnd := strings.Index(raw, "\r\n\r\n")
	headerSection := raw
	if headerEnd >= 0 {
		headerSection = raw[:headerEnd]
	}

	// Build a set of requested field names (case-insensitive)
	wanted := make(map[string]bool)
	for _, f := range fields {
		wanted[strings.ToLower(f)] = true
	}

	var result strings.Builder
	for _, line := range strings.Split(headerSection, "\r\n") {
		if line == "" {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(line[:colonIdx]))
		if wanted[name] {
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

	raw, rok := loadRaw()
	if !rok {
		return "", "", peek, false
	}

	switch {
	case section == "":
		return "BODY[]", raw, peek, true
	case section == "HEADER":
		return "BODY[HEADER]", headerSection(raw), peek, true
	case section == "TEXT":
		return "BODY[TEXT]", textSection(raw), peek, true
	case strings.HasPrefix(section, "HEADER.FIELDS"):
		fields := extractHeaderFieldNames(item)
		return "BODY[HEADER.FIELDS (" + strings.Join(fields, " ") + ")]", filterHeaders(raw, fields), peek, true
	default:
		// Unsupported section spec (e.g. a MIME part number) — fall back to the
		// full message rather than nothing, preserving prior lenient behavior.
		return "BODY[" + section + "]", raw, peek, true
	}
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
		return multipartStructure(subtype, params, body, extension)
	}
	return singlePartStructure(maintype, subtype, params, header, body, extension)
}

// multipartStructure builds the parenthesized structure for a multipart entity:
// the nested part structures concatenated with no separator, then the subtype,
// then (for BODYSTRUCTURE) the extension fields (RFC 3501 §7.4.2).
func multipartStructure(subtype string, params map[string]string, body []byte, extension bool) string {
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
		// body-ext-mpart: parameters, disposition, language, location.
		b.WriteByte(' ')
		b.WriteString(paramList(params))
		b.WriteString(" NIL NIL NIL")
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
	if strings.EqualFold(maintype, "text") {
		// body-type-text adds a line count after the octet size.
		fmt.Fprintf(&b, " %d", countLines(body))
	}
	if extension {
		// body-ext-1part: MD5, disposition, language, location.
		b.WriteString(" NIL NIL NIL NIL")
	}
	b.WriteByte(')')
	return b.String()
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

// matchIMAPPattern matches a folder name against an IMAP LIST pattern.
// '*' matches any characters including hierarchy separator.
// '%' matches any characters except hierarchy separator '/'.
func matchIMAPPattern(pattern, name string) bool {
	// Simple cases
	if pattern == "*" {
		return true
	}
	if pattern == "%" {
		return !strings.Contains(name, "/")
	}

	// Case-insensitive match for INBOX
	pLower := strings.ToLower(pattern)
	nLower := strings.ToLower(name)

	return matchPatternRecursive(pLower, nLower)
}

func matchPatternRecursive(pattern, name string) bool {
	for len(pattern) > 0 {
		switch pattern[0] {
		case '*':
			// '*' matches everything — try matching rest of pattern at every position
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return true
			}
			for i := 0; i <= len(name); i++ {
				if matchPatternRecursive(pattern, name[i:]) {
					return true
				}
			}
			return false
		case '%':
			// '%' matches everything except '/'
			pattern = pattern[1:]
			if len(pattern) == 0 {
				return !strings.Contains(name, "/")
			}
			for i := 0; i <= len(name); i++ {
				if i > 0 && name[i-1] == '/' {
					break
				}
				if matchPatternRecursive(pattern, name[i:]) {
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
