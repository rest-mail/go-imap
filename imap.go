// Package imap implements an IMAP4rev1 ([RFC 3501]) server engine with zero
// external dependencies (standard library only).
//
// You supply a [Backend] that authenticates users and returns a [Mailbox] view
// over their folders and messages, expressed as neutral [Message] values; the
// [Server] speaks the wire protocol. The engine holds no assumptions about where
// mail lives — a Backend can be a database, a maildir, or a remote API.
//
// # Backend and Mailbox
//
// [Backend] has a single method, Authenticate, which a [Server] calls once per
// session when the client issues LOGIN or AUTHENTICATE. It returns a [Mailbox]:
// the authenticated account's view of its mail, every method scoped to that user.
// The Server caches the slice [Mailbox.Messages] returns for the selected folder
// and derives sequence numbers, UIDs, flags, ENVELOPE and SEARCH results from it,
// calling [Mailbox.Fetch] only when a body is requested — and FETCH BODY[] then
// serves whatever Fetch returns, byte-for-byte. Folders are implicit: there is no
// folder-management method, so a folder exists once a message names it, and
// deleting or renaming one is composed from [Mailbox.Messages] and [Mailbox.Move].
//
// The commands the engine answers include CAPABILITY, STARTTLS, LOGIN,
// AUTHENTICATE PLAIN, LIST/LSUB, SELECT/EXAMINE, STATUS, FETCH, UID FETCH,
// SEARCH, STORE, COPY, MOVE, EXPUNGE, UID EXPUNGE, CLOSE, UNSELECT ([RFC 3691]),
// ENABLE ([RFC 5161]), APPEND, IDLE ([RFC 2177]) and QUOTA ([RFC 2087]).
//
// # Optional extensions
//
// Extensions that need more from the store are opt-in through optional interfaces
// the Server type-asserts on the concrete [Mailbox] after authentication. A
// Mailbox that also implements [UIDPlusMailbox] turns on UIDPLUS ([RFC 4315]) —
// APPENDUID/COPYUID response codes, the real UIDVALIDITY in SELECT/EXAMINE, and
// UID EXPUNGE — and one that implements [Mover] turns the baseline MOVE
// ([RFC 6851]) into a single atomic backend operation. The advertised CAPABILITY
// list reflects only the optional interfaces the concrete Mailbox satisfies, so a
// backend implementing just [Mailbox] is unaffected and the list never
// over-promises.
//
// # TLS and authentication
//
// A non-nil *tls.Config enables both STARTTLS on the cleartext port and an
// implicit-TLS listener. When TLS is configured, LOGIN is refused on a cleartext
// connection until the client issues STARTTLS, and AUTH=PLAIN is advertised only
// once the connection is protected; with no TLS config at all the server offers
// AUTH=PLAIN in the clear, for development. LOGIN and AUTHENTICATE PLAIN share one
// credential check against [Backend.Authenticate], and each failure is reported to
// the [Limiter] — the per-IP connection and auth-failure guard the Server
// consults. Pass [NopLimiter] (or nil) to impose no limits.
//
// # Running a server
//
// [NewServer] builds a Server; [Server.ListenAndServe] opens the configured
// [Ports] and serves in the background until [Server.Shutdown]. To drive a single
// already-accepted connection behind your own listener, construct a [Session] with
// [NewSession] and call [Session.Handle].
//
// [RFC 3501]: https://www.rfc-editor.org/rfc/rfc3501
// [RFC 2087]: https://www.rfc-editor.org/rfc/rfc2087
// [RFC 2177]: https://www.rfc-editor.org/rfc/rfc2177
// [RFC 3691]: https://www.rfc-editor.org/rfc/rfc3691
// [RFC 4315]: https://www.rfc-editor.org/rfc/rfc4315
// [RFC 5161]: https://www.rfc-editor.org/rfc/rfc5161
// [RFC 6851]: https://www.rfc-editor.org/rfc/rfc6851
package imap

import "time"

// Address is a parsed mail address for ENVELOPE and SEARCH.
type Address struct {
	Name  string // display name, e.g. "Alice"
	Email string // addr-spec, e.g. "alice@example.com"
}

// Message is one message in a mailbox, as IMAP needs to see it. A Server caches
// the slice [Mailbox.Messages] returns for the selected folder and derives
// sequence numbers (position+1), UIDs, flags, ENVELOPE and SEARCH results from
// it, calling [Mailbox.Fetch] only when a body is requested.
type Message struct {
	// UID is the IMAP unique identifier. It must be a positive, stable value that
	// ascends with arrival order within the mailbox. The Server also passes it to
	// the Backend to name this message.
	UID uint32
	// Size is RFC822.SIZE, the octet count reported in FETCH responses.
	Size int
	// Seen, Flagged and Draft are the persistent flags IMAP reports as \Seen,
	// \Flagged and \Draft.
	Seen    bool
	Flagged bool
	Draft   bool
	// Subject, From and Date populate ENVELOPE and INTERNALDATE, and back the
	// SUBJECT/FROM/SINCE/BEFORE/ON SEARCH keys.
	Subject string
	From    Address
	Date    time.Time
	// To is the message's recipient list as a single string; SEARCH TO does a
	// case-insensitive substring match against it.
	To string
}

// Folder is one mailbox folder reported by LIST/LSUB.
type Folder struct {
	Name string
}

// FlagUpdate is a change to a message's persistent flags. A nil field leaves that
// flag unchanged; a non-nil field sets it to the pointed-to value.
type FlagUpdate struct {
	Seen    *bool
	Flagged *bool
	Draft   *bool
}

// Backend authenticates IMAP users. A [Server] calls Authenticate once per
// session, when the client issues LOGIN or AUTHENTICATE.
type Backend interface {
	// Authenticate validates the credentials. A non-nil error rejects the login
	// (counting as an auth failure against the [Limiter]); on success it returns
	// the [Mailbox] the session operates on.
	Authenticate(user, pass string) (Mailbox, error)
}

// Mailbox is a single authenticated account's view of its mail. Every method is
// scoped to the user that [Backend.Authenticate] accepted; a Mailbox is used by
// one session, though [Mailbox.Messages] may be called concurrently by the IDLE
// poller while the client is idling.
type Mailbox interface {
	// Folders lists the account's folders (LIST/LSUB).
	Folders() ([]Folder, error)
	// Messages returns a folder's messages oldest-first (SELECT, STATUS, IDLE).
	Messages(folder string) ([]Message, error)
	// Fetch returns the full RFC 5322 bytes of a message by UID (FETCH BODY[]).
	Fetch(uid uint32) ([]byte, error)
	// Store applies a persistent flag change to a message (STORE, and the
	// auto-\Seen a non-peek BODY[] fetch triggers).
	Store(uid uint32, f FlagUpdate) error
	// Move relocates a message to another folder (MOVE; also how deleting or
	// renaming a folder relocates its messages).
	Move(uid uint32, dest string) error
	// Delete permanently removes a message (EXPUNGE, CLOSE).
	Delete(uid uint32) error
	// Copy duplicates a message into dest (COPY).
	Copy(uid uint32, dest string) error
	// Append delivers raw RFC 5322 bytes into dest with the given flags (APPEND).
	Append(dest string, f FlagUpdate, raw []byte) error
	// Quota returns storage use and limit in bytes (GETQUOTA/GETQUOTAROOT).
	Quota() (used, limit int64, err error)
}

// UIDPlusMailbox is the optional interface a [Mailbox] may also implement to
// enable the UIDPLUS extension ([RFC 4315]). When the authenticated Mailbox
// satisfies it, the [Server] advertises the UIDPLUS capability, reports the real
// UIDVALIDITY in SELECT/EXAMINE, emits APPENDUID and COPYUID response codes on
// APPEND / COPY / MOVE, and honours UID EXPUNGE. A Mailbox that does not
// implement it keeps exact pre-UIDPLUS behaviour: the capability is not
// advertised and no response codes are sent.
//
// The three methods mirror the base [Mailbox.Append] and [Mailbox.Copy] but
// return the UID the store assigned, which the resp-codes require; a base
// implementation can simply call its UID-returning form and discard the result.
//
// [RFC 4315]: https://www.rfc-editor.org/rfc/rfc4315
type UIDPlusMailbox interface {
	// UIDValidity returns the UIDVALIDITY of folder — the value SELECT/EXAMINE
	// reports and that scopes the UIDs quoted in APPENDUID and COPYUID. A zero
	// value or error makes the Server fall back to reporting UIDVALIDITY 1 and
	// omit the affected response code.
	UIDValidity(folder string) (uint32, error)
	// AppendUID delivers raw into dest exactly like [Mailbox.Append] and returns
	// the UID assigned to the stored message, for the APPENDUID response code.
	AppendUID(dest string, f FlagUpdate, raw []byte) (uid uint32, err error)
	// CopyUID duplicates the message named by srcUID into dest exactly like
	// [Mailbox.Copy] and returns the UID assigned to the copy, for COPYUID.
	CopyUID(srcUID uint32, dest string) (uid uint32, err error)
}

// Mover is the optional interface a [Mailbox] may also implement to perform an
// atomic server-side MOVE ([RFC 6851]). Without it the [Server] still supports
// MOVE and UID MOVE by calling [Mailbox.Move] once per message; with it the move
// runs as a single backend operation and, when the Mailbox also implements
// [UIDPlusMailbox], the returned destination UID is reported in a COPYUID
// response code. The MOVE capability is advertised for every backend either way.
//
// [RFC 6851]: https://www.rfc-editor.org/rfc/rfc6851
type Mover interface {
	// MoveUID relocates the message named by srcUID to dest as one atomic
	// operation and returns the UID assigned to it in dest, for COPYUID. A
	// backend that cannot report the new UID may return 0.
	MoveUID(srcUID uint32, dest string) (uid uint32, err error)
}

// Limiter is the per-IP connection and authentication guard the [Server]
// consults. It is a structural interface: any type with these methods satisfies
// it. Pass [NopLimiter] (or nil) to impose no limits.
type Limiter interface {
	// Accept reports whether a new connection from ip may proceed, incrementing
	// the in-use count when it returns true.
	Accept(ip string) bool
	// Release decrements the in-use count for ip when a connection ends.
	Release(ip string)
	// RecordAuthFail records an authentication failure from ip.
	RecordAuthFail(ip string)
	// IsBanned reports whether ip is currently banned.
	IsBanned(ip string) bool
	// ResetAuth clears the recorded auth-failure history for ip after a success.
	ResetAuth(ip string)
}

// NopLimiter is a [Limiter] that imposes no limits: it accepts every connection
// and never bans. It is the default when a nil Limiter is passed to [NewServer]
// or [NewSession].
type NopLimiter struct{}

func (NopLimiter) Accept(string) bool    { return true }
func (NopLimiter) Release(string)        {}
func (NopLimiter) RecordAuthFail(string) {}
func (NopLimiter) IsBanned(string) bool  { return false }
func (NopLimiter) ResetAuth(string)      {}
