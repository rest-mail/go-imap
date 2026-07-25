package imap

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"runtime/debug"
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
	readOnly bool            // true when the selection was opened via EXAMINE (RFC 3501 §6.3.2)
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

// MaxCommandLineLength bounds a single protocol line read from the client —
// the command line in the main loop, the AUTHENTICATE continuation, the IDLE
// DONE line, and the CRLF trailing an APPEND literal. RFC 3501 sizes command
// lines in the low thousands of octets; 8 KiB leaves generous headroom while
// preventing an unauthenticated client from streaming an unbounded "line" (no
// terminator) to exhaust process memory. It is a package variable so operators
// can tune it.
//
// This cap does NOT limit message payloads: those arrive as length-prefixed
// IMAP literals (e.g. APPEND's {N}), which are read with io.ReadFull against a
// separately enforced size bound and never flow through readLine.
var MaxCommandLineLength = 8 * 1024

// readLine reads one CRLF/LF-terminated line from the client and returns it with
// any trailing CR/LF stripped, enforcing MaxCommandLineLength on the line length
// (excluding the terminator).
//
// If the line exceeds the cap, readLine stops accumulating, drains the remainder
// up to and including the next '\n' without retaining it, and returns
// tooLong=true — so at most one buffer chunk beyond the cap is ever held, no
// matter how much the client streams. Callers reject the command with a BAD
// response instead of buffering the whole line. A read error (including EOF) is
// returned in err with whatever partial line was read.
func (s *Session) readLine() (line string, tooLong bool, err error) {
	var buf []byte
	for {
		// ReadSlice yields chunks bounded by the reader's buffer, so a copy into
		// buf below is required before the next read invalidates chunk.
		chunk, e := s.reader.ReadSlice('\n')
		buf = append(buf, chunk...)

		if len(buf) > MaxCommandLineLength {
			// Over the cap: stop accumulating (buf is discarded on return, so at
			// most one chunk beyond the cap is ever held) and resync the stream.
			if e == nil {
				// Terminator already consumed; stream is at the next line.
				return "", true, nil
			}
			if errors.Is(e, bufio.ErrBufferFull) {
				if de := s.discardToLineEnd(); de != nil {
					return "", true, de
				}
				return "", true, nil
			}
			return "", true, e // read error before any terminator
		}

		if e == nil {
			return strings.TrimRight(string(buf), "\r\n"), false, nil
		}
		if errors.Is(e, bufio.ErrBufferFull) {
			continue // more of the same (still-within-cap) line
		}
		// Real read error / EOF, possibly with a partial final line.
		return strings.TrimRight(string(buf), "\r\n"), false, e
	}
}

// discardToLineEnd consumes bytes up to and including the next '\n' without
// retaining them, resynchronising the stream after an over-long line in
// constant memory. Work is bounded by the connection's read deadline.
func (s *Session) discardToLineEnd() error {
	for {
		_, err := s.reader.ReadSlice('\n')
		if err == nil {
			return nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return err
	}
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

// recoverPanic contains a panic raised anywhere in a single client's serve
// goroutine — typically a bug in a backend Mailbox method or a handler — so that
// one failing session cannot crash the process and drop every other connection.
// It logs the panic with a stack trace, tells the client the server is closing
// this connection (untagged BYE, RFC 3501 §7.1.5), and closes the connection so
// a recovered panic never leaves a hung socket. It must be deferred at the top
// of every goroutine that serves or polls one client (the main [Session.Handle]
// loop and the IDLE poll goroutine).
func (s *Session) recoverPanic() {
	if r := recover(); r != nil {
		slog.Error("imap: recovered panic in session; closing connection",
			"remote", s.conn.RemoteAddr(),
			"panic", r,
			"stack", string(debug.Stack()),
		)
		// Best-effort graceful failure; the close below guarantees the socket is
		// torn down even if this write fails.
		s.send("* BYE Internal server error, closing connection")
		_ = s.conn.Close()
	}
}

// Handle runs the IMAP state machine until the client disconnects or LOGOUTs.
func (s *Session) Handle() {
	defer func() { _ = s.conn.Close() }()
	defer s.recoverPanic() // contain any panic from a backend method or handler

	slog.Info("imap: new connection", "remote", s.conn.RemoteAddr())

	// Send greeting
	s.send("* OK [CAPABILITY IMAP4rev1 STARTTLS AUTH=PLAIN] %s IMAP4rev1 ready", s.hostname)

	for {
		_ = s.conn.SetDeadline(time.Now().Add(30 * time.Minute))

		line, tooLong, err := s.readLine()
		if err != nil {
			slog.Debug("imap: connection closed", "remote", s.conn.RemoteAddr(), "error", err)
			return
		}
		if tooLong {
			slog.Debug("imap: command line too long", "remote", s.conn.RemoteAddr(), "limit", MaxCommandLineLength)
			s.send("* BAD Command line too long (limit %d bytes)", MaxCommandLineLength)
			continue
		}

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
			s.handleSelect(tag, args, false)
		case "EXAMINE":
			s.handleSelect(tag, args, true) // read-only select (RFC 3501 §6.3.2)
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
			// Implicitly expunge \Deleted messages (RFC 3501 §6.4.2), but never
			// for a mailbox opened read-only via EXAMINE (§6.3.2). Unlike EXPUNGE,
			// CLOSE does not send untagged EXPUNGE responses.
			if s.selected != nil && !s.readOnly {
				for _, msg := range s.messages {
					if s.deleted[msg.UID] {
						_ = s.mailbox.Delete(msg.UID)
					}
				}
			}
			s.selected = nil
			s.readOnly = false
			s.messages = nil
			s.deleted = make(map[uint32]bool)
			s.tagged(tag, "OK", "CLOSE completed")
		case "EXPUNGE":
			s.handleExpunge(tag)
		case "UNSELECT":
			s.handleUnselect(tag)
		case "ENABLE":
			s.handleEnable(tag, args)
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
	s.send("* CAPABILITY %s", s.capabilities())
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

	s.authenticate(tag, username, password, "LOGIN completed")
}

func (s *Session) handleAuthenticate(tag, args string) {
	// Gate cleartext SASL the same way LOGIN is gated: when TLS is configured
	// but not yet active, refuse before soliciting any credential so the base64
	// PLAIN response is never sent over the wire (RFC 3501 / RFC 2595).
	if !s.usingTLS && s.tlsConfig != nil {
		s.tagged(tag, "NO", "[PRIVACYREQUIRED] STARTTLS required")
		return
	}

	// Simplified — only support PLAIN
	if !strings.EqualFold(strings.TrimSpace(args), "PLAIN") {
		s.tagged(tag, "NO", "Unsupported mechanism")
		return
	}
	s.send("+")

	line, tooLong, err := s.readLine()
	if err != nil {
		return
	}
	if tooLong {
		s.tagged(tag, "BAD", "Authentication response line too long")
		return
	}
	decoded, err := decodeBase64(line)
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

// resolveUIDValidity picks the UIDVALIDITY reported for folder in SELECT/EXAMINE:
// the mailbox's real value when it implements [UIDPlusMailbox], else the legacy
// constant 1 (unchanged pre-UIDPLUS behaviour).
func (s *Session) resolveUIDValidity(folder string) uint32 {
	if v, ok := s.uidValidity(folder); ok {
		return v
	}
	return 1
}

// authenticate runs the shared LOGIN/AUTHENTICATE credential check, wiring the
// resulting Mailbox and applying limiter accounting. okMsg is the tagged OK text
// sent on success; a [CAPABILITY ...] response code carrying the post-auth
// capability list (which reflects the concrete Mailbox's optional interfaces) is
// prepended so clients see extensions like UIDPLUS without re-probing.
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
	s.tagged(tag, "OK", "[CAPABILITY "+s.capabilities()+"] "+okMsg)
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

// handleSelect implements SELECT (readOnly=false) and EXAMINE (readOnly=true).
// EXAMINE opens the mailbox read-only (RFC 3501 §6.3.2): the tagged OK carries
// [READ-ONLY] and s.readOnly is set so state-mutating commands are refused.
func (s *Session) handleSelect(tag, args string, readOnly bool) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	folder := unquote(strings.TrimSpace(args))
	if folder == "" {
		folder = "INBOX"
	}
	// "INBOX" is case-insensitive (RFC 3501 §5.1): fold any-case spelling to the
	// canonical name so the backend lookup, UIDVALIDITY and selection state all
	// resolve to the one real INBOX.
	folder = canonicalizeInbox(folder)

	// SELECT/EXAMINE begins a new selection (RFC 3501 §6.3.1/§6.3.2), and "if a
	// SELECT command that fails is attempted, no mailbox is selected." Drop any
	// prior selection up front so that a failure below leaves the session in the
	// authenticated-but-unselected state — otherwise a later FETCH/STORE/SEARCH
	// would keep operating on the previously selected mailbox. \Deleted marks are
	// keyed by folder-scoped UIDs, so they must not carry over either (otherwise
	// EXPUNGE could delete a same-numbered UID in the new mailbox).
	s.selected = nil
	s.readOnly = false
	s.messages = nil
	s.deleted = make(map[uint32]bool)

	// Fetch messages for this folder
	messages, err := s.mailbox.Messages(folder)
	if err != nil {
		s.tagged(tag, "NO", "Failed to select folder")
		return
	}

	s.readOnly = readOnly
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
	s.send("* OK [UIDVALIDITY %d]", s.resolveUIDValidity(folder))
	// UIDNEXT (RFC 3501 §2.3.1.1) is the UID that will be assigned to the next
	// message: it must be strictly greater than every existing UID, i.e.
	// highest-UID + 1 — not oldest-UID + 1. maxUID() returns 0 for an empty
	// mailbox, so this yields 1 there.
	s.send("* OK [UIDNEXT %d]", s.maxUID()+1)
	s.send("* FLAGS (\\Seen \\Answered \\Flagged \\Deleted \\Draft)")
	if readOnly {
		// A read-only mailbox permits no permanent flag changes (RFC 3501 §6.3.2).
		s.send("* OK [PERMANENTFLAGS ()]")
		s.tagged(tag, "OK", "[READ-ONLY] EXAMINE completed")
		return
	}
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
	// "INBOX" is case-insensitive (RFC 3501 §5.1); fold to canonical so any-case
	// spelling reports the real INBOX's status.
	folder := canonicalizeInbox(unquote(parts[0]))

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
	tokens := fetchItemTokens(parts[1])

	// Parse sequence numbers
	seqNums := parseSequenceSet(seqStr, len(s.messages))

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]

		if s.fetchResponse(seq, msg, tokens) && !msg.Seen {
			_ = s.mailbox.Store(msg.UID, FlagUpdate{Seen: boolPtr(true)})
			s.messages[seq-1].Seen = true
		}
	}

	s.tagged(tag, "OK", "FETCH completed")
}

// fetchResponse composes and sends a single untagged FETCH response for msg,
// answering exactly the requested data items (RFC 3501 §7.4.2), and reports
// whether a non-peek body-content item was fetched — the only case that requires
// the caller to set \Seen. RFC822.SIZE, RFC822.HEADER, BODY.PEEK[...] and the
// metadata items (FLAGS, INTERNALDATE, ENVELOPE) never trigger that: they must
// not implicitly mark a message read. UID and FLAGS are always included.
func (s *Session) fetchResponse(seq int, msg Message, tokens []string) (marksSeen bool) {
	// raw is loaded lazily: a metadata-only fetch (FLAGS, RFC822.SIZE, …) must
	// never pull the body — that is the entire point of answering RFC822.SIZE
	// from the stored size rather than the message content.
	var raw string
	rawLoaded, rawOK := false, false
	loadRaw := func() (string, bool) {
		if !rawLoaded {
			rawLoaded = true
			if b, err := s.mailbox.Fetch(msg.UID); err == nil {
				raw, rawOK = string(b), true
			}
		}
		return raw, rawOK
	}

	// plain holds non-literal "name value" fragments; lits holds body-content
	// fragments emitted as IMAP literals after the plain items. The FLAGS slot is
	// a placeholder filled in after the item loop: a non-peek body fetch of a
	// previously-unseen message sets \Seen as a side effect, and the FLAGS echoed
	// in THIS response must reflect that new state (RFC 3501 §6.4.5) — otherwise
	// the client is never told the message became \Seen. FLAGS is always index 1.
	plain := []string{
		fmt.Sprintf("UID %d", msg.UID),
		"", // FLAGS — set after the loop, once marksSeen is known
	}
	type litFrag struct{ name, payload string }
	var lits []litFrag

	for _, item := range tokens {
		switch up := strings.ToUpper(item); {
		case up == "FLAGS", up == "UID":
			// Always included above.
		case up == "RFC822.SIZE":
			plain = append(plain, fmt.Sprintf("RFC822.SIZE %d", msg.Size))
		case up == "INTERNALDATE":
			plain = append(plain, fmt.Sprintf("INTERNALDATE %q", msg.Date.Format("02-Jan-2006 15:04:05 -0700")))
		case up == "ENVELOPE":
			plain = append(plain, "ENVELOPE "+buildEnvelope(msg))
		case up == "BODYSTRUCTURE":
			// Body structure is metadata (RFC 3501 §7.4.2): it must never set
			// \Seen. It needs the raw message to describe the MIME layout.
			if r, ok := loadRaw(); ok {
				plain = append(plain, "BODYSTRUCTURE "+buildBodyStructure(r, true))
			}
		case up == "BODY":
			// The bare BODY item is the non-extensible form of BODYSTRUCTURE
			// (no extension fields); likewise metadata, so no \Seen.
			if r, ok := loadRaw(); ok {
				plain = append(plain, "BODY "+buildBodyStructure(r, false))
			}
		case up == "RFC822":
			if r, ok := loadRaw(); ok {
				lits = append(lits, litFrag{"RFC822", r})
				marksSeen = true
			}
		case up == "RFC822.HEADER": // defined as BODY.PEEK[HEADER]: no \Seen.
			if r, ok := loadRaw(); ok {
				lits = append(lits, litFrag{"RFC822.HEADER", headerSection(r)})
			}
		case up == "RFC822.TEXT":
			if r, ok := loadRaw(); ok {
				lits = append(lits, litFrag{"RFC822.TEXT", textSection(r)})
				marksSeen = true
			}
		case strings.HasPrefix(up, "BODY[") || strings.HasPrefix(up, "BODY.PEEK["):
			name, payload, peek, ok := bodySection(item, loadRaw)
			if !ok {
				continue
			}
			lits = append(lits, litFrag{name, payload})
			if !peek {
				marksSeen = true
			}
		default:
			// Unknown / unsupported data item — ignore.
		}
	}

	// A fetch marks the message \Seen only when it pulled non-peek body content
	// (marksSeen) and the message was not already seen. When it does, echo the
	// post-change flags — including \Seen — so this same FETCH response tells the
	// client the message is now seen (RFC 3501 §6.4.5). A peek fetch or an
	// already-seen message leaves the flags unchanged. msg is a local copy, so
	// this does not mutate session state.
	echoed := msg
	if marksSeen && !msg.Seen {
		echoed.Seen = true
	}
	plain[1] = fmt.Sprintf("FLAGS (%s)", s.flagString(echoed))

	head := fmt.Sprintf("* %d FETCH (%s", seq, strings.Join(plain, " "))
	if len(lits) == 0 {
		s.writeString(head + ")\r\n")
		return marksSeen
	}
	s.writeString(head)
	for _, lf := range lits {
		s.writeString(fmt.Sprintf(" %s {%d}\r\n", lf.name, len(lf.payload)))
		s.writeString(lf.payload)
	}
	s.writeString(")\r\n")
	return marksSeen
}

// ── SEARCH ────────────────────────────────────────────────────────────

type searchCriterion struct {
	kind string // "all", "seen", "unseen", "flagged", "unflagged", "deleted", "undeleted",
	// "from", "to", "subject", "since", "before", "on", "uid", "not", "or"
	value  string            // for string/date/uid criteria
	date   time.Time         // parsed date for since/before/on
	sub    []searchCriterion // for NOT (1 element) or OR (2 elements)
	ranges []seqRange        // for uid: pre-parsed UID ranges, tested by membership (never expanded)
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
// Any UID set is parsed into ranges here — once, not once per message — so the
// per-message match is a cheap membership test (issue #8).
func (s *Session) parseSearchCriteria(args string) []searchCriterion {
	tokens := tokenizeSearch(args)
	maxUID := s.maxUID()
	var criteria []searchCriterion
	idx := 0
	for idx < len(tokens) {
		c, newIdx := parseSingleCriterion(tokens, idx, maxUID)
		criteria = append(criteria, c)
		idx = newIdx
	}
	return criteria
}

// maxUID returns the largest UID among the currently selected messages, or 0
// when the mailbox is empty. It resolves "*" in a UID set to the real high-water
// mark instead of an arbitrary large constant.
func (s *Session) maxUID() uint32 {
	var max uint32
	for _, m := range s.messages {
		if m.UID > max {
			max = m.UID
		}
	}
	return max
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
// maxUID resolves "*" in a UID set to the mailbox's high-water mark.
func parseSingleCriterion(tokens []string, idx int, maxUID uint32) (searchCriterion, int) {
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
			return searchCriterion{kind: "uid", value: tokens[idx+1], ranges: parseUIDRanges(tokens[idx+1], maxUID)}, idx + 2
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "NOT":
		if idx+1 < len(tokens) {
			sub, newIdx := parseSingleCriterion(tokens, idx+1, maxUID)
			return searchCriterion{kind: "not", sub: []searchCriterion{sub}}, newIdx
		}
		return searchCriterion{kind: "all"}, idx + 1
	case "OR":
		if idx+2 < len(tokens) {
			sub1, newIdx1 := parseSingleCriterion(tokens, idx+1, maxUID)
			sub2, newIdx2 := parseSingleCriterion(tokens, newIdx1, maxUID)
			return searchCriterion{kind: "or", sub: []searchCriterion{sub1, sub2}}, newIdx2
		}
		return searchCriterion{kind: "all"}, idx + 1
	default:
		// Unknown token — treat as ALL (ignore)
		return searchCriterion{kind: "all"}, idx + 1
	}
}

// parseIMAPDateTime parses an IMAP date-time (RFC 3501 §9) as sent in the
// optional APPEND argument, e.g. "25-Jul-2026 13:04:05 +0000". A single-digit
// day may be space-padded per the grammar or bare; both are accepted. ok is
// false when s is not a valid date-time.
func parseIMAPDateTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse("2-Jan-2006 15:04:05 -0700", s); err == nil {
		return t, true
	}
	return time.Time{}, false
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
		// Test membership against the pre-parsed ranges (built once in
		// parseSearchCriteria). Never expand the set: a crafted "1:4294967295"
		// must not iterate billions of times per message (issue #8).
		return uidInRanges(msg.UID, c.ranges)
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
	if s.refuseReadOnly(tag) {
		return
	}

	// Parse: STORE <seq> (FLAGS|+FLAGS|-FLAGS)[.SILENT] (<flags>)
	parts := strings.SplitN(args, " ", 3)
	if len(parts) < 3 {
		s.tagged(tag, "BAD", "STORE requires sequence, action, and flags")
		return
	}

	mode, silent, ok := parseStoreAction(parts[1])
	if !ok {
		s.tagged(tag, "BAD", "STORE requires FLAGS, +FLAGS or -FLAGS")
		return
	}

	seqNums := parseSequenceSet(parts[0], len(s.messages))
	flags := parseFlags(parts[2])

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		s.applyStore(seq, mode, flags, silent, false)
	}

	s.tagged(tag, "OK", "STORE completed")
}

// storeMode is the STORE operation selected by the data-item name: replace
// (bare FLAGS), add (+FLAGS) or remove (-FLAGS), per RFC 3501 §6.4.6.
type storeMode int

const (
	storeReplace storeMode = iota
	storeAdd
	storeRemove
)

// parseStoreAction parses a STORE flags data-item name such as "FLAGS",
// "+FLAGS" or "-FLAGS.SILENT" (case-insensitive). It returns the mode, whether
// the .SILENT suffix was present, and ok=false if the token is not a recognised
// FLAGS store item.
func parseStoreAction(action string) (mode storeMode, silent bool, ok bool) {
	a := strings.ToUpper(strings.TrimSpace(action))
	if strings.HasSuffix(a, ".SILENT") {
		silent = true
		a = strings.TrimSuffix(a, ".SILENT")
	}
	switch a {
	case "FLAGS":
		return storeReplace, silent, true
	case "+FLAGS":
		return storeAdd, silent, true
	case "-FLAGS":
		return storeRemove, silent, true
	default:
		return 0, false, false
	}
}

// applyStore mutates the message at the given sequence number per RFC 3501
// §6.4.6 and, unless silent, emits the untagged FETCH carrying the resulting
// flags. When withUID is true the FETCH also reports the UID (UID STORE).
func (s *Session) applyStore(seq int, mode storeMode, flags []string, silent, withUID bool) {
	msg := &s.messages[seq-1]
	update := s.applyStoreFlags(msg, mode, flags)

	if update.Seen != nil || update.Answered != nil || update.Flagged != nil || update.Draft != nil {
		_ = s.mailbox.Store(msg.UID, update)
	}

	if silent {
		return
	}
	if withUID {
		s.send("* %d FETCH (UID %d FLAGS (%s))", seq, msg.UID, s.flagString(*msg))
	} else {
		s.send("* %d FETCH (FLAGS (%s))", seq, s.flagString(*msg))
	}
}

// applyStoreFlags applies the flag change to msg (and the session's \Deleted
// set) and returns the FlagUpdate to persist. In replace mode every settable
// system flag is forced to its membership in flags; in add/remove mode only the
// listed flags are toggled. Unrecognised flags and keywords are accepted but not
// stored (the model represents only the system flags).
func (s *Session) applyStoreFlags(msg *Message, mode storeMode, flags []string) FlagUpdate {
	listed := func(name string) bool {
		for _, f := range flags {
			if strings.EqualFold(f, name) {
				return true
			}
		}
		return false
	}
	apply := func(name string, cur *bool, out **bool) {
		switch mode {
		case storeReplace:
			v := listed(name)
			*cur = v
			*out = boolPtr(v)
		case storeAdd:
			if listed(name) {
				*cur = true
				*out = boolPtr(true)
			}
		case storeRemove:
			if listed(name) {
				*cur = false
				*out = boolPtr(false)
			}
		}
	}

	var update FlagUpdate
	apply(`\Seen`, &msg.Seen, &update.Seen)
	apply(`\Answered`, &msg.Answered, &update.Answered)
	apply(`\Flagged`, &msg.Flagged, &update.Flagged)
	apply(`\Draft`, &msg.Draft, &update.Draft)

	// \Deleted is per-session state (reset on SELECT), not part of the persistent
	// Message model, so it never rides in the FlagUpdate sent to the backend.
	s.applyDeleted(msg.UID, mode, listed(`\Deleted`))

	return update
}

// applyDeleted updates the session's \Deleted set for uid. In replace mode the
// flag is set to its membership in the STORE list; in add/remove mode it changes
// only when \Deleted appears in the list.
func (s *Session) applyDeleted(uid uint32, mode storeMode, listed bool) {
	var target bool
	switch mode {
	case storeReplace:
		target = listed
	case storeAdd:
		if !listed {
			return
		}
		target = true
	case storeRemove:
		if !listed {
			return
		}
		target = false
	}
	if target {
		if s.deleted == nil {
			s.deleted = make(map[uint32]bool)
		}
		s.deleted[uid] = true
	} else {
		delete(s.deleted, uid)
	}
}

// flagString renders msg's flags for a FETCH response, adding the session-local
// \Deleted state that buildFlags (which sees only the Message) cannot know.
func (s *Session) flagString(msg Message) string {
	flags := buildFlags(msg)
	if s.deleted[msg.UID] {
		if flags == "" {
			return `\Deleted`
		}
		return flags + ` \Deleted`
	}
	return flags
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

	if code := s.copyMessages(seqNums, dest); code != "" {
		s.tagged(tag, "OK", "["+code+"] COPY completed")
		return
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
	s.moveMessages(tag, "MOVE", seqNums, dest)
}

func (s *Session) handleExpunge(tag string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}
	if s.refuseReadOnly(tag) {
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
	// INBOX always exists and is case-insensitive (RFC 3501 §5.1, §6.3.3), so
	// CREATE of any-case "INBOX" must fail rather than silently succeed.
	if strings.EqualFold(folder, "INBOX") {
		s.tagged(tag, "NO", "INBOX already exists")
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
	// "INBOX" is case-insensitive (RFC 3501 §5.1): fold to canonical so any-case
	// spelling ("inbox", "InBoX") hits the standard-folder guard below rather
	// than bypassing it. Other standard names stay case-sensitive.
	folder = canonicalizeInbox(folder)

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
	// "INBOX" is case-insensitive (RFC 3501 §5.1): fold the source name so any-case
	// spelling matches the standard-folder guard below rather than bypassing it.
	oldName = canonicalizeInbox(oldName)

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

	// APPEND mailbox [flag-list] [date-time] literal  (RFC 3501 §6.3.11). The
	// mailbox is an astring: an atom (unquoted), a quoted-string, or a literal.
	// Parse it as an astring so an unquoted name is honoured rather than ignored,
	// which previously left folder at "INBOX" and misdelivered the message.
	folder, rest, ok := parseAstring(args)
	if !ok || folder == "" {
		s.tagged(tag, "BAD", "Missing mailbox name")
		return
	}
	// "INBOX" is case-insensitive (RFC 3501 §5.1): fold to canonical so an any-case
	// destination delivers to the one real INBOX, not a phantom "inbox" mailbox.
	folder = canonicalizeInbox(folder)

	// Optional flag-list: (\Seen \Draft ...) immediately after the mailbox. Taking
	// it positionally — rather than scanning for the first '(' — avoids matching a
	// parenthesis inside a quoted mailbox name.
	var appendFlags []string
	rest = strings.TrimLeft(rest, " ")
	if strings.HasPrefix(rest, "(") {
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			s.tagged(tag, "BAD", "Malformed flag list")
			return
		}
		appendFlags = parseFlags(rest[:end+1])
		rest = rest[end+1:]
	}

	// Translate the flags into a neutral FlagUpdate, rejecting any flag the store
	// cannot honour so the client is not misled into thinking it took effect. This
	// runs before the continuation: a rejected APPEND must not read the literal.
	var flags FlagUpdate
	for _, flag := range appendFlags {
		switch flag {
		case `\Seen`:
			flags.Seen = boolPtr(true)
		case `\Answered`:
			flags.Answered = boolPtr(true)
		case `\Flagged`:
			flags.Flagged = boolPtr(true)
		case `\Draft`:
			flags.Draft = boolPtr(true)
		case `\Deleted`:
			// \Deleted is a valid system flag (it is advertised in PERMANENTFLAGS),
			// but this engine models it as per-session state, not a persistent flag
			// the backend stores (see applyDeleted). The appended message is not part
			// of any selection, so accept the flag without error but do not persist
			// it — matching how \Deleted is handled everywhere else.
		default:
			s.tagged(tag, "NO", "Unsupported flag: "+flag)
			return
		}
	}

	// Optional date-time: a quoted "dd-Mon-yyyy HH:MM:SS +ZZZZ" that sets the
	// message's INTERNALDATE. Parsed and validated here; it is stored only if the
	// backend implements DateAppender (otherwise it is dropped, best-effort).
	var internalDate time.Time
	rest = strings.TrimLeft(rest, " ")
	if strings.HasPrefix(rest, `"`) {
		dateStr, after, dok := parseAstring(rest)
		if !dok {
			s.tagged(tag, "BAD", "Malformed date-time")
			return
		}
		t, tok := parseIMAPDateTime(dateStr)
		if !tok {
			s.tagged(tag, "BAD", "Invalid date-time")
			return
		}
		internalDate = t
		rest = after
	}

	// The message follows as a synchronizing literal {N}; tolerate the {N+}
	// non-synchronizing form's trailing '+'.
	braceStart := strings.LastIndex(rest, "{")
	braceEnd := strings.LastIndex(rest, "}")
	if braceStart < 0 || braceEnd <= braceStart {
		s.tagged(tag, "BAD", "Missing literal size")
		return
	}
	sizeStr := strings.TrimSuffix(rest[braceStart+1:braceEnd], "+")
	size, err := strconv.Atoi(sizeStr)
	if err != nil || size < 0 {
		s.tagged(tag, "BAD", "Invalid literal size")
		return
	}
	if size > 10*1024*1024 {
		s.tagged(tag, "NO", "Message too large")
		return
	}

	// Send continuation.
	s.send("+ Ready for literal data")

	// Read exactly size bytes.
	data := make([]byte, size)
	if _, err = io.ReadFull(s.reader, data); err != nil {
		s.tagged(tag, "NO", "Failed to read message data")
		return
	}

	// Read (and bound) the CRLF trailing the literal payload.
	_, _, _ = s.readLine()

	// Deliver. Precedence: a client date-time with a DateAppender backend honours
	// INTERNALDATE; otherwise a UIDPlusMailbox reports the assigned UID; otherwise
	// the base Append. AppendWithDate and AppendUID both return the UID for the
	// APPENDUID (RFC 4315) response code.
	up, isUIDPlus := s.mailbox.(UIDPlusMailbox)
	da, isDater := s.mailbox.(DateAppender)
	var uid uint32
	switch {
	case !internalDate.IsZero() && isDater:
		uid, err = da.AppendWithDate(folder, flags, internalDate, data)
	case isUIDPlus:
		uid, err = up.AppendUID(folder, flags, data)
	default:
		err = s.mailbox.Append(folder, flags, data)
	}
	if err != nil {
		slog.Warn("imap: append failed", "error", err)
		s.tagged(tag, "NO", "APPEND failed")
		return
	}

	// APPENDUID (RFC 4315) is emitted only for a UIDPLUS backend that reported a
	// real UID, whether the store handled the append via AppendUID or the
	// date-aware AppendWithDate.
	if isUIDPlus && uid != 0 {
		if v, ok := s.uidValidity(folder); ok {
			s.tagged(tag, "OK", fmt.Sprintf("[APPENDUID %d %d] APPEND completed", v, uid))
			return
		}
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

// idlePollInterval is how often the IDLE poll goroutine re-reads the selected
// mailbox to announce new messages. It is a var, not a const, only so tests can
// shorten it; production always uses the default.
var idlePollInterval = 15 * time.Second

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
		defer s.recoverPanic() // a backend Messages panic here is a separate goroutine; contain it too
		ticker := time.NewTicker(idlePollInterval)
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
		line, tooLong, err := s.readLine()
		if err != nil {
			close(done)
			<-stopped // let any in-flight poll finish before we return
			return
		}
		if tooLong {
			continue // ignore over-long junk; keep waiting for DONE
		}
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
	case "EXPUNGE":
		s.handleUIDExpunge(tag, subArgs)
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
	tokens := fetchItemTokens(parts[1])

	seqNums := s.parseUIDSet(uidSetStr)

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]

		if s.fetchResponse(seq, msg, tokens) && !msg.Seen {
			_ = s.mailbox.Store(msg.UID, FlagUpdate{Seen: boolPtr(true)})
			s.messages[seq-1].Seen = true
		}
	}

	s.tagged(tag, "OK", "UID FETCH completed")
}

func (s *Session) handleUIDStore(tag, args string) {
	if s.refuseReadOnly(tag) {
		return
	}
	parts := strings.SplitN(args, " ", 2)
	if len(parts) < 2 {
		s.tagged(tag, "BAD", "UID STORE requires uid set and flags")
		return
	}

	// Parse the flags data-item once (it is the same for every message in the set).
	flagParts := strings.SplitN(parts[1], " ", 2)
	if len(flagParts) < 2 {
		s.tagged(tag, "BAD", "UID STORE requires an action and flags")
		return
	}
	mode, silent, ok := parseStoreAction(flagParts[0])
	if !ok {
		s.tagged(tag, "BAD", "UID STORE requires FLAGS, +FLAGS or -FLAGS")
		return
	}
	flags := parseFlags(flagParts[1])

	seqNums := s.parseUIDSet(parts[0])
	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		s.applyStore(seq, mode, flags, silent, true)
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
	if code := s.copyMessages(seqNums, dest); code != "" {
		s.tagged(tag, "OK", "["+code+"] UID COPY completed")
		return
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
	s.moveMessages(tag, "UID MOVE", seqNums, dest)
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

// refuseReadOnly answers a tagged NO and returns true when the current selection
// was opened read-only via EXAMINE (RFC 3501 §6.3.2): no command may change the
// mailbox's permanent state. State-mutating handlers (STORE, EXPUNGE, MOVE and
// their UID variants) call this before touching the backend.
func (s *Session) refuseReadOnly(tag string) bool {
	if s.readOnly {
		s.tagged(tag, "NO", "Mailbox is read-only")
		return true
	}
	return false
}

// writeString emits str to the client and flushes. Protocol writes are
// best-effort: a failure surfaces on the next read, so the error is ignored.
func (s *Session) writeString(str string) {
	_, _ = s.writer.WriteString(str)
	_ = s.writer.Flush()
}

// boolPtr returns a pointer to b, for building a FlagUpdate.
func boolPtr(b bool) *bool { return &b }
