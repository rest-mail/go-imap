package imap

import (
	"encoding/base64"
	"fmt"
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
	if msg.Flagged {
		flags = append(flags, `\Flagged`)
	}
	if msg.Draft {
		flags = append(flags, `\Draft`)
	}
	return strings.Join(flags, " ")
}

// buildEnvelope constructs an IMAP ENVELOPE response from a message.
func buildEnvelope(msg Message) string {
	date := msg.Date.Format("Mon, 02 Jan 2006 15:04:05 -0700")
	subject := quoteString(msg.Subject)

	// Simplified envelope: (date subject from sender reply-to to cc bcc in-reply-to message-id)
	// Each address is ((name NIL user host))
	fromAddr := buildAddress(msg.From.Name, msg.From.Email)

	return fmt.Sprintf("(%s %s %s %s %s NIL NIL NIL NIL NIL)",
		quoteString(date), subject, fromAddr, fromAddr, fromAddr)
}

// buildAddress constructs an IMAP address structure.
func buildAddress(name, email string) string {
	if email == "" {
		return "NIL"
	}
	parts := strings.SplitN(email, "@", 2)
	user := parts[0]
	host := ""
	if len(parts) > 1 {
		host = parts[1]
	}
	return fmt.Sprintf("((%s NIL %s %s))", quoteString(name), quoteString(user), quoteString(host))
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
	return tokens
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
