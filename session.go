package imap

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"
)

// Session represents a single IMAP conversation with a client.
type Session struct {
	conn      net.Conn
	reader    *bufio.Reader
	writer    *bufio.Writer
	backend   Backend
	hostname  string
	tlsConfig *tls.Config
	limiter   Limiter

	// Session state
	usingTLS bool
	auth     *authState
	mailbox  Mailbox
	selected *selectedMailbox
	messages []Message       // cached message list for current selection
	deleted  map[uint32]bool // UIDs flagged \Deleted in this session
}

type authState struct {
	authenticated bool
}

type selectedMailbox struct {
	name   string
	total  int64
	unread int64
}

// NewSession creates an IMAP session over conn, authenticating against backend.
// hostname is announced in the greeting. A nil limiter defaults to [NopLimiter].
// Call [Session.Handle] to run it.
func NewSession(conn net.Conn, backend Backend, hostname string, tlsConfig *tls.Config, limiter Limiter) *Session {
	if limiter == nil {
		limiter = NopLimiter{}
	}
	return &Session{
		conn:      conn,
		reader:    bufio.NewReader(conn),
		writer:    bufio.NewWriter(conn),
		backend:   backend,
		hostname:  hostname,
		tlsConfig: tlsConfig,
		limiter:   limiter,
		auth:      &authState{},
		deleted:   make(map[uint32]bool),
	}
}

// Handle runs the IMAP state machine until the client disconnects or LOGOUTs.
func (s *Session) Handle() {
	defer func() { _ = s.conn.Close() }()

	slog.Info("imap: new connection", "remote", s.conn.RemoteAddr())

	// Send greeting
	s.send("* OK [CAPABILITY IMAP4rev1 STARTTLS AUTH=PLAIN] %s IMAP4rev1 ready", s.hostname)

	for {
		_ = s.conn.SetDeadline(time.Now().Add(30 * time.Minute))

		line, err := s.reader.ReadString('\n')
		if err != nil {
			slog.Debug("imap: connection closed", "remote", s.conn.RemoteAddr(), "error", err)
			return
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			continue
		}

		slog.Debug("imap: recv", "remote", s.conn.RemoteAddr(), "cmd", line)

		// IMAP commands are: <tag> <command> [<args>]
		tag, cmd, args := parseIMAPCommand(line)
		if tag == "" {
			continue
		}

		switch strings.ToUpper(cmd) {
		case "CAPABILITY":
			s.handleCapability(tag)
		case "STARTTLS":
			if s.handleSTARTTLS(tag) {
				return
			}
		case "LOGIN":
			s.handleLogin(tag, args)
		case "AUTHENTICATE":
			s.handleAuthenticate(tag, args)
		case "LIST":
			s.handleList(tag, args)
		case "LSUB":
			s.handleList(tag, args) // treat LSUB same as LIST
		case "SELECT":
			s.handleSelect(tag, args)
		case "EXAMINE":
			s.handleSelect(tag, args) // read-only select
		case "STATUS":
			s.handleStatus(tag, args)
		case "FETCH":
			s.handleFetch(tag, args)
		case "SEARCH":
			s.handleSearch(tag, args)
		case "STORE":
			s.handleStore(tag, args)
		case "COPY":
			s.handleCopy(tag, args)
		case "MOVE":
			s.handleMove(tag, args)
		case "CREATE":
			s.handleCreate(tag, args)
		case "DELETE":
			s.handleDelete(tag, args)
		case "RENAME":
			s.handleRename(tag, args)
		case "APPEND":
			s.handleAppend(tag, args)
		case "NOOP":
			s.tagged(tag, "OK", "NOOP completed")
		case "CHECK":
			s.tagged(tag, "OK", "CHECK completed")
		case "CLOSE":
			// Implicitly expunge \Deleted messages (RFC 3501 §6.4.2)
			// Unlike EXPUNGE, CLOSE does not send untagged EXPUNGE responses
			if s.selected != nil {
				for _, msg := range s.messages {
					if s.deleted[msg.UID] {
						_ = s.mailbox.Delete(msg.UID)
					}
				}
			}
			s.selected = nil
			s.messages = nil
			s.deleted = make(map[uint32]bool)
			s.tagged(tag, "OK", "CLOSE completed")
		case "EXPUNGE":
			s.handleExpunge(tag)
		case "GETQUOTA":
			s.handleGetQuota(tag, args)
		case "GETQUOTAROOT":
			s.handleGetQuotaRoot(tag, args)
		case "IDLE":
			s.handleIdle(tag)
		case "UID":
			s.handleUID(tag, args)
		case "LOGOUT":
			s.send("* BYE IMAP4rev1 Server logging out")
			s.tagged(tag, "OK", "LOGOUT completed")
			return
		default:
			s.tagged(tag, "BAD", "Unknown command")
		}
	}
}

func (s *Session) handleCapability(tag string) {
	caps := "IMAP4rev1 UIDPLUS IDLE MOVE QUOTA"
	if !s.usingTLS && s.tlsConfig != nil {
		caps += " STARTTLS"
	}
	if s.usingTLS || s.tlsConfig == nil {
		caps += " AUTH=PLAIN"
	}
	s.send("* CAPABILITY %s", caps)
	s.tagged(tag, "OK", "CAPABILITY completed")
}

func (s *Session) handleSTARTTLS(tag string) bool {
	if s.usingTLS {
		s.tagged(tag, "BAD", "Already in TLS mode")
		return false
	}
	if s.tlsConfig == nil {
		s.tagged(tag, "BAD", "TLS not available")
		return false
	}

	s.tagged(tag, "OK", "Begin TLS negotiation now")

	tlsConn := tls.Server(s.conn, s.tlsConfig)
	if err := tlsConn.Handshake(); err != nil {
		slog.Warn("imap: TLS handshake failed", "error", err)
		return true
	}

	s.conn = tlsConn
	s.reader = bufio.NewReader(tlsConn)
	s.writer = bufio.NewWriter(tlsConn)
	s.usingTLS = true

	slog.Info("imap: TLS established", "remote", s.conn.RemoteAddr())
	return false
}

func (s *Session) handleLogin(tag, args string) {
	if !s.usingTLS && s.tlsConfig != nil {
		s.tagged(tag, "NO", "[PRIVACYREQUIRED] STARTTLS required")
		return
	}

	// Parse: LOGIN <user> <password>
	parts := parseIMAPArgs(args)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "LOGIN requires username and password")
		return
	}
	username := unquote(parts[0])
	password := unquote(parts[1])

	s.authenticate(tag, username, password, "[CAPABILITY IMAP4rev1] LOGIN completed")
}

func (s *Session) handleAuthenticate(tag, args string) {
	// Simplified — only support PLAIN
	if !strings.EqualFold(strings.TrimSpace(args), "PLAIN") {
		s.tagged(tag, "NO", "Unsupported mechanism")
		return
	}
	s.send("+")

	line, err := s.reader.ReadString('\n')
	if err != nil {
		return
	}
	decoded, err := decodeBase64(strings.TrimRight(line, "\r\n"))
	if err != nil {
		s.tagged(tag, "NO", "Invalid base64")
		return
	}
	parts := strings.SplitN(string(decoded), "\x00", 3)
	if len(parts) != 3 {
		s.tagged(tag, "NO", "Invalid PLAIN data")
		return
	}

	s.authenticate(tag, parts[1], parts[2], "AUTHENTICATE completed")
}

// authenticate runs the shared LOGIN/AUTHENTICATE credential check, wiring the
// resulting Mailbox and applying limiter accounting. okMsg is the tagged OK text
// sent on success.
func (s *Session) authenticate(tag, username, password, okMsg string) {
	ip := extractIP(s.conn.RemoteAddr().String())

	mailbox, err := s.backend.Authenticate(username, password)
	if err != nil {
		slog.Warn("imap: auth failed",
			"remote", s.conn.RemoteAddr(),
			"user", username,
			"event", "imap_auth_failed",
			"ip", ip,
		)
		s.limiter.RecordAuthFail(ip)
		if s.limiter.IsBanned(ip) {
			s.tagged(tag, "NO", "Too many authentication failures")
			_ = s.conn.Close()
			return
		}
		s.tagged(tag, "NO", "[AUTHENTICATIONFAILED] Invalid credentials")
		return
	}

	s.limiter.ResetAuth(ip)
	s.auth.authenticated = true
	s.mailbox = mailbox

	slog.Info("imap: authenticated", "remote", s.conn.RemoteAddr(), "user", username)
	s.tagged(tag, "OK", okMsg)
}

func (s *Session) handleList(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	// Parse reference and mailbox pattern
	parts := parseIMAPArgs(args)
	pattern := "*"
	if len(parts) >= 2 {
		pattern = unquote(parts[1])
	} else if len(parts) == 1 {
		pattern = unquote(parts[0])
	}

	// Empty pattern = return hierarchy delimiter only
	if pattern == "" {
		s.send(`* LIST (\Noselect) "/" ""`)
		s.tagged(tag, "OK", "LIST completed")
		return
	}

	folders, err := s.mailbox.Folders()
	if err != nil {
		s.tagged(tag, "NO", "Failed to list folders")
		return
	}

	for _, f := range folders {
		if !matchIMAPPattern(pattern, f.Name) {
			continue
		}
		attrs := ""
		switch f.Name {
		case "INBOX":
			// no special attributes
		case "Sent":
			attrs = `\Sent`
		case "Drafts":
			attrs = `\Drafts`
		case "Trash":
			attrs = `\Trash`
		case "Junk":
			attrs = `\Junk`
		}
		s.send(`* LIST (%s) "/" "%s"`, attrs, f.Name)
	}
	s.tagged(tag, "OK", "LIST completed")
}

func (s *Session) handleSelect(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	folder := unquote(strings.TrimSpace(args))
	if folder == "" {
		folder = "INBOX"
	}

	// Fetch messages for this folder
	messages, err := s.mailbox.Messages(folder)
	if err != nil {
		s.tagged(tag, "NO", "Failed to select folder")
		return
	}

	s.messages = messages
	total := int64(len(s.messages))
	var unread int64
	for _, m := range s.messages {
		if !m.Seen {
			unread++
		}
	}

	s.selected = &selectedMailbox{
		name:   folder,
		total:  total,
		unread: unread,
	}

	s.send("* %d EXISTS", total)
	s.send("* %d RECENT", unread)
	s.send("* OK [UNSEEN %d]", unread)
	s.send("* OK [UIDVALIDITY 1]")
	if total > 0 {
		s.send("* OK [UIDNEXT %d]", s.messages[0].UID+1)
	} else {
		s.send("* OK [UIDNEXT 1]")
	}
	s.send("* FLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft)")
	s.send("* OK [PERMANENTFLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft \\*)]")

	s.tagged(tag, "OK", "[READ-WRITE] SELECT completed")
}

func (s *Session) handleStatus(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	// Parse: STATUS <mailbox> (MESSAGES UNSEEN RECENT)
	parts := parseIMAPArgs(args)
	if len(parts) < 1 {
		s.tagged(tag, "BAD", "STATUS requires mailbox name")
		return
	}
	folder := unquote(parts[0])

	messages, err := s.mailbox.Messages(folder)
	if err != nil {
		s.tagged(tag, "NO", "Failed to get status")
		return
	}

	total := len(messages)
	var unseen int
	for _, m := range messages {
		if !m.Seen {
			unseen++
		}
	}

	s.send(`* STATUS "%s" (MESSAGES %d RECENT %d UNSEEN %d)`, folder, total, unseen, unseen)
	s.tagged(tag, "OK", "STATUS completed")
}

func (s *Session) handleFetch(tag, args string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	// Parse sequence set and data items
	// Simplified: handle "FETCH <n> (FLAGS)" and "FETCH <n> (BODY[])" etc.
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "FETCH requires sequence and data items")
		return
	}

	seqStr := parts[0]
	dataItems := strings.ToUpper(parts[1])

	// Parse sequence numbers
	seqNums := parseSequenceSet(seqStr, len(s.messages))

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]

		if strings.Contains(dataItems, "BODY[]") || strings.Contains(dataItems, "BODY.PEEK[]") || strings.Contains(dataItems, "RFC822") {
			// Full message fetch.
			rawBytes, err := s.mailbox.Fetch(msg.UID)
			if err != nil {
				continue
			}
			raw := string(rawBytes)
			flags := buildFlags(msg)

			s.send("* %d FETCH (FLAGS (%s) RFC822.SIZE %d BODY[] {%d}", seq, flags, len(raw), len(raw))
			s.writeString(raw + ")\r\n")

			// Auto-mark as read — but BODY.PEEK[] must NOT set \Seen (RFC 3501).
			if !msg.Seen && !strings.Contains(dataItems, "BODY.PEEK") {
				_ = s.mailbox.Store(msg.UID, FlagUpdate{Seen: boolPtr(true)})
				s.messages[seq-1].Seen = true
			}
		} else if strings.Contains(dataItems, "BODY[HEADER]") {
			rawBytes, err := s.mailbox.Fetch(msg.UID)
			if err != nil {
				continue
			}
			raw := string(rawBytes)
			headerEnd := strings.Index(raw, "\r\n\r\n")
			headers := raw
			if headerEnd >= 0 {
				headers = raw[:headerEnd+4]
			}
			flags := buildFlags(msg)
			s.send("* %d FETCH (FLAGS (%s) BODY[HEADER] {%d}", seq, flags, len(headers))
			s.writeString(headers + ")\r\n")
		} else if strings.Contains(dataItems, "BODY[TEXT]") {
			rawBytes, err := s.mailbox.Fetch(msg.UID)
			if err != nil {
				continue
			}
			raw := string(rawBytes)
			headerEnd := strings.Index(raw, "\r\n\r\n")
			body := ""
			if headerEnd >= 0 && headerEnd+4 < len(raw) {
				body = raw[headerEnd+4:]
			}
			flags := buildFlags(msg)
			s.send("* %d FETCH (FLAGS (%s) BODY[TEXT] {%d}", seq, flags, len(body))
			s.writeString(body + ")\r\n")
		} else if strings.Contains(dataItems, "BODY[HEADER.FIELDS") || strings.Contains(dataItems, "BODY.PEEK[HEADER.FIELDS") {
			rawBytes, err := s.mailbox.Fetch(msg.UID)
			if err != nil {
				continue
			}
			raw := string(rawBytes)
			// Extract requested header fields
			requested := extractHeaderFieldNames(dataItems)
			headers := filterHeaders(raw, requested)
			flags := buildFlags(msg)
			fetchItem := "BODY[HEADER.FIELDS (" + strings.Join(requested, " ") + ")]"
			if strings.Contains(dataItems, "BODY.PEEK") {
				fetchItem = "BODY.PEEK[HEADER.FIELDS (" + strings.Join(requested, " ") + ")]"
			}
			s.send("* %d FETCH (FLAGS (%s) %s {%d}", seq, flags, fetchItem, len(headers))
			s.writeString(headers + ")\r\n")
		} else if strings.Contains(dataItems, "FLAGS") || strings.Contains(dataItems, "ENVELOPE") || strings.Contains(dataItems, "INTERNALDATE") {
			flags := buildFlags(msg)
			date := msg.Date.Format("02-Jan-2006 15:04:05 -0700")
			envelope := buildEnvelope(msg)
			s.send("* %d FETCH (FLAGS (%s) INTERNALDATE \"%s\" RFC822.SIZE %d ENVELOPE %s UID %d)",
				seq, flags, date, msg.Size, envelope, msg.UID)
		} else {
			flags := buildFlags(msg)
			s.send("* %d FETCH (FLAGS (%s) UID %d)", seq, flags, msg.UID)
		}
	}

	s.tagged(tag, "OK", "FETCH completed")
}

// ── SEARCH ────────────────────────────────────────────────────────────

type searchCriterion struct {
	kind string // "all", "seen", "unseen", "flagged", "unflagged", "deleted", "undeleted",
	// "from", "to", "subject", "since", "before", "on", "uid", "not", "or"
	value string            // for string/date/uid criteria
	date  time.Time         // parsed date for since/before/on
	sub   []searchCriterion // for NOT (1 element) or OR (2 elements)
}

func (s *Session) handleSearch(tag, args string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	criteria := s.parseSearchCriteria(strings.TrimSpace(args))

	var seqNums []string
	for i, msg := range s.messages {
		if s.matchesCriteria(msg, criteria) {
			seqNums = append(seqNums, strconv.Itoa(i+1))
		}
	}

	if len(seqNums) > 0 {
		s.send("* SEARCH %s", strings.Join(seqNums, " "))
	} else {
		s.send("* SEARCH")
	}
	s.tagged(tag, "OK", "SEARCH completed")
}

// parseSearchCriteria tokenizes the IMAP SEARCH arguments and builds criteria.
func (s *Session) parseSearchCriteria(args string) []searchCriterion {
	tokens := tokenizeSearch(args)
	var criteria []searchCriterion
	idx := 0
	for idx < len(tokens) {
		c, newIdx := parseSingleCriterion(tokens, idx)
		criteria = append(criteria, c)
		idx = newIdx
	}
	return criteria
}

// tokenizeSearch splits the search arguments into tokens, respecting quoted strings.
func tokenizeSearch(args string) []string {
	var tokens []string
	i := 0
	for i < len(args) {
		// Skip whitespace
		for i < len(args) && args[i] == ' ' {
			i++
		}
		if i >= len(args) {
			break
		}
		if args[i] == '"' {
			// Quoted string — find closing quote
			j := i + 1
			for j < len(args) && args[j] != '"' {
				j++
			}
			if j < len(args) {
				j++ // include closing quote
			}
			tokens = append(tokens, args[i:j])
			i = j
		} else {
			// Unquoted token
			j := i
			for j < len(args) && args[j] != ' ' {
				j++
			}
			tokens = append(tokens, args[i:j])
			i = j
		}
	}
	return tokens
}

// parseSingleCriterion parses one criterion from the token list starting at idx.
func parseSingleCriterion(tokens []string, idx int) (searchCriterion, int) {
	if idx >= len(tokens) {
		return searchCriterion{kind: "all"}, idx + 1
	}

	keyword := strings.ToUpper(tokens[idx])

	switch keyword {
	case "ALL":
		return searchCriterion{kind: "all"}, idx + 1
	case "SEEN":
		return searchCriterion{kind: "seen"}, idx + 1
	case "UNSEEN":
		return searchCriterion{kind: "unseen"}, idx + 1
	case "FLAGGED":
		return searchCriterion{kind: "flagged"}, idx + 1
	case "UNFLAGGED":
		return searchCriterion{kind: "unflagged"}, idx + 1
	case "DELETED":
		return searchCriterion{kind: "deleted"}, idx + 1
	case "UNDELETED":
		return searchCriterion{kind: "undeleted"}, idx + 1
	case "FROM":
		if idx+1 < len(tokens) {
			return searchCriterion{kind: "from", value: unquote(tokens[idx+1])}, idx + 2
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "TO":
		if idx+1 < len(tokens) {
			return searchCriterion{kind: "to", value: unquote(tokens[idx+1])}, idx + 2
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "SUBJECT":
		if idx+1 < len(tokens) {
			return searchCriterion{kind: "subject", value: unquote(tokens[idx+1])}, idx + 2
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "SINCE":
		if idx+1 < len(tokens) {
			d := parseSearchDate(unquote(tokens[idx+1]))
			return searchCriterion{kind: "since", value: tokens[idx+1], date: d}, idx + 2
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "BEFORE":
		if idx+1 < len(tokens) {
			d := parseSearchDate(unquote(tokens[idx+1]))
			return searchCriterion{kind: "before", value: tokens[idx+1], date: d}, idx + 2
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "ON":
		if idx+1 < len(tokens) {
			d := parseSearchDate(unquote(tokens[idx+1]))
			return searchCriterion{kind: "on", value: tokens[idx+1], date: d}, idx + 2
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "UID":
		if idx+1 < len(tokens) {
			return searchCriterion{kind: "uid", value: tokens[idx+1]}, idx + 2
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "NOT":
		if idx+1 < len(tokens) {
			sub, newIdx := parseSingleCriterion(tokens, idx+1)
			return searchCriterion{kind: "not", sub: []searchCriterion{sub}}, newIdx
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "OR":
		if idx+2 < len(tokens) {
			sub1, newIdx1 := parseSingleCriterion(tokens, idx+1)
			sub2, newIdx2 := parseSingleCriterion(tokens, newIdx1)
			return searchCriterion{kind: "or", sub: []searchCriterion{sub1, sub2}}, newIdx2
		}
		return searchCriterion{kind: "all"}, idx + 1
	default:
		// Unknown token — treat as ALL (ignore)
		return searchCriterion{kind: "all"}, idx + 1
	}
}

// parseSearchDate parses IMAP date formats: "1-Jan-2006" or "01-Jan-2006".
func parseSearchDate(s string) time.Time {
	s = strings.TrimSpace(s)
	// Try both single-digit and double-digit day formats
	for _, layout := range []string{"2-Jan-2006", "02-Jan-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (s *Session) matchesCriteria(msg Message, criteria []searchCriterion) bool {
	for _, c := range criteria {
		if !s.matchOne(msg, c) {
			return false
		}
	}
	return true
}

func (s *Session) matchOne(msg Message, c searchCriterion) bool {
	switch c.kind {
	case "all":
		return true
	case "seen":
		return msg.Seen
	case "unseen":
		return !msg.Seen
	case "flagged":
		return msg.Flagged
	case "unflagged":
		return !msg.Flagged
	case "deleted":
		return s.deleted[msg.UID]
	case "undeleted":
		return !s.deleted[msg.UID]
	case "from":
		return strings.Contains(strings.ToLower(msg.From.Email), strings.ToLower(c.value))
	case "to":
		return strings.Contains(strings.ToLower(msg.To), strings.ToLower(c.value))
	case "subject":
		return strings.Contains(strings.ToLower(msg.Subject), strings.ToLower(c.value))
	case "since":
		return !msg.Date.Before(c.date)
	case "before":
		return msg.Date.Before(c.date)
	case "on":
		y1, m1, d1 := msg.Date.Date()
		y2, m2, d2 := c.date.Date()
		return y1 == y2 && m1 == m2 && d1 == d2
	case "uid":
		// Parse UID set and check if msg.UID is in it
		// We need a max UID — use a large number since UIDs are store IDs
		uidSet := parseSequenceSet(c.value, int(msg.UID)+1000000)
		for _, uid := range uidSet {
			if uint32(uid) == msg.UID {
				return true
			}
		}
		return false
	case "not":
		if len(c.sub) > 0 {
			return !s.matchOne(msg, c.sub[0])
		}
		return true
	case "or":
		if len(c.sub) >= 2 {
			return s.matchOne(msg, c.sub[0]) || s.matchOne(msg, c.sub[1])
		}
		return true
	default:
		return true // unknown criteria: don't filter
	}
}

func (s *Session) handleStore(tag, args string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	// Parse: STORE <seq> +FLAGS (\Seen) or -FLAGS (\Seen)
	parts := strings.SplitN(args, " ", 3)
	if len(parts) < 3 {
		s.tagged(tag, "BAD", "STORE requires sequence, action, and flags")
		return
	}

	seqStr := parts[0]
	action := strings.ToUpper(parts[1])
	flagStr := parts[2]

	seqNums := parseSequenceSet(seqStr, len(s.messages))
	flags := parseFlags(flagStr)

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := &s.messages[seq-1]

		var update FlagUpdate
		changed := false

		for _, flag := range flags {
			switch flag {
			case `\Seen`:
				val := strings.HasPrefix(action, "+")
				update.Seen = boolPtr(val)
				msg.Seen = val
				changed = true
			case `\Flagged`:
				val := strings.HasPrefix(action, "+")
				update.Flagged = boolPtr(val)
				msg.Flagged = val
				changed = true
			case `\Deleted`:
				if strings.HasPrefix(action, "+") {
					if s.deleted == nil {
						s.deleted = make(map[uint32]bool)
					}
					s.deleted[msg.UID] = true
				} else {
					delete(s.deleted, msg.UID)
				}
			}
		}

		if changed {
			_ = s.mailbox.Store(msg.UID, update)
		}

		newFlags := buildFlags(*msg)
		s.send("* %d FETCH (FLAGS (%s))", seq, newFlags)
	}

	s.tagged(tag, "OK", "STORE completed")
}

func (s *Session) handleCopy(tag, args string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "COPY requires sequence and destination")
		return
	}

	seqStr := parts[0]
	dest := unquote(strings.TrimSpace(parts[1]))

	seqNums := parseSequenceSet(seqStr, len(s.messages))

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]
		if err := s.mailbox.Copy(msg.UID, dest); err != nil {
			slog.Warn("imap: copy failed", "uid", msg.UID, "error", err)
			continue
		}
	}

	s.tagged(tag, "OK", "COPY completed")
}

func (s *Session) handleMove(tag, args string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "MOVE requires sequence and destination")
		return
	}

	seqStr := parts[0]
	dest := unquote(strings.TrimSpace(parts[1]))

	seqNums := parseSequenceSet(seqStr, len(s.messages))

	// Move = update folder + expunge from current view (in reverse for stable seq nums)
	for i := len(seqNums) - 1; i >= 0; i-- {
		seq := seqNums[i]
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]
		if err := s.mailbox.Move(msg.UID, dest); err != nil {
			slog.Warn("imap: move failed", "uid", msg.UID, "error", err)
			continue
		}
		// Send EXPUNGE for this sequence number
		s.send("* %d EXPUNGE", seq)
		// Remove from local message list
		s.messages = append(s.messages[:seq-1], s.messages[seq:]...)
	}

	if s.selected != nil {
		s.selected.total = int64(len(s.messages))
	}

	s.tagged(tag, "OK", "MOVE completed")
}

func (s *Session) handleExpunge(tag string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	// Process in reverse order so sequence numbers stay valid
	for i := len(s.messages) - 1; i >= 0; i-- {
		msg := s.messages[i]
		if !s.deleted[msg.UID] {
			continue
		}
		seq := i + 1
		// Delete via backend
		if err := s.mailbox.Delete(msg.UID); err != nil {
			slog.Warn("imap: expunge failed", "uid", msg.UID, "error", err)
			continue
		}
		// Send untagged EXPUNGE response
		s.send("* %d EXPUNGE", seq)
		// Remove from messages slice
		s.messages = append(s.messages[:i], s.messages[i+1:]...)
	}

	// Update selected mailbox count
	if s.selected != nil {
		s.selected.total = int64(len(s.messages))
	}

	s.deleted = make(map[uint32]bool)
	s.tagged(tag, "OK", "EXPUNGE completed")
}

func (s *Session) handleCreate(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	folder := unquote(strings.TrimSpace(args))
	if folder == "" {
		s.tagged(tag, "NO", "Missing folder name")
		return
	}
	// Reject folder names that are too long or contain path separators
	if len(folder) > 200 {
		s.tagged(tag, "NO", "Folder name too long")
		return
	}
	if strings.ContainsAny(folder, "\x00\r\n") {
		s.tagged(tag, "NO", "Invalid folder name")
		return
	}

	// Folders are implicit — they exist once a message is moved into them.
	// CREATE just validates and acknowledges.
	s.tagged(tag, "OK", "CREATE completed")
}

func (s *Session) handleDelete(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	folder := unquote(strings.TrimSpace(args))
	if folder == "" {
		s.tagged(tag, "NO", "Missing folder name")
		return
	}

	// Prevent deleting standard folders
	standard := map[string]bool{"INBOX": true, "Sent": true, "Drafts": true, "Trash": true}
	if standard[folder] {
		s.tagged(tag, "NO", "Cannot delete standard folder")
		return
	}

	// Move all messages in the folder to Trash.
	if messages, err := s.mailbox.Messages(folder); err == nil {
		for _, msg := range messages {
			_ = s.mailbox.Move(msg.UID, "Trash")
		}
	}

	s.tagged(tag, "OK", "DELETE completed")
}

func (s *Session) handleRename(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "RENAME requires old and new name")
		return
	}

	oldName := unquote(strings.TrimSpace(parts[0]))
	newName := unquote(strings.TrimSpace(parts[1]))

	if oldName == "" || newName == "" {
		s.tagged(tag, "BAD", "RENAME requires old and new name")
		return
	}

	standard := map[string]bool{"INBOX": true, "Sent": true, "Drafts": true, "Trash": true}
	if standard[oldName] {
		s.tagged(tag, "NO", "Cannot rename standard folder")
		return
	}

	// Move all messages from old folder to new folder.
	if messages, err := s.mailbox.Messages(oldName); err == nil {
		for _, msg := range messages {
			_ = s.mailbox.Move(msg.UID, newName)
		}
	}

	s.tagged(tag, "OK", "RENAME completed")
}

func (s *Session) handleAppend(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	// Parse: APPEND "folder" (\flags) {size}
	// Minimal parse — extract folder and literal size
	folder := "INBOX"
	if idx := strings.Index(args, "\""); idx >= 0 {
		end := strings.Index(args[idx+1:], "\"")
		if end >= 0 {
			folder = args[idx+1 : idx+1+end]
		}
	}

	// Parse optional flags: (\Seen \Draft) between folder and {size}
	var appendFlags []string
	if flagStart := strings.Index(args, "("); flagStart >= 0 {
		if flagEnd := strings.Index(args[flagStart:], ")"); flagEnd >= 0 {
			flagStr := args[flagStart+1 : flagStart+flagEnd]
			appendFlags = append(appendFlags, strings.Fields(flagStr)...)
		}
	}

	// Find literal size {N}
	braceStart := strings.LastIndex(args, "{")
	braceEnd := strings.LastIndex(args, "}")
	if braceStart < 0 || braceEnd <= braceStart {
		s.tagged(tag, "BAD", "Missing literal size")
		return
	}
	sizeStr := args[braceStart+1 : braceEnd]
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size < 0 {
		s.tagged(tag, "BAD", "Invalid literal size")
		return
	}
	if size > 10*1024*1024 {
		s.tagged(tag, "NO", "Message too large")
		return
	}

	// Send continuation
	s.send("+ Ready for literal data")

	// Read exactly size bytes
	data := make([]byte, size)
	_, err = io.ReadFull(s.reader, data)
	if err != nil {
		s.tagged(tag, "NO", "Failed to read message data")
		return
	}

	// Read trailing CRLF
	_, _ = s.reader.ReadString('\n')

	// Translate the parsed flags into a neutral FlagUpdate.
	var flags FlagUpdate
	for _, flag := range appendFlags {
		switch flag {
		case `\Seen`:
			flags.Seen = boolPtr(true)
		case `\Flagged`:
			flags.Flagged = boolPtr(true)
		case `\Draft`:
			flags.Draft = boolPtr(true)
		}
	}

	if err := s.mailbox.Append(folder, flags, data); err != nil {
		slog.Warn("imap: append failed", "error", err)
		s.tagged(tag, "NO", "APPEND failed")
		return
	}

	s.tagged(tag, "OK", "APPEND completed")
}

// handleGetQuota returns quota for a named quota root (RFC 2087).
func (s *Session) handleGetQuota(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	used, limit, err := s.mailbox.Quota()
	if err != nil {
		s.tagged(tag, "NO", "Failed to get quota")
		return
	}

	// Report in KB (IMAP QUOTA uses 1024-byte units)
	s.send("* QUOTA \"\" (STORAGE %d %d)", used/1024, limit/1024)
	s.tagged(tag, "OK", "GETQUOTA completed")
}

// handleGetQuotaRoot returns the quota root for a mailbox (RFC 2087).
func (s *Session) handleGetQuotaRoot(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	mailbox := strings.Trim(args, "\" ")
	if mailbox == "" {
		s.tagged(tag, "BAD", "Missing mailbox name")
		return
	}

	used, limit, err := s.mailbox.Quota()
	if err != nil {
		s.tagged(tag, "NO", "Failed to get quota")
		return
	}

	s.send("* QUOTAROOT %s \"\"", mailbox)
	s.send("* QUOTA \"\" (STORAGE %d %d)", used/1024, limit/1024)
	s.tagged(tag, "OK", "GETQUOTAROOT completed")
}

func (s *Session) handleIdle(tag string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	s.send("+ idling")

	// Start polling goroutine for new messages. It owns s.writer and s.messages
	// for the duration of IDLE; the main goroutine only reads DONE and must not
	// touch either until the poll goroutine has fully stopped. `stopped` closes
	// when the goroutine returns — including after any in-flight tick — so the
	// main goroutine can wait for it before writing the tagged response.
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				messages, err := s.mailbox.Messages(s.selected.name)
				if err != nil {
					continue
				}
				newTotal := int64(len(messages))
				if newTotal > s.selected.total {
					s.send("* %d EXISTS", newTotal)
					s.selected.total = newTotal
					s.messages = messages
				}
			}
		}
	}()

	// Wait for DONE from client.
	_ = s.conn.SetDeadline(time.Now().Add(29 * time.Minute))
	for {
		line, err := s.reader.ReadString('\n')
		if err != nil {
			close(done)
			<-stopped // let any in-flight poll finish before we return
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.ToUpper(line) == "DONE" {
			break
		}
	}

	close(done)
	<-stopped // ensure the poll goroutine stopped before we write again
	s.tagged(tag, "OK", "IDLE terminated")
}

// ── UID command ───────────────────────────────────────────────────────

func (s *Session) handleUID(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	// Parse "UID FETCH 1:* (FLAGS)" → subCmd="FETCH", subArgs="1:* (FLAGS)"
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 1 {
		s.tagged(tag, "BAD", "UID requires a command")
		return
	}
	subCmd := strings.ToUpper(parts[0])
	subArgs := ""
	if len(parts) > 1 {
		subArgs = parts[1]
	}

	// Convert UID sequence set to message sequence numbers
	switch subCmd {
	case "FETCH":
		s.handleUIDFetch(tag, subArgs)
	case "STORE":
		s.handleUIDStore(tag, subArgs)
	case "COPY":
		s.handleUIDCopy(tag, subArgs)
	case "MOVE":
		s.handleUIDMove(tag, subArgs)
	case "SEARCH":
		s.handleUIDSearch(tag, subArgs)
	default:
		s.tagged(tag, "BAD", "Unknown UID command")
	}
}

// uidToSeq converts a UID to a sequence number (1-based) in the current message list.
func (s *Session) uidToSeq(uid uint32) int {
	for i, msg := range s.messages {
		if msg.UID == uid {
			return i + 1
		}
	}
	return 0
}

// parseUIDSet parses a UID set like "1,3:5,*" and returns matching sequence numbers.
func (s *Session) parseUIDSet(uidSetStr string) []int {
	var seqNums []int
	for _, part := range strings.Split(uidSetStr, ",") {
		part = strings.TrimSpace(part)
		if strings.Contains(part, ":") {
			rangeParts := strings.SplitN(part, ":", 2)
			var startUID, endUID uint32
			if rangeParts[0] == "*" {
				if len(s.messages) > 0 {
					startUID = s.messages[len(s.messages)-1].UID
				}
			} else {
				v, _ := strconv.ParseUint(rangeParts[0], 10, 32)
				startUID = uint32(v)
			}
			if rangeParts[1] == "*" {
				if len(s.messages) > 0 {
					endUID = s.messages[len(s.messages)-1].UID
				}
			} else {
				v, _ := strconv.ParseUint(rangeParts[1], 10, 32)
				endUID = uint32(v)
			}
			if startUID > endUID {
				startUID, endUID = endUID, startUID
			}
			for i, msg := range s.messages {
				if msg.UID >= startUID && msg.UID <= endUID {
					seqNums = append(seqNums, i+1)
				}
			}
		} else if part == "*" {
			if len(s.messages) > 0 {
				seqNums = append(seqNums, len(s.messages))
			}
		} else {
			uid, _ := strconv.ParseUint(part, 10, 32)
			seq := s.uidToSeq(uint32(uid))
			if seq > 0 {
				seqNums = append(seqNums, seq)
			}
		}
	}
	return seqNums
}

func (s *Session) handleUIDFetch(tag, args string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "UID FETCH requires uid set and data items")
		return
	}

	uidSetStr := parts[0]
	dataItems := strings.ToUpper(parts[1])

	seqNums := s.parseUIDSet(uidSetStr)

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]

		if strings.Contains(dataItems, "BODY[]") || strings.Contains(dataItems, "BODY.PEEK[]") || strings.Contains(dataItems, "RFC822") {
			rawBytes, err := s.mailbox.Fetch(msg.UID)
			if err != nil {
				continue
			}
			raw := string(rawBytes)
			flags := buildFlags(msg)
			s.send("* %d FETCH (UID %d FLAGS (%s) RFC822.SIZE %d BODY[] {%d}", seq, msg.UID, flags, len(raw), len(raw))
			s.writeString(raw + ")\r\n")

			if !msg.Seen && !strings.Contains(dataItems, "BODY.PEEK") {
				_ = s.mailbox.Store(msg.UID, FlagUpdate{Seen: boolPtr(true)})
				s.messages[seq-1].Seen = true
			}
		} else if strings.Contains(dataItems, "BODY[HEADER]") {
			rawBytes, err := s.mailbox.Fetch(msg.UID)
			if err != nil {
				continue
			}
			raw := string(rawBytes)
			// Extract headers only (up to first blank line)
			headerEnd := strings.Index(raw, "\r\n\r\n")
			headers := raw
			if headerEnd >= 0 {
				headers = raw[:headerEnd+4] // include trailing CRLF CRLF
			}
			flags := buildFlags(msg)
			s.send("* %d FETCH (UID %d FLAGS (%s) BODY[HEADER] {%d}", seq, msg.UID, flags, len(headers))
			s.writeString(headers + ")\r\n")
		} else if strings.Contains(dataItems, "BODY[TEXT]") {
			rawBytes, err := s.mailbox.Fetch(msg.UID)
			if err != nil {
				continue
			}
			raw := string(rawBytes)
			headerEnd := strings.Index(raw, "\r\n\r\n")
			body := ""
			if headerEnd >= 0 && headerEnd+4 < len(raw) {
				body = raw[headerEnd+4:]
			}
			flags := buildFlags(msg)
			s.send("* %d FETCH (UID %d FLAGS (%s) BODY[TEXT] {%d}", seq, msg.UID, flags, len(body))
			s.writeString(body + ")\r\n")
		} else if strings.Contains(dataItems, "BODY[HEADER.FIELDS") || strings.Contains(dataItems, "BODY.PEEK[HEADER.FIELDS") {
			rawBytes, err := s.mailbox.Fetch(msg.UID)
			if err != nil {
				continue
			}
			raw := string(rawBytes)
			requested := extractHeaderFieldNames(dataItems)
			headers := filterHeaders(raw, requested)
			flags := buildFlags(msg)
			fetchItem := "BODY[HEADER.FIELDS (" + strings.Join(requested, " ") + ")]"
			if strings.Contains(dataItems, "BODY.PEEK") {
				fetchItem = "BODY.PEEK[HEADER.FIELDS (" + strings.Join(requested, " ") + ")]"
			}
			s.send("* %d FETCH (UID %d FLAGS (%s) %s {%d}", seq, msg.UID, flags, fetchItem, len(headers))
			s.writeString(headers + ")\r\n")
		} else if strings.Contains(dataItems, "FLAGS") || strings.Contains(dataItems, "ENVELOPE") || strings.Contains(dataItems, "INTERNALDATE") {
			flags := buildFlags(msg)
			date := msg.Date.Format("02-Jan-2006 15:04:05 -0700")
			envelope := buildEnvelope(msg)
			s.send("* %d FETCH (UID %d FLAGS (%s) INTERNALDATE \"%s\" RFC822.SIZE %d ENVELOPE %s)",
				seq, msg.UID, flags, date, msg.Size, envelope)
		} else {
			flags := buildFlags(msg)
			s.send("* %d FETCH (UID %d FLAGS (%s))", seq, msg.UID, flags)
		}
	}

	s.tagged(tag, "OK", "UID FETCH completed")
}

func (s *Session) handleUIDStore(tag, args string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "UID STORE requires uid set and flags")
		return
	}

	uidSetStr := parts[0]
	flagArgs := parts[1]

	seqNums := s.parseUIDSet(uidSetStr)

	// Rewrite args to use sequence numbers and delegate to handleStore
	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]

		// Parse flags action
		flagParts := strings.SplitN(flagArgs, " ", 2)
		if len(flagParts) < 2 {
			continue
		}
		action := flagParts[0]
		flagStr := strings.Trim(flagParts[1], "()")
		flags := strings.Fields(flagStr)

		var update FlagUpdate
		changed := false
		for _, flag := range flags {
			switch flag {
			case `\Seen`:
				val := strings.HasPrefix(action, "+")
				update.Seen = boolPtr(val)
				s.messages[seq-1].Seen = val
				changed = true
			case `\Flagged`:
				val := strings.HasPrefix(action, "+")
				update.Flagged = boolPtr(val)
				s.messages[seq-1].Flagged = val
				changed = true
			case `\Deleted`:
				if strings.HasPrefix(action, "+") {
					if s.deleted == nil {
						s.deleted = make(map[uint32]bool)
					}
					s.deleted[msg.UID] = true
				} else {
					delete(s.deleted, msg.UID)
				}
			}
		}

		if changed {
			_ = s.mailbox.Store(msg.UID, update)
		}

		newFlags := buildFlags(s.messages[seq-1])
		s.send("* %d FETCH (UID %d FLAGS (%s))", seq, msg.UID, newFlags)
	}

	s.tagged(tag, "OK", "UID STORE completed")
}

func (s *Session) handleUIDCopy(tag, args string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "UID COPY requires uid set and destination")
		return
	}

	uidSetStr := parts[0]
	dest := unquote(strings.TrimSpace(parts[1]))

	seqNums := s.parseUIDSet(uidSetStr)
	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]
		if err := s.mailbox.Copy(msg.UID, dest); err != nil {
			slog.Warn("imap: uid copy failed", "uid", msg.UID, "error", err)
			continue
		}
	}

	s.tagged(tag, "OK", "UID COPY completed")
}

func (s *Session) handleUIDMove(tag, args string) {
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "UID MOVE requires uid set and destination")
		return
	}

	uidSetStr := parts[0]
	dest := unquote(strings.TrimSpace(parts[1]))

	seqNums := s.parseUIDSet(uidSetStr)

	for i := len(seqNums) - 1; i >= 0; i-- {
		seq := seqNums[i]
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]
		if err := s.mailbox.Move(msg.UID, dest); err != nil {
			continue
		}
		s.send("* %d EXPUNGE", seq)
		s.messages = append(s.messages[:seq-1], s.messages[seq:]...)
	}

	if s.selected != nil {
		s.selected.total = int64(len(s.messages))
	}
	s.tagged(tag, "OK", "UID MOVE completed")
}

func (s *Session) handleUIDSearch(tag, args string) {
	// UID SEARCH returns UIDs instead of sequence numbers
	// Parse the criteria the same way as SEARCH
	criteria := s.parseSearchCriteria(args)

	var uids []string
	for _, msg := range s.messages {
		if s.matchesCriteria(msg, criteria) {
			uids = append(uids, strconv.FormatUint(uint64(msg.UID), 10))
		}
	}

	if len(uids) > 0 {
		s.send("* SEARCH %s", strings.Join(uids, " "))
	} else {
		s.send("* SEARCH")
	}
	s.tagged(tag, "OK", "UID SEARCH completed")
}

// ── Output helpers ────────────────────────────────────────────────────

func (s *Session) send(format string, args ...interface{}) {
	s.writeString(fmt.Sprintf(format, args...) + "\r\n")
}

func (s *Session) tagged(tag, status, msg string) {
	s.writeString(tag + " " + status + " " + msg + "\r\n")
}

// writeString emits str to the client and flushes. Protocol writes are
// best-effort: a failure surfaces on the next read, so the error is ignored.
func (s *Session) writeString(str string) {
	_, _ = s.writer.WriteString(str)
	_ = s.writer.Flush()
}

// boolPtr returns a pointer to b, for building a FlagUpdate.
func boolPtr(b bool) *bool { return &b }
