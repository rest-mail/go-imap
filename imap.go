// Package imap implements an IMAP4rev1 ([RFC 3501]) server engine with zero
// external dependencies (standard library only).
//
// A caller supplies a [Backend] that authenticates users and returns a [Mailbox]
// view over their folders and messages, expressed as neutral [Message] values;
// the [Server] speaks the wire protocol — CAPABILITY, STARTTLS, LOGIN,
// AUTHENTICATE PLAIN, LIST/LSUB, SELECT/EXAMINE, STATUS, FETCH, UID FETCH,
// SEARCH, STORE, COPY, MOVE, EXPUNGE, APPEND, IDLE, QUOTA and more. The engine
// holds no assumptions about where mail lives: a Backend can be a database, a
// maildir, or a remote API.
//
// Message bodies served by FETCH BODY[] are whatever [Mailbox.Fetch] returns,
// byte-for-byte. Higher-level folder operations (deleting or renaming a folder)
// are composed from [Mailbox.Messages] and [Mailbox.Move]; there is no
// folder-management method on the Backend, matching the "folders are implicit"
// model where a folder exists once a message names it.
//
// [RFC 3501]: https://www.rfc-editor.org/rfc/rfc3501
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
