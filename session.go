package imap

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/mail"
	"os"
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

// MaxLiteralSize bounds a synchronizing (or LITERAL+ non-synchronizing) literal
// used as an ordinary string/astring command argument — a literal mailbox name,
// LOGIN credential, SEARCH key, etc. (RFC 3501 §4.3). Such arguments are short by
// nature; 8 KiB is far more than any real name or credential needs while
// preventing an unauthenticated client from declaring a huge {N} to make the
// server allocate and read unbounded octets before authenticating. The declared
// size is checked BEFORE the "+" continuation is sent, so an over-large literal
// is refused without its octets ever being invited. It is a package variable so
// operators can tune it.
//
// This cap is deliberately independent of — and much smaller than — APPEND's own
// message-literal limit: APPEND carries whole messages and keeps its separate,
// larger bound on its dedicated read path (handleAppend), which this general
// argument-literal handling never touches.
var MaxLiteralSize = 8 * 1024

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
	line, tooLong, _, err = s.readCommandLine()
	return line, tooLong, err
}

// readCommandLine is readLine that also reports, for a discarded over-long
// line, whether it ended in "+}" — a possible LITERAL+ marker whose octets are
// already in flight (RFC 7888 §3). Only the last few raw octets are kept, so the
// check costs no extra memory however long the line is.
func (s *Session) readCommandLine() (line string, tooLong, nonSyncLiteral bool, err error) {
	var buf []byte
	for {
		// ReadSlice yields chunks bounded by the reader's buffer, so a copy into
		// buf below is required before the next read invalidates chunk.
		chunk, e := s.reader.ReadSlice('\n')
		buf = append(buf, chunk...)

		if len(buf) > MaxCommandLineLength {
			// Over the cap: stop accumulating (buf is discarded on return, so at
			// most one chunk beyond the cap is ever held) and resync the stream.
			tail := lineTail(nil, buf)
			if e == nil {
				// Terminator already consumed; stream is at the next line.
				return "", true, endsNonSyncLiteral(tail), nil
			}
			if errors.Is(e, bufio.ErrBufferFull) {
				tail, de := s.discardToLineEnd(tail)
				if de != nil {
					return "", true, false, de
				}
				return "", true, endsNonSyncLiteral(tail), nil
			}
			return "", true, false, e // read error before any terminator
		}

		if e == nil {
			return strings.TrimRight(string(buf), "\r\n"), false, false, nil
		}
		if errors.Is(e, bufio.ErrBufferFull) {
			continue // more of the same (still-within-cap) line
		}
		// Real read error / EOF, possibly with a partial final line.
		return strings.TrimRight(string(buf), "\r\n"), false, false, e
	}
}

// discardToLineEnd consumes bytes up to and including the next '\n' without
// retaining them, resynchronising the stream after an over-long line in
// constant memory. Work is bounded by the connection's read deadline. It
// returns the line's last few octets, continuing from tail.
func (s *Session) discardToLineEnd(tail []byte) ([]byte, error) {
	for {
		chunk, err := s.reader.ReadSlice('\n')
		tail = lineTail(tail, chunk)
		if err == nil {
			return tail, nil
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		return tail, err
	}
}

// lineTail keeps the last four octets of tail followed by chunk: enough for a
// "+}" marker plus its CRLF terminator.
func lineTail(tail, chunk []byte) []byte {
	const keep = 4
	if len(chunk) >= keep {
		return append([]byte(nil), chunk[len(chunk)-keep:]...)
	}
	tail = append(tail, chunk...)
	if len(tail) > keep {
		tail = append([]byte(nil), tail[len(tail)-keep:]...)
	}
	return tail
}

// endsNonSyncLiteral reports whether a line tail ends in "+}" before its
// terminator.
func endsNonSyncLiteral(tail []byte) bool {
	return strings.HasSuffix(strings.TrimRight(string(tail), "\r\n"), "+}")
}

// readLiteralArgs materializes any synchronizing (or LITERAL+ non-synchronizing)
// literals embedded in a command's argument text and returns the fully assembled
// arguments (RFC 3501 §4.3). A literal — "{n}" / "{n+}" at the end of a line —
// may stand in for any string/astring argument of any command, so this runs for
// every command except APPEND (whose terminal message literal is read on its own
// large-message path in handleAppend).
//
// For each trailing literal it: bounds the declared size against MaxLiteralSize
// (before any continuation, so an over-large literal's octets are never invited);
// sends a "+" continuation for a synchronizing literal (a LITERAL+ "{n+}" needs
// none — its octets are already in flight); reads exactly n octets as the
// argument value; reads the remainder of the command line that follows the
// octets; and splices the octets back in place of the marker, re-encoded as a
// quoted-string so the per-command parsers consume them unchanged (an embedded
// quote/backslash is escaped; other 8-bit content, including CR/LF/NUL, is
// preserved verbatim for those parsers to validate). The spliced-in continuation
// may itself end in another literal, so this repeats until the assembled line
// carries no trailing literal.
//
// ok is false after a tagged BAD for a resource limit, or a read error/EOF;
// the caller must not dispatch the command. closeConn also tells the caller to
// end the session when unread literal bytes or a lost line boundary make the
// stream unsafe to reuse. Refusals with unread non-synchronizing literals send
// BYE instead of draining potentially large payloads (RFC 7888 §§3–4).
// Memory stays bounded:
// the aggregate literal octets are capped at MaxLiteralSize and the assembled
// line at MaxCommandLineLength, both independent of how the client chunks input.
func (s *Session) readLiteralArgs(tag, args string) (assembled string, ok, closeConn bool) {
	reject := func(reason string, fatal bool) (string, bool, bool) {
		s.tagged(tag, "BAD", reason)
		if fatal {
			s.send("* BYE Command literal rejected; stream cannot be reused")
		}
		return "", false, fatal
	}
	total := 0 // aggregate literal octets across this command
	for {
		prefix, size, sync, isLit := splitTrailingLiteral(args)
		if !isLit {
			return args, true, false // no (further) trailing literal
		}

		// Bound the literal BEFORE soliciting its octets. A synchronizing literal
		// is refused without a continuation, so the client never sends the octets.
		if size > MaxLiteralSize || total+size > MaxLiteralSize {
			return reject(fmt.Sprintf("Literal too large (limit %d bytes)", MaxLiteralSize), !sync)
		}
		total += size

		if sync {
			s.send("+ Ready for literal data")
		}

		buf := make([]byte, size)
		if _, err := io.ReadFull(s.reader, buf); err != nil {
			return "", false, true // never dispatch buffered partial input after a read error
		}

		// The command continues on the line following the literal octets.
		cont, tooLong, err := s.readLine()
		if err != nil {
			return "", false, true
		}
		if tooLong {
			// readLine discarded the suffix, including any next literal marker.
			return reject(fmt.Sprintf("Command line too long (limit %d bytes)", MaxCommandLineLength), true)
		}

		args = prefix + quoteLiteral(buf) + cont
		if len(args) > MaxCommandLineLength {
			// Only the continuation can introduce another unread literal. A
			// synchronizing marker has not invited bytes; a complete command
			// without a marker has already consumed its final CRLF.
			_, _, nextSync, nextLiteral := splitTrailingLiteral(cont)
			return reject(fmt.Sprintf("Command too long (limit %d bytes)", MaxCommandLineLength), nextLiteral && !nextSync)
		}
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

// autologoutTimeout is the inactivity window after which the server autologs the
// client out (RFC 3501 §5.4). The RFC requires this timer to be at least 30
// minutes, so production must never shorten it; it is a var, not a const, only so
// tests can drive the autologout path quickly — the same seam idlePollInterval
// uses. When it fires, [Session.Handle] and [Session.handleIdle] announce the
// close with an untagged "* BYE" (§7.1.5) before dropping the connection.
var autologoutTimeout = 30 * time.Minute

// isAutologout reports whether err is the inactivity read-deadline expiry
// (autologout) rather than a peer disconnect or other read error — the only case
// that warrants sending a "* BYE" before closing, since a disconnected peer can
// receive nothing. A net.Conn (and net.Pipe) read past its deadline returns
// os.ErrDeadlineExceeded, which readLine surfaces unwrapped.
func isAutologout(err error) bool {
	return errors.Is(err, os.ErrDeadlineExceeded)
}

// autologoutByeGrace bounds how long the autologout BYE write may block. The
// inactivity deadline that just fired also governs writes (net.Conn.SetDeadline
// sets both directions), so a fresh write deadline is required or the BYE would
// itself time out and never leave the buffer; the grace is short so a wedged
// client cannot pin the serve goroutine.
var autologoutByeGrace = 10 * time.Second

// sendAutologoutBye announces the inactivity autologout with an untagged BYE
// (RFC 3501 §5.4/§7.1.5) after refreshing the write deadline the expired
// inactivity deadline left in the past, then leaves the caller to close the
// connection.
func (s *Session) sendAutologoutBye() {
	_ = s.conn.SetWriteDeadline(time.Now().Add(autologoutByeGrace))
	s.send("* BYE Autologout; idle for too long")
}

// Handle runs the IMAP state machine until the client disconnects or LOGOUTs.
func (s *Session) Handle() {
	defer func() { _ = s.conn.Close() }()
	defer s.recoverPanic() // contain any panic from a backend method or handler

	slog.Info("imap: new connection", "remote", s.conn.RemoteAddr())

	// Send greeting. The CAPABILITY response code is built from the live session
	// state (via s.capabilities()) rather than hardcoded, so it reflects the TLS
	// state and the LOGINDISABLED policy and never advertises what the server
	// would refuse (RFC 3501 §7.1.1).
	s.send("* OK [CAPABILITY %s] %s IMAP4rev1 ready", s.capabilities(), s.hostname)

	for {
		_ = s.conn.SetDeadline(time.Now().Add(autologoutTimeout))

		line, tooLong, nonSyncLiteral, err := s.readCommandLine()
		if err != nil {
			if isAutologout(err) {
				// Inactivity autologout (RFC 3501 §5.4): announce the close with an
				// untagged BYE (§7.1.5) before the deferred Close drops the socket,
				// rather than disconnecting silently.
				slog.Info("imap: autologout on inactivity", "remote", s.conn.RemoteAddr())
				s.sendAutologoutBye()
				return
			}
			slog.Debug("imap: connection closed", "remote", s.conn.RemoteAddr(), "error", err)
			return
		}
		if tooLong {
			slog.Debug("imap: command line too long", "remote", s.conn.RemoteAddr(), "limit", MaxCommandLineLength)
			s.send("* BAD Command line too long (limit %d bytes)", MaxCommandLineLength)
			if nonSyncLiteral {
				// The discarded line may have ended in a LITERAL+ marker whose
				// octets are already in flight; reading on would run them as
				// commands. RFC 7888 §4 permits BYE-and-close here.
				s.send("* BYE Command line too long with a pending literal")
				return
			}
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

		ucmd := strings.ToUpper(cmd)

		// A synchronizing literal {n} (or LITERAL+ {n+}) may stand in for any
		// string/astring argument of any command (RFC 3501 §4.3): send the "+"
		// continuation, read the octets, and splice them into the argument text so
		// the per-command parsers below see a fully materialized command line —
		// rather than misreading the octet line as a new command and desyncing.
		// APPEND is the sole exception: its terminal message literal is read on
		// handleAppend's large-message path, which needs the raw octets, not a
		// re-encoded argument.
		if ucmd != "APPEND" {
			materialized, argsOK, closeConn := s.readLiteralArgs(tag, args)
			if closeConn {
				return // buffered unread octets must never become new commands
			}
			if !argsOK {
				continue // safe refusal before a continuation or after a complete command
			}
			args = materialized
		}

		switch ucmd {
		case "CAPABILITY":
			s.handleCapability(tag)
		case "STARTTLS":
			if s.handleSTARTTLS(tag, args) {
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
		case "SUBSCRIBE":
			s.handleSubscribe(tag, args)
		case "UNSUBSCRIBE":
			s.handleUnsubscribe(tag, args)
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
			if s.handleAppend(tag, args) {
				return
			}
		case "NOOP":
			s.tagged(tag, "OK", "NOOP completed")
		case "CHECK":
			// CHECK is a Selected State command (RFC 3501 §6.4.1): reject it when
			// no mailbox is selected — which also covers the non-authenticated
			// state, since a mailbox can only be selected after authentication.
			if s.selected == nil {
				s.tagged(tag, "BAD", "CHECK valid only in selected state")
				break
			}
			s.tagged(tag, "OK", "CHECK completed")
		case "CLOSE":
			if args != "" {
				s.tagged(tag, "BAD", "CLOSE takes no arguments")
				break
			}
			// CLOSE is a Selected State command (RFC 3501 §6.4.2): reject it when
			// no mailbox is selected rather than answering OK.
			if s.selected == nil {
				s.tagged(tag, "BAD", "CLOSE valid only in selected state")
				break
			}
			// Implicitly expunge \Deleted messages (RFC 3501 §6.4.2), but never
			// for a mailbox opened read-only via EXAMINE (§6.3.2). Unlike EXPUNGE,
			// CLOSE does not send untagged EXPUNGE responses.
			if !s.readOnly {
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
			if args != "" {
				s.tagged(tag, "BAD", "EXPUNGE takes no arguments")
				break
			}
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
			if s.handleIdle(tag) {
				return
			}
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

func (s *Session) handleSTARTTLS(tag, args string) bool {
	// STARTTLS takes no arguments (RFC 3501 §6.2.1); refuse rather than begin a
	// negotiation the client may not expect.
	if args != "" {
		s.tagged(tag, "BAD", "STARTTLS takes no arguments")
		return false
	}
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
	// SECURITY: install a FRESH reader over the TLS conn, discarding anything the
	// cleartext bufio.Reader had already buffered from the socket. A man-in-the-
	// middle can pipeline plaintext IMAP commands after STARTTLS in the same
	// segment; preserving that buffer would run them as though they arrived inside
	// the TLS session (plaintext injection, CVE-2011-0411 class). A conforming
	// client sends nothing until it sees this tagged OK, so no legitimate data is
	// lost by discarding.
	s.reader = bufio.NewReader(tlsConn)
	s.writer = bufio.NewWriter(tlsConn)
	s.usingTLS = true

	slog.Info("imap: TLS established", "remote", s.conn.RemoteAddr())
	return false
}

func (s *Session) handleLogin(tag, args string) {
	// Enforce the same policy the greeting advertises via LOGINDISABLED: refuse
	// plaintext LOGIN until TLS is established (RFC 3501 §7.1.1, RFC 2595).
	if s.loginDisabled() {
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
	if s.loginDisabled() {
		s.tagged(tag, "NO", "[PRIVACYREQUIRED] STARTTLS required")
		return
	}

	// Simplified — only support PLAIN
	if !strings.EqualFold(strings.TrimSpace(args), "PLAIN") {
		s.tagged(tag, "NO", "Unsupported mechanism")
		return
	}
	// Continuation request: "+" SP [base64-challenge] (RFC 3501 §7.5 / §6.2.2,
	// RFC 4959). PLAIN carries no server challenge, so the base64 part is empty
	// and the line is "+ " (note the required trailing space after the "+").
	s.send("+ ")

	line, tooLong, err := s.readLine()
	if err != nil {
		return
	}
	if tooLong {
		s.tagged(tag, "BAD", "Authentication response line too long")
		return
	}
	// A lone "*" is the SASL abort signal (RFC 4959): the client cancels the
	// exchange, and the server MUST reject the AUTHENTICATE command with a tagged
	// BAD — not decode it as base64 and report a NO.
	if line == "*" {
		s.tagged(tag, "BAD", "authentication aborted")
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
		s.send(`* LIST (%s) "/" %s`, attrs, encodeMailboxName(f.Name))
	}
	s.tagged(tag, "OK", "LIST completed")
}

// handleSubscribe implements SUBSCRIBE (RFC 3501 §6.3.6) and handleUnsubscribe
// implements UNSUBSCRIBE (§6.3.7) — both mandatory IMAP4rev1 commands. This
// engine keeps no persistent per-user subscription list: folders are implicit
// (there is no folder-management backend method) and LSUB already reports every
// existing folder as subscribed. The commands therefore validate the request and
// acknowledge it without maintaining separate state, rather than answering the
// BAD "Unknown command" a client would otherwise get for a mandatory command.
func (s *Session) handleSubscribe(tag, args string) {
	s.handleSubscription(tag, args, "SUBSCRIBE")
}

func (s *Session) handleUnsubscribe(tag, args string) {
	s.handleSubscription(tag, args, "UNSUBSCRIBE")
}

func (s *Session) handleSubscription(tag, args, cmd string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}
	mailbox := unquote(strings.TrimSpace(args))
	if mailbox == "" {
		s.tagged(tag, "BAD", cmd+" requires a mailbox name")
		return
	}
	s.tagged(tag, "OK", cmd+" completed")
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
	var firstUnseen int
	for i, m := range s.messages {
		if !m.Seen {
			unread++
			if firstUnseen == 0 {
				firstUnseen = i + 1
			}
		}
	}

	s.selected = &selectedMailbox{
		name:   folder,
		total:  total,
		unread: unread,
	}

	s.send("* %d EXISTS", total)
	s.send("* %d RECENT", unread)
	// SELECT/EXAMINE UNSEEN is the first unseen sequence number, unlike
	// STATUS UNSEEN's count (RFC 3501 §§6.3.1, 6.3.10). Omit it when none
	// exists: zero is not a valid message sequence number.
	if firstUnseen != 0 {
		s.send("* OK [UNSEEN %d]", firstUnseen)
	}
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

	// Parse the complete RFC 3501 §9 STATUS grammar before touching the
	// backend. parseIMAPArgs is permissive and can discard trailing arguments.
	// Literal mailbox arguments have already been spliced as quoted strings.
	folder, rest, ok := parseAstring(args)
	if !ok || !validAstring(args[:len(args)-len(rest)]) {
		s.tagged(tag, "BAD", "Invalid STATUS mailbox name")
		return
	}
	rest, ok = cutSingleSP(rest)
	if !ok {
		s.tagged(tag, "BAD", "STATUS requires SP before the item list")
		return
	}
	items, ok := parseStatusItems(rest)
	if !ok {
		s.tagged(tag, "BAD", "STATUS requires a non-empty parenthesized item list")
		return
	}
	// INBOX is case-insensitive; other names retain their spelling.
	folder = canonicalizeInbox(folder)
	// Validate every requested item up front so an unknown one is a BAD syntax
	// error before any untagged response is emitted.
	for _, item := range items {
		switch item {
		case "MESSAGES", "RECENT", "UNSEEN", "UIDNEXT", "UIDVALIDITY":
		default:
			s.tagged(tag, "BAD", "Unknown STATUS item: "+item)
			return
		}
	}

	// STATUS must not change the selected mailbox or touch flags: read the
	// folder's messages fresh, independent of any selected-folder cache.
	messages, err := s.mailbox.Messages(folder)
	if err != nil {
		s.tagged(tag, "NO", "Failed to get status")
		return
	}

	total := len(messages)
	var unseen int
	var maxUID uint32
	for _, m := range messages {
		if !m.Seen {
			unseen++
		}
		if m.UID > maxUID {
			maxUID = m.UID
		}
	}

	// Emit exactly the requested items, in the order the client asked for them.
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			b.WriteByte(' ')
		}
		switch item {
		case "MESSAGES":
			fmt.Fprintf(&b, "MESSAGES %d", total)
		case "RECENT":
			// This engine models no \Recent flag; it reports the unseen count as
			// RECENT, consistent with the "* n RECENT" line SELECT/EXAMINE send.
			fmt.Fprintf(&b, "RECENT %d", unseen)
		case "UNSEEN":
			fmt.Fprintf(&b, "UNSEEN %d", unseen)
		case "UIDNEXT":
			// The UID the next message will be assigned: strictly greater than
			// every existing UID, i.e. highest-UID + 1 (RFC 3501 §2.3.1.1). An
			// empty mailbox has maxUID 0, yielding 1.
			fmt.Fprintf(&b, "UIDNEXT %d", maxUID+1)
		case "UIDVALIDITY":
			// The mailbox's real UIDVALIDITY when the backend supports UIDPLUS,
			// else the legacy constant 1 — matching SELECT/EXAMINE.
			fmt.Fprintf(&b, "UIDVALIDITY %d", s.resolveUIDValidity(folder))
		}
	}

	s.send(`* STATUS %s (%s)`, encodeMailboxName(folder), b.String())
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
	if !validSequenceSet(seqStr) {
		s.tagged(tag, "BAD", "Invalid sequence set")
		return
	}
	tokens, ok := parseFetchItems(parts[1])
	if !ok {
		s.tagged(tag, "BAD", "Invalid FETCH data items")
		return
	}

	// Parse sequence numbers
	seqNums := parseSequenceSet(seqStr, len(s.messages))

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]

		if err := s.fetchResponse(seq, msg, tokens); err != nil {
			s.tagged(tag, "NO", "FETCH failed")
			return
		}
	}

	s.tagged(tag, "OK", "FETCH completed")
}

// fetchResponse composes and sends a single untagged FETCH response for msg,
// answering requested data items (RFC 3501 §7.4.2) plus UID and FLAGS. It loads
// all requested data and persists any implicit \Seen update before emitting the
// response, so a backend error cannot produce a successful-looking partial
// response or phantom flags. Read-only selections never set or echo new \Seen.
func (s *Session) fetchResponse(seq int, msg Message, tokens []string) error {
	marksSeen := false
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
			// ENVELOPE's recipient/reference fields and its date come from the
			// message's own headers (RFC 3501 §7.4.2), so load the raw message.
			// This is still metadata: loadRaw never sets \Seen. A load failure
			// is reported before sending this message's response.
			r, ok := loadRaw()
			plain = append(plain, "ENVELOPE "+buildEnvelope(msg, r, ok))
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

	if rawLoaded && !rawOK {
		return errors.New("cannot load message data")
	}

	// Only advertise a new \Seen after the backend accepted it. Earlier
	// messages in a multi-message FETCH may already have succeeded; an error
	// stops this command without rolling their completed writes back.
	if marksSeen && !msg.Seen && !s.readOnly {
		if err := s.mailbox.Store(msg.UID, FlagUpdate{Seen: boolPtr(true)}); err != nil {
			return err
		}
		msg.Seen = true
		s.messages[seq-1].Seen = true
	}
	plain[1] = fmt.Sprintf("FLAGS (%s)", s.flagString(msg))

	head := fmt.Sprintf("* %d FETCH (%s", seq, strings.Join(plain, " "))
	if len(lits) == 0 {
		s.writeString(head + ")\r\n")
		return nil
	}
	s.writeString(head)
	for _, lf := range lits {
		s.writeString(fmt.Sprintf(" %s {%d}\r\n", lf.name, len(lf.payload)))
		s.writeString(lf.payload)
	}
	s.writeString(")\r\n")
	return nil
}

// ── SEARCH ────────────────────────────────────────────────────────────

type searchCriterion struct {
	kind string // "all", "seen", "unseen", "recent", "flagged", "unflagged", "deleted", "undeleted",
	// "answered", "unanswered", "draft", "undraft", "larger", "smaller",
	// "from", "to", "subject", "body", "text", "header", "since", "before", "on",
	// "sentsince", "sentbefore", "senton", "uid", "seqset", "not", "or", "and"
	size   uint64            // unsigned 32-bit SEARCH number, compared without narrowing to int
	field  string            // header: the header field name to search
	value  string            // for string/date/uid/seqset criteria
	date   time.Time         // parsed date for since/before/on
	sub    []searchCriterion // for NOT (1 element), OR (2 elements) or AND (a "(...)" group)
	ranges []seqRange        // uid/seqset: pre-parsed ranges, tested by membership (never expanded)
}

// searchError carries the tagged response that an unparseable SEARCH must
// produce instead of silently matching every message (RFC 3501 §6.4.4): status
// is "BAD" (malformed criteria) or "NO" (an unsupported CHARSET), and text is
// the response text — for BADCHARSET it begins with the "[BADCHARSET (...)]"
// response code.
type searchError struct {
	status string
	text   string
}

// badSearch builds a tagged-BAD searchError for a malformed search key.
func badSearch(text string) *searchError {
	return &searchError{status: "BAD", text: text}
}

// rawLoader returns a lazy, memoized loader of msg's raw RFC 5322 bytes for the
// SEARCH body/text keys. The backend Fetch runs at most once, and only when a
// body/text criterion actually calls it — so a metadata-only SEARCH never pulls a
// message body. A Fetch error yields ("", false), which those keys treat as no
// match.
func (s *Session) rawLoader(msg Message) func() (string, bool) {
	var raw string
	loaded, ok := false, false
	return func() (string, bool) {
		if !loaded {
			loaded = true
			if b, err := s.mailbox.Fetch(msg.UID); err == nil {
				raw, ok = string(b), true
			}
		}
		return raw, ok
	}
}

func (s *Session) handleSearch(tag, args string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}

	criteria, serr := s.parseSearchCriteria(args)
	if serr != nil {
		s.tagged(tag, serr.status, serr.text)
		return
	}

	var seqNums []string
	for i, msg := range s.messages {
		if s.matchesCriteria(msg, criteria, s.rawLoader(msg)) {
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

// searchCharsets are the character sets SEARCH accepts (RFC 3501 §6.4.4). Any
// other CHARSET is answered with a tagged NO [BADCHARSET (...)].
var searchCharsets = map[string]bool{"US-ASCII": true, "UTF-8": true}

// parseSearchCriteria tokenizes the IMAP SEARCH arguments and builds criteria.
// It returns a *searchError when the arguments are unparseable — an unknown key,
// a malformed date, unbalanced parentheses (BAD) or an unsupported CHARSET
// (NO [BADCHARSET ...]) — so the caller answers with a tagged failure instead of
// silently matching every message (RFC 3501 §6.4.4).
//
// Any UID set is parsed into ranges here — once, not once per message — so the
// per-message match is a cheap membership test (issue #8).
func (s *Session) parseSearchCriteria(args string) ([]searchCriterion, *searchError) {
	tokens := tokenizeSearch(args)

	// An optional leading "CHARSET <name>" selects the charset the string keys
	// are encoded in. Only US-ASCII and UTF-8 are supported; any other name is a
	// tagged NO [BADCHARSET (...)] rather than being ignored.
	if len(tokens) > 0 && strings.ToUpper(tokens[0]) == "CHARSET" {
		if len(tokens) < 2 {
			return nil, badSearch("CHARSET requires an argument")
		}
		name := unquote(tokens[1])
		if !searchCharsets[strings.ToUpper(name)] {
			return nil, &searchError{status: "NO", text: "[BADCHARSET (US-ASCII UTF-8)] unsupported charset " + name}
		}
		tokens = tokens[2:]
	}

	if len(tokens) == 0 {
		return nil, badSearch("SEARCH requires at least one search key")
	}

	maxUID := s.maxUID()
	var criteria []searchCriterion
	idx := 0
	for idx < len(tokens) {
		c, newIdx, err := parseSingleCriterion(tokens, idx, maxUID)
		if err != nil {
			return nil, err
		}
		criteria = append(criteria, c)
		idx = newIdx
	}

	// Resolve any bare sequence-set keys against the current message list once,
	// mapping matched sequence numbers to their UIDs so the per-message test is
	// the same O(1) membership check UID sets use (never expanded — issue #8).
	s.resolveSeqSets(criteria)
	return criteria, nil
}

// resolveSeqSets walks the criteria tree and turns every "seqset" node's raw set
// string into UID ranges, one entry per matched message. Sequence numbers are
// resolved against the live message list (bounded by the mailbox size), so a
// pathological set such as "1:4294967295" costs O(messages), not O(span).
func (s *Session) resolveSeqSets(criteria []searchCriterion) {
	for i := range criteria {
		s.resolveSeqSetNode(&criteria[i])
	}
}

func (s *Session) resolveSeqSetNode(c *searchCriterion) {
	if c.kind == "seqset" {
		total := len(s.messages)
		for _, seq := range parseSequenceSet(c.value, total) {
			if seq >= 1 && seq <= total {
				uid := s.messages[seq-1].UID
				c.ranges = append(c.ranges, seqRange{start: uid, end: uid})
			}
		}
	}
	for i := range c.sub {
		s.resolveSeqSetNode(&c.sub[i])
	}
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

// tokenizeSearch splits the search arguments into tokens, respecting quoted
// strings and emitting "(" and ")" as their own tokens so parenthesized key
// groups (RFC 3501 §6.4.4) parse correctly — "(FROM x)" yields ["(","FROM","x",")"]
// rather than the mis-split ["(FROM","x)"] that the parens-blind tokenizer produced.
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
		switch args[i] {
		case '"':
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
		case '(', ')':
			tokens = append(tokens, string(args[i]))
			i++
		default:
			// Unquoted token — runs to the next space or parenthesis.
			j := i
			for j < len(args) && args[j] != ' ' && args[j] != '(' && args[j] != ')' {
				j++
			}
			tokens = append(tokens, args[i:j])
			i = j
		}
	}
	return tokens
}

// parseSingleCriterion parses one criterion from the token list starting at idx.
// maxUID resolves "*" in a UID set to the mailbox's high-water mark. It returns a
// *searchError for a malformed key (unknown keyword, missing argument, bad date
// or unbalanced parenthesis) rather than degrading to a match-ALL criterion.
func parseSingleCriterion(tokens []string, idx int, maxUID uint32) (searchCriterion, int, *searchError) {
	if idx >= len(tokens) {
		return searchCriterion{}, idx, badSearch("missing search key")
	}

	tok := tokens[idx]
	keyword := strings.ToUpper(tok)

	switch keyword {
	case "(":
		return parseSearchGroup(tokens, idx, maxUID)
	case ")":
		return searchCriterion{}, idx, badSearch("unbalanced parentheses in SEARCH")
	case "ALL":
		return searchCriterion{kind: "all"}, idx + 1, nil
	case "ANSWERED", "UNANSWERED", "DRAFT", "UNDRAFT":
		return searchCriterion{kind: strings.ToLower(keyword)}, idx + 1, nil
	case "SEEN":
		return searchCriterion{kind: "seen"}, idx + 1, nil
	case "UNSEEN":
		return searchCriterion{kind: "unseen"}, idx + 1, nil
	case "FLAGGED":
		return searchCriterion{kind: "flagged"}, idx + 1, nil
	case "UNFLAGGED":
		return searchCriterion{kind: "unflagged"}, idx + 1, nil
	case "DELETED":
		return searchCriterion{kind: "deleted"}, idx + 1, nil
	case "UNDELETED":
		return searchCriterion{kind: "undeleted"}, idx + 1, nil
	case "RECENT":
		// \Recent has no distinct model in this engine, which reports the unseen
		// set as RECENT in SELECT/EXAMINE and STATUS; SEARCH RECENT matches that
		// same set (RFC 3501 §6.4.4).
		return searchCriterion{kind: "recent"}, idx + 1, nil
	case "FROM", "TO", "SUBJECT", "BODY", "TEXT":
		if idx+1 >= len(tokens) {
			return searchCriterion{}, idx, badSearch(keyword + " requires a string argument")
		}
		return searchCriterion{kind: strings.ToLower(keyword), value: unquote(tokens[idx+1])}, idx + 2, nil
	case "CC", "BCC":
		// CC/BCC search the message's own Cc/Bcc header fields, the source of
		// the envelope's cc and bcc members (RFC 3501 §§6.4.4, 7.4.2).
		if idx+1 >= len(tokens) {
			return searchCriterion{}, idx, badSearch(keyword + " requires a string argument")
		}
		return searchCriterion{kind: "header", field: keyword, value: unquote(tokens[idx+1])}, idx + 2, nil
	case "HEADER":
		if idx+2 >= len(tokens) {
			return searchCriterion{}, idx, badSearch("HEADER requires a field name and a string")
		}
		field := unquote(tokens[idx+1])
		if field == "" || field == "(" || field == ")" {
			return searchCriterion{}, idx, badSearch("HEADER requires a field name")
		}
		return searchCriterion{kind: "header", field: field, value: unquote(tokens[idx+2])}, idx + 3, nil
	case "LARGER", "SMALLER":
		if idx+1 >= len(tokens) {
			return searchCriterion{}, idx, badSearch(keyword + " requires a number")
		}
		// RFC 3501 §9 number is 1*DIGIT, unsigned 32-bit; ParseUint
		// rejects signs, quoted strings and overflow, but permits leading zeroes.
		n, err := strconv.ParseUint(tokens[idx+1], 10, 32)
		if err != nil {
			return searchCriterion{}, idx, badSearch("invalid " + keyword + " number")
		}
		return searchCriterion{kind: strings.ToLower(keyword), size: n}, idx + 2, nil
	case "SINCE", "BEFORE", "ON", "SENTSINCE", "SENTBEFORE", "SENTON":
		if idx+1 >= len(tokens) {
			return searchCriterion{}, idx, badSearch(keyword + " requires a date")
		}
		d, ok := parseSearchDate(unquote(tokens[idx+1]))
		if !ok {
			return searchCriterion{}, idx, badSearch("invalid " + keyword + " date: " + unquote(tokens[idx+1]))
		}
		return searchCriterion{kind: strings.ToLower(keyword), value: tokens[idx+1], date: d}, idx + 2, nil
	case "UID":
		if idx+1 >= len(tokens) || !validSequenceSet(tokens[idx+1]) {
			return searchCriterion{}, idx, badSearch("UID requires a valid sequence set")
		}
		return searchCriterion{kind: "uid", value: tokens[idx+1], ranges: parseUIDRanges(tokens[idx+1], maxUID)}, idx + 2, nil
	case "NOT":
		if idx+1 >= len(tokens) {
			return searchCriterion{}, idx, badSearch("NOT requires a search key")
		}
		sub, newIdx, err := parseSingleCriterion(tokens, idx+1, maxUID)
		if err != nil {
			return searchCriterion{}, idx, err
		}
		return searchCriterion{kind: "not", sub: []searchCriterion{sub}}, newIdx, nil
	case "OR":
		sub1, newIdx1, err := parseSingleCriterion(tokens, idx+1, maxUID)
		if err != nil {
			return searchCriterion{}, idx, err
		}
		sub2, newIdx2, err := parseSingleCriterion(tokens, newIdx1, maxUID)
		if err != nil {
			return searchCriterion{}, idx, err
		}
		return searchCriterion{kind: "or", sub: []searchCriterion{sub1, sub2}}, newIdx2, nil
	default:
		// A bare message sequence set is a valid search key (RFC 3501 §6.4.4);
		// resolve it later against the message list. Anything else is unparseable
		// and must be a tagged BAD, not a silent match-ALL.
		if validSequenceSet(tok) {
			return searchCriterion{kind: "seqset", value: tok}, idx + 1, nil
		}
		return searchCriterion{}, idx, badSearch("unknown search criterion: " + tok)
	}
}

// parseSearchGroup parses a parenthesized key group "( key key ... )", which is
// the AND of the enclosed keys (RFC 3501 §6.4.4). tokens[idx] is the "(". A
// missing closing ")" is a tagged BAD.
func parseSearchGroup(tokens []string, idx int, maxUID uint32) (searchCriterion, int, *searchError) {
	var group []searchCriterion
	j := idx + 1
	for j < len(tokens) && tokens[j] != ")" {
		sub, newIdx, err := parseSingleCriterion(tokens, j, maxUID)
		if err != nil {
			return searchCriterion{}, idx, err
		}
		group = append(group, sub)
		j = newIdx
	}
	if j >= len(tokens) {
		return searchCriterion{}, idx, badSearch("unbalanced parentheses in SEARCH")
	}
	if len(group) == 0 {
		return searchCriterion{}, idx, badSearch("SEARCH group requires at least one search key")
	}
	return searchCriterion{kind: "and", sub: group}, j + 1, nil
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

// parseSearchDate parses an IMAP SEARCH date ("1-Jan-2006" or "01-Jan-2006",
// RFC 3501 §9). ok is false when s is not a valid date (bad month, day out of
// range, non-numeric) — the caller answers BAD rather than comparing against the
// zero time, which would silently match every message.
func parseSearchDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"2-Jan-2006", "02-Jan-2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// compareSearchDate applies a SINCE, BEFORE or ON comparison (kind is
// "since", "before" or "on") of t's calendar date, as written in t's own
// location, against the SEARCH date d.
func compareSearchDate(kind string, t, d time.Time) bool {
	year, month, day := t.Date()
	date := time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
	switch kind {
	case "since":
		return !date.Before(d)
	case "before":
		return date.Before(d)
	default: // on
		return date.Equal(d)
	}
}

// matchesCriteria reports whether msg satisfies every criterion (the criteria are
// ANDed). loadRaw lazily supplies msg's raw RFC 5322 bytes for the body/text keys;
// it is called at most once per message and only when such a key is present, so a
// metadata-only search never pulls a body.
func (s *Session) matchesCriteria(msg Message, criteria []searchCriterion, loadRaw func() (string, bool)) bool {
	for _, c := range criteria {
		if !s.matchOne(msg, c, loadRaw) {
			return false
		}
	}
	return true
}

func (s *Session) matchOne(msg Message, c searchCriterion, loadRaw func() (string, bool)) bool {
	switch c.kind {
	case "all":
		return true
	case "answered":
		return msg.Answered
	case "unanswered":
		return !msg.Answered
	case "draft":
		return msg.Draft
	case "undraft":
		return !msg.Draft
	case "seen":
		return msg.Seen
	case "unseen":
		return !msg.Seen
	case "recent":
		// The engine models \Recent as the unseen set (see parseSingleCriterion),
		// matching the RECENT count reported by SELECT/EXAMINE and STATUS.
		return !msg.Seen
	case "flagged":
		return msg.Flagged
	case "unflagged":
		return !msg.Flagged
	case "deleted":
		return s.deleted[msg.UID]
	case "undeleted":
		return !s.deleted[msg.UID]
	case "larger", "smaller":
		// Size is the host's full RFC822.SIZE octet count, also used by
		// FETCH. Avoid narrowing the operand to int on 32-bit hosts.
		if c.kind == "larger" {
			return msg.Size >= 0 && uint64(msg.Size) > c.size
		}
		return msg.Size >= 0 && uint64(msg.Size) < c.size
	case "from":
		return strings.Contains(strings.ToLower(msg.From.Email), strings.ToLower(c.value))
	case "to":
		return strings.Contains(strings.ToLower(msg.To), strings.ToLower(c.value))
	case "subject":
		return strings.Contains(strings.ToLower(msg.Subject), strings.ToLower(c.value))
	case "body":
		// BODY <string>: case-insensitive substring in the message body (the text
		// after the header block), RFC 3501 §6.4.4. Missing body ⇒ no match.
		raw, ok := loadRaw()
		if !ok {
			return false
		}
		return strings.Contains(strings.ToLower(textSection(raw)), strings.ToLower(c.value))
	case "text":
		// TEXT <string>: case-insensitive substring in the header OR the body —
		// i.e. anywhere in the raw message (RFC 3501 §6.4.4). Missing raw ⇒ no match.
		raw, ok := loadRaw()
		if !ok {
			return false
		}
		return strings.Contains(strings.ToLower(raw), strings.ToLower(c.value))
	case "header":
		// HEADER <field-name> <string> (and CC/BCC): a header with that field
		// name whose text after the colon contains the string, case-insensitive;
		// an empty string matches any message that has the field (RFC 3501
		// §6.4.4). Missing raw ⇒ no match.
		raw, ok := loadRaw()
		if !ok {
			return false
		}
		needle := strings.ToLower(c.value)
		for _, v := range headerFieldValues(raw, c.field) {
			if strings.Contains(strings.ToLower(v), needle) {
				return true
			}
		}
		return false
	case "since", "before", "on":
		// RFC 3501 §6.4.4 compares the internal calendar date, ignoring
		// both the time of day and timezone. Converting the instant to UTC
		// first would move messages near midnight onto the wrong day.
		return compareSearchDate(c.kind, msg.Date, c.date)
	case "sentsince", "sentbefore", "senton":
		// The SENT keys compare the Date: header's calendar date, again
		// disregarding time and timezone (RFC 3501 §6.4.4). A message with no
		// parseable Date: header has no sent date and matches none of them.
		raw, ok := loadRaw()
		if !ok {
			return false
		}
		values := headerFieldValues(raw, "Date")
		if len(values) == 0 {
			return false
		}
		sent, err := mail.ParseDate(strings.TrimSpace(values[0]))
		if err != nil {
			return false
		}
		return compareSearchDate(strings.TrimPrefix(c.kind, "sent"), sent, c.date)
	case "uid", "seqset":
		// Test membership against the pre-parsed ranges (built once in
		// parseSearchCriteria; a seqset is resolved to its messages' UIDs). Never
		// expand the set: a crafted "1:4294967295" must not iterate billions of
		// times per message (issue #8).
		return uidInRanges(msg.UID, c.ranges)
	case "not":
		if len(c.sub) > 0 {
			return !s.matchOne(msg, c.sub[0], loadRaw)
		}
		return true
	case "or":
		if len(c.sub) >= 2 {
			return s.matchOne(msg, c.sub[0], loadRaw) || s.matchOne(msg, c.sub[1], loadRaw)
		}
		return true
	case "and":
		// A parenthesized group matches when every enclosed key matches.
		for _, sub := range c.sub {
			if !s.matchOne(msg, sub, loadRaw) {
				return false
			}
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

	if !validSequenceSet(parts[0]) {
		s.tagged(tag, "BAD", "Invalid sequence set")
		return
	}

	mode, silent, ok := parseStoreAction(parts[1])
	if !ok {
		s.tagged(tag, "BAD", "STORE requires FLAGS, +FLAGS or -FLAGS")
		return
	}

	if !validStoreFlags(parts[2]) {
		s.tagged(tag, "BAD", "Invalid flag list")
		return
	}

	seqNums := parseSequenceSet(parts[0], len(s.messages))
	flags := parseFlags(parts[2])

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		if err := s.applyStore(seq, mode, flags, silent, false); err != nil {
			s.tagged(tag, "NO", "STORE failed")
			return
		}
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
// flags. When withUID is true the FETCH also reports the UID (UID STORE). If
// the backend refuses the write, the message's session state is restored and
// the error returned, so the caller can answer NO (RFC 3501 §6.4.6).
func (s *Session) applyStore(seq int, mode storeMode, flags []string, silent, withUID bool) error {
	msg := &s.messages[seq-1]
	before, wasDeleted := *msg, s.deleted[msg.UID]
	update := s.applyStoreFlags(msg, mode, flags)

	if update.Seen != nil || update.Answered != nil || update.Flagged != nil || update.Draft != nil {
		if err := s.mailbox.Store(msg.UID, update); err != nil {
			*msg = before
			s.applyDeleted(msg.UID, storeReplace, wasDeleted)
			return err
		}
	}

	if silent {
		return nil
	}
	if withUID {
		s.send("* %d FETCH (UID %d FLAGS (%s))", seq, msg.UID, s.flagString(*msg))
	} else {
		s.send("* %d FETCH (FLAGS (%s))", seq, s.flagString(*msg))
	}
	return nil
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

	seqStr, dest, problem := parseSetMailbox(args)
	if problem != "" {
		s.tagged(tag, "BAD", problem)
		return
	}

	seqNums := parseSequenceSet(seqStr, len(s.messages))

	code, err := s.copyMessages(seqNums, dest)
	if err != nil {
		s.tagged(tag, "NO", "COPY failed")
		return
	}
	if code != "" {
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

	seqStr, dest, problem := parseSetMailbox(args)
	if problem != "" {
		s.tagged(tag, "BAD", problem)
		return
	}

	seqNums := parseSequenceSet(seqStr, len(s.messages))
	s.moveMessages(tag, "MOVE", seqNums, dest)
}

func (s *Session) handleExpunge(tag string) {
	if s.selected == nil {
		s.tagged(tag, "BAD", "EXPUNGE valid only in selected state")
		return
	}
	if s.refuseReadOnly(tag) {
		return
	}
	s.expungeMessages(tag, "EXPUNGE", nil)
}

// expungeMessages removes marked messages, optionally restricted to the given
// sequence numbers. Descending order keeps every reported sequence valid. A
// refused delete leaves the message and its mark intact for a later retry;
// successful removals are still reported before the final NO (RFC 3501 §6.4.3,
// RFC 4315 §2.1). Backends need not support an atomic multi-message deletion.
func (s *Session) expungeMessages(tag, command string, targets map[int]bool) {
	failed := false
	for i := len(s.messages) - 1; i >= 0; i-- {
		msg := s.messages[i]
		if !s.deleted[msg.UID] || (targets != nil && !targets[i+1]) {
			continue
		}
		if err := s.mailbox.Delete(msg.UID); err != nil {
			slog.Warn("imap: expunge failed", "command", command, "uid", msg.UID, "error", err)
			failed = true
			continue
		}
		s.send("* %d EXPUNGE", i+1)
		s.messages = append(s.messages[:i], s.messages[i+1:]...)
		delete(s.deleted, msg.UID)
	}

	s.selected.total = int64(len(s.messages))
	if failed {
		s.tagged(tag, "NO", command+" failed")
		return
	}
	s.tagged(tag, "OK", command+" completed")
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

// handleAppend returns true when the command stream cannot safely be reused.
func (s *Session) handleAppend(tag, args string) (closeConn bool) {
	// A refused non-synchronizing literal may already be buffered behind this
	// line. Until we commit to reading it, every refusal must end the session
	// rather than dispatching those octets as commands (RFC 7888 §§3–4).
	_, _, synchronizing, literal := splitTrailingLiteral(args)
	rejectNonSync := literal && !synchronizing
	defer func() {
		if rejectNonSync {
			s.send("* BYE APPEND rejected with unread non-synchronizing literal")
			closeConn = true
		}
	}()

	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	// APPEND mailbox [flag-list] [date-time] literal  (RFC 3501 §6.3.11). The
	// mailbox is an astring: an atom (unquoted), a quoted-string, or a literal.
	// Parse it as an astring so an unquoted name is honoured rather than ignored,
	// which previously left folder at "INBOX" and misdelivered the message.
	// Arguments are separated by exactly one SP (RFC 3501 §9): a doubled, missing
	// or leading separator is BAD before the continuation, not skipped.
	if strings.HasPrefix(args, " ") {
		s.tagged(tag, "BAD", "Invalid argument separator")
		return
	}
	folder, rest, ok := parseAstring(args)
	if !ok || folder == "" || (strings.HasPrefix(args, `"`) && !validAppendQuotedMailbox(args[:len(args)-len(rest)])) {
		s.tagged(tag, "BAD", "Invalid mailbox name")
		return
	}
	if rest, ok = cutSingleSP(rest); !ok {
		s.tagged(tag, "BAD", "Invalid argument separator")
		return
	}
	// "INBOX" is case-insensitive (RFC 3501 §5.1): fold to canonical so an any-case
	// destination delivers to the one real INBOX, not a phantom "inbox" mailbox.
	folder = canonicalizeInbox(folder)

	// Optional flag-list: (\Seen \Draft ...) immediately after the mailbox. Taking
	// it positionally — rather than scanning for the first '(' — avoids matching a
	// parenthesis inside a quoted mailbox name.
	var appendFlags []string
	if strings.HasPrefix(rest, "(") {
		// No flag contains ')', so the first one closes the list. The list must be
		// well formed and followed by SP (RFC 3501 §9); a malformed list is BAD,
		// not an unsupported flag, and is refused before the continuation.
		end := strings.IndexByte(rest, ')')
		if end < 0 || !validFlagList(rest[:end+1]) || !strings.HasPrefix(rest[end+1:], " ") {
			s.tagged(tag, "BAD", "Malformed flag list")
			return
		}
		appendFlags = parseFlags(rest[:end+1])
		if rest, ok = cutSingleSP(rest[end+1:]); !ok {
			s.tagged(tag, "BAD", "Invalid argument separator")
			return
		}
	}

	// Translate the flags into a neutral FlagUpdate, rejecting any flag the store
	// cannot honour so the client is not misled into thinking it took effect. This
	// runs before the continuation: a rejected APPEND must not read the literal.
	var flags FlagUpdate
	for _, flag := range appendFlags {
		switch strings.ToLower(flag) {
		case `\seen`:
			flags.Seen = boolPtr(true)
		case `\answered`:
			flags.Answered = boolPtr(true)
		case `\flagged`:
			flags.Flagged = boolPtr(true)
		case `\draft`:
			flags.Draft = boolPtr(true)
		case `\deleted`:
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
		if rest, ok = cutSingleSP(after); !ok {
			s.tagged(tag, "BAD", "Invalid argument separator")
			return
		}
	}

	// The terminal argument must be exactly one literal marker, with no
	// ignored arguments before it or suffix after it. Reuse the unsigned
	// literal grammar used by ordinary command arguments.
	prefix, size, sync, isLiteral := splitTrailingLiteral(rest)
	if !isLiteral || prefix != "" {
		s.tagged(tag, "BAD", "Invalid message literal")
		return
	}
	if size > 10*1024*1024 {
		s.tagged(tag, "NO", "Message too large")
		return
	}

	// All early refusals are past. Reading the payload and command terminator
	// now establishes the next command boundary, even if the backend refuses it.
	rejectNonSync = false

	// A non-synchronizing {N+} already has its octets in flight and must not
	// receive a continuation (RFC 7888 §3). Synchronizing literals wait here.
	if sync {
		s.send("+ Ready for literal data")
	}

	data := make([]byte, size)
	if _, err := io.ReadFull(s.reader, data); err != nil {
		// Incomplete commands must never reach the backend. The stream cannot
		// be reused after a partial literal or timeout.
		return true
	}

	// The command's CRLF is separate from the literal's counted octets. Do
	// not discard arbitrary trailing text or ignore EOF: neither completes a
	// valid APPEND, and a buffered next command must remain untouched.
	var terminator [2]byte
	if _, err := io.ReadFull(s.reader, terminator[:]); err != nil {
		return true
	}
	if string(terminator[:]) != "\r\n" {
		s.tagged(tag, "BAD", "Expected CRLF after message literal")
		return true
	}
	// RFC 3501 §9 literal payloads are CHAR8 (%x01-ff). After the counted
	// octets and terminator have been consumed, this refusal leaves the next
	// command boundary intact, including for a non-synchronizing literal.
	if bytes.IndexByte(data, 0) >= 0 {
		s.tagged(tag, "BAD", "NUL in message literal")
		return false
	}

	var err error
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

	// RFC 3501 §6.3.11: when the message is appended to the mailbox this session
	// currently has selected, notify the session of the new message count with an
	// untagged EXISTS (and refresh the cached message set). Appends to any other
	// mailbox are invisible to the current selection. This runs before the tagged
	// completion — including the APPENDUID branch below — so the EXISTS precedes
	// the OK, as untagged responses must (RFC 3501 §7).
	s.notifyAppendToSelected(folder)

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
	return
}

// notifyAppendToSelected sends an untagged "* n EXISTS" (RFC 3501 §6.3.11) when a
// just-completed APPEND targeted the mailbox this session has selected, so the
// client learns a message arrived. It re-reads the mailbox so the cached message
// slice — which backs sequence-number resolution for later FETCH/STORE/SEARCH —
// reflects the new message rather than going stale. folder is already canonical
// (INBOX-folded), as is s.selected.name, so the comparison is exact. A read error
// leaves the prior cache and count in place; the next SELECT or IDLE poll
// reconciles.
func (s *Session) notifyAppendToSelected(folder string) {
	if s.selected == nil || s.selected.name != folder {
		return
	}
	messages, err := s.mailbox.Messages(folder)
	if err != nil {
		return
	}
	s.messages = messages
	s.selected.total = int64(len(messages))
	s.send("* %d EXISTS", len(messages))
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

	s.send("* QUOTAROOT %s \"\"", encodeMailboxName(mailbox))
	s.send("* QUOTA \"\" (STORAGE %d %d)", used/1024, limit/1024)
	s.tagged(tag, "OK", "GETQUOTAROOT completed")
}

// idlePollInterval is how often the IDLE poll goroutine re-reads the selected
// mailbox to announce new messages. It is a var, not a const, only so tests can
// shorten it; production always uses the default.
var idlePollInterval = 15 * time.Second

// handleIdle runs an IDLE command (RFC 2177) until the client sends DONE, the
// peer disconnects, or the inactivity autologout timer fires. It reports whether
// the caller must terminate the session: true when the read ended in error (an
// autologout or a peer disconnect) rather than a clean DONE. On an autologout it
// emits the untagged "* BYE" (RFC 3501 §5.4/§7.1.5) before returning so the
// caller's deferred Close is preceded by the announcement — the same contract the
// main command loop honours.
func (s *Session) handleIdle(tag string) (terminate bool) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return false
	}
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return false
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
				s.reconcileIdle(messages)
			}
		}
	}()

	// Wait for DONE from client, bounded by the same inactivity autologout timer
	// the main loop uses (RFC 3501 §5.4).
	_ = s.conn.SetDeadline(time.Now().Add(autologoutTimeout))
	var readErr error
	for {
		line, tooLong, err := s.readLine()
		if err != nil {
			readErr = err
			break
		}
		if tooLong {
			continue // ignore over-long junk; keep waiting for DONE
		}
		if strings.ToUpper(line) == "DONE" {
			break
		}
	}

	// The poll goroutine owns s.writer during IDLE; stop it fully before this
	// goroutine writes anything (the tagged OK or the autologout BYE).
	close(done)
	<-stopped

	if readErr != nil {
		// The read ended in error rather than a DONE. On an inactivity autologout
		// announce the close with an untagged BYE (RFC 3501 §5.4/§7.1.5) before the
		// caller closes the connection; a peer disconnect can receive nothing. Tell
		// the caller to terminate the session either way.
		if isAutologout(readErr) {
			slog.Info("imap: autologout on inactivity during IDLE", "remote", s.conn.RemoteAddr())
			s.sendAutologoutBye()
		} else {
			slog.Debug("imap: connection closed during IDLE", "remote", s.conn.RemoteAddr(), "error", readErr)
		}
		return true
	}

	s.tagged(tag, "OK", "IDLE terminated")
	return false
}

// reconcileIdle diffs a freshly-read message set against the cached one and
// pushes the untagged responses RFC 2177 requires the server to volunteer while
// a client is idling (RFC 3501 §7): "* n EXPUNGE" for messages removed from the
// mailbox by another session, "* n EXISTS" for new arrivals, and
// "* n FETCH (FLAGS ...)" when a surviving message's flags changed. The pre-fix
// poll only ever announced growth, so an external expunge was never reported and
// the cached message set (which backs sequence-number resolution for every later
// command) went stale.
//
// It runs solely in the IDLE poll goroutine, which owns s.writer, s.messages and
// s.selected for the duration of IDLE — the main goroutine only reads DONE and
// does not touch them until the poll goroutine has stopped — so these mutations
// need no further synchronisation.
func (s *Session) reconcileIdle(messages []Message) {
	if s.selected == nil {
		return
	}

	// Index the fresh set by UID for membership tests and flag comparison. UIDs
	// are stable and ascend with arrival order (see Message.UID), so new messages
	// only ever append after the existing ones.
	fresh := make(map[uint32]Message, len(messages))
	for _, m := range messages {
		fresh[m.UID] = m
	}

	// 1) EXPUNGE removed messages. Walk the cached set highest-sequence-first so
	// each "* n EXPUNGE" carries the sequence number the message still holds at the
	// instant it is removed; lower sequence numbers are unaffected by the splice
	// (RFC 3501 §7.4.1 renumbering rule).
	survivors := append([]Message(nil), s.messages...)
	for i := len(survivors) - 1; i >= 0; i-- {
		if _, ok := fresh[survivors[i].UID]; !ok {
			s.send("* %d EXPUNGE", i+1)
			survivors = append(survivors[:i], survivors[i+1:]...)
		}
	}

	// 2) FLAGS changes on messages that survived. Their sequence numbers are the
	// post-expunge indices; arrivals reported below only append, so they do not
	// shift these.
	for i, old := range survivors {
		if nm := fresh[old.UID]; buildFlags(nm) != buildFlags(old) {
			s.send("* %d FETCH (FLAGS (%s))", i+1, s.flagString(nm))
		}
	}

	// 3) EXISTS for new arrivals — any growth beyond the surviving count. An
	// expunge on its own never triggers EXISTS: the EXPUNGE responses already
	// convey the smaller mailbox.
	if len(messages) > len(survivors) {
		s.send("* %d EXISTS", len(messages))
	}

	// Replace the cache with the authoritative fresh set.
	s.messages = messages
	s.selected.total = int64(len(messages))
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
	if !validSequenceSet(uidSetStr) {
		s.tagged(tag, "BAD", "Invalid uid set")
		return
	}
	tokens, ok := parseFetchItems(parts[1])
	if !ok {
		s.tagged(tag, "BAD", "Invalid FETCH data items")
		return
	}

	seqNums := s.parseUIDSet(uidSetStr)

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]

		if err := s.fetchResponse(seq, msg, tokens); err != nil {
			s.tagged(tag, "NO", "UID FETCH failed")
			return
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

	if !validSequenceSet(parts[0]) {
		s.tagged(tag, "BAD", "Invalid uid set")
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
	if !validStoreFlags(flagParts[1]) {
		s.tagged(tag, "BAD", "Invalid flag list")
		return
	}
	flags := parseFlags(flagParts[1])

	seqNums := s.parseUIDSet(parts[0])
	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		if err := s.applyStore(seq, mode, flags, silent, true); err != nil {
			s.tagged(tag, "NO", "UID STORE failed")
			return
		}
	}

	s.tagged(tag, "OK", "UID STORE completed")
}

func (s *Session) handleUIDCopy(tag, args string) {
	uidSetStr, dest, problem := parseSetMailbox(args)
	if problem != "" {
		s.tagged(tag, "BAD", problem)
		return
	}

	seqNums := s.parseUIDSet(uidSetStr)
	code, err := s.copyMessages(seqNums, dest)
	if err != nil {
		s.tagged(tag, "NO", "UID COPY failed")
		return
	}
	if code != "" {
		s.tagged(tag, "OK", "["+code+"] UID COPY completed")
		return
	}
	s.tagged(tag, "OK", "UID COPY completed")
}

func (s *Session) handleUIDMove(tag, args string) {
	uidSetStr, dest, problem := parseSetMailbox(args)
	if problem != "" {
		s.tagged(tag, "BAD", problem)
		return
	}

	seqNums := s.parseUIDSet(uidSetStr)
	s.moveMessages(tag, "UID MOVE", seqNums, dest)
}

func (s *Session) handleUIDSearch(tag, args string) {
	// UID SEARCH returns UIDs instead of sequence numbers
	// Parse the criteria the same way as SEARCH
	criteria, serr := s.parseSearchCriteria(args)
	if serr != nil {
		s.tagged(tag, serr.status, serr.text)
		return
	}

	var uids []string
	for _, msg := range s.messages {
		if s.matchesCriteria(msg, criteria, s.rawLoader(msg)) {
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
