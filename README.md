# go-imap

[![CI](https://github.com/rest-mail/go-imap/actions/workflows/ci.yml/badge.svg)](https://github.com/rest-mail/go-imap/actions/workflows/ci.yml)
[![Coverage gate: >=95%](https://img.shields.io/badge/coverage%20gate-%3E%3D95%25-brightgreen)](COVERAGE.md)
[![Partial checklist: 284/332 mapped](https://img.shields.io/badge/partial%20checklist-284%2F332%20mapped-blue)](CHECKLIST.md)
[![Go Reference](https://pkg.go.dev/badge/github.com/rest-mail/go-imap.svg)](https://pkg.go.dev/github.com/rest-mail/go-imap)
[![Go Report Card](https://goreportcard.com/badge/github.com/rest-mail/go-imap)](https://goreportcard.com/report/github.com/rest-mail/go-imap)

An IMAP4rev1 ([RFC 3501](https://www.rfc-editor.org/rfc/rfc3501)) server engine
for Go — standard library only, no external dependencies.

## About

You supply a `Backend` that authenticates users and returns a `Mailbox` view
over their folders and messages, expressed as neutral `Message` values; the
`Server` speaks the wire protocol. The engine holds no assumptions about where
mail lives — a `Backend` can be a database, a maildir, or a remote API.

The store surface is deliberately small. `Backend` has one method,
`Authenticate`; the `Mailbox` it returns lists folders, returns a folder's
messages, and fetches, stores, moves, copies, deletes and appends by UID. The
server caches the message slice for the selected folder and derives sequence
numbers, flags and `SEARCH` results from it. It calls `Fetch` for raw content,
including `ENVELOPE`, whose fields come from the stored message headers. Folders are implicit — there is no folder-management
method, so a folder exists once a message names it.

Richer extensions light up only when the concrete backend opts in through the
optional interfaces below, and the advertised `CAPABILITY` list is derived from
what your type actually implements, so it never over-promises.

## Features

- IMAP4rev1 server engine (RFC 3501) driven by a `Backend` you implement;
  store-agnostic, built on neutral `Message` values.
- `STARTTLS` and implicit TLS; `LOGIN` and `AUTHENTICATE PLAIN`, with plaintext
  authentication withheld until the connection is protected.
- `SELECT`/`EXAMINE`, `FETCH` and `UID FETCH` (`BODY[]`, `BODY.PEEK[]`,
  `BODY[HEADER]`, `BODY[TEXT]`, `FLAGS`, `ENVELOPE`, `INTERNALDATE`,
  `RFC822.SIZE`, `UID`), `SEARCH`, `STORE`, `COPY`, `EXPUNGE` and `APPEND`.
- `BODY.PEEK[]` never sets `\Seen`; a plain `BODY[]` fetch does (RFC 3501
  §6.4.5). UID ranges expand across non-contiguous UIDs and `UID FETCH *`
  resolves to the newest message.
- `IDLE` (RFC 2177), with the poll goroutine fully stopped before each tagged
  completion so responses never interleave, and `QUOTA` (RFC 2087).
- `UNSELECT` (RFC 3691) and `ENABLE` (RFC 5161) baseline commands.
- Optional `UIDPLUS` (RFC 4315) via `UIDPlusMailbox` and atomic server-side
  `MOVE` (RFC 6851) via `Mover` — advertised only when your `Mailbox`
  implements them.
- Optional `UTF8=ACCEPT` (RFC 6855) via `UTF8Mailbox`: advertised after
  login and switched on with `ENABLE UTF8=ACCEPT`, so the backend can serve
  raw UTF-8 headers (RFC 6532) instead of a downgraded form.
- Pluggable per-IP `Limiter` for connection caps and auth-failure bans
  (`NopLimiter` for none).
- Zero external dependencies.

## Install

```sh
go get github.com/rest-mail/go-imap
```

## Quickstart

Implement `Backend` and `Mailbox`, then hand the server a TLS config and a set of
ports. The `account` type below is a sketch — fill in the method bodies against
your own store.

```go
package main

import (
	"crypto/tls"

	"github.com/rest-mail/go-imap"
)

// store is your mail store. Authenticate returns a Mailbox scoped to the user.
type store struct{ /* db handle, etc. */ }

func (s *store) Authenticate(user, pass string) (imap.Mailbox, error) {
	// verify credentials, then return the user's view
	return &account{ /* ... */ }, nil
}

type account struct{ /* ... */ }

func (a *account) Folders() ([]imap.Folder, error) {
	return []imap.Folder{{Name: "INBOX"}, {Name: "Sent"}, {Name: "Trash"}}, nil
}

func (a *account) Messages(folder string) ([]imap.Message, error) {
	// oldest-first; UID stable and ascending with arrival
	return []imap.Message{
		{UID: 101, Size: 4213, Subject: "hello",
			From: imap.Address{Name: "Alice", Email: "alice@example.com"}},
	}, nil
}

func (a *account) Fetch(uid uint32) ([]byte, error)                        { /* full RFC 5322 bytes */ return nil, nil }
func (a *account) Store(uid uint32, f imap.FlagUpdate) error               { /* \Seen/\Flagged changes */ return nil }
func (a *account) Move(uid uint32, dest string) error                      { return nil }
func (a *account) Delete(uid uint32) error                                 { /* EXPUNGE */ return nil }
func (a *account) Copy(uid uint32, dest string) error                      { return nil }
func (a *account) Append(dest string, f imap.FlagUpdate, raw []byte) error { return nil }
func (a *account) Quota() (used, limit int64, err error)                   { return 0, 0, nil }

func main() {
	cert, _ := tls.LoadX509KeyPair("cert.pem", "key.pem")
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

	// nil Limiter -> imap.NopLimiter (no per-IP limits).
	srv := imap.NewServer("mail.example.com", &store{}, tlsConfig, nil)
	if err := srv.ListenAndServe(imap.Ports{IMAP: 143, IMAPTLS: 993}); err != nil {
		panic(err)
	}
	select {} // serve until srv.Shutdown(ctx) or srv.Close()
}
```

For a single accepted connection (e.g. behind your own listener), construct a
session directly with `imap.NewSession(conn, backend, hostname, tlsConfig,
limiter)` and call `Handle()`. A runnable, self-contained version of this wiring
— driving one session over an in-memory pipe — is in
[`example_test.go`](example_test.go).

## Implementing optional extensions

Richer extensions are opt-in through **optional interfaces** the server
type-asserts on your `Mailbox` at runtime. You never have to implement them: a
`Mailbox` that satisfies only the base interface keeps its exact behaviour, and
the server advertises neither `UIDPLUS` nor a `COPYUID`-bearing `MOVE`. Implement
an interface and the matching capability and response codes turn on automatically.

- **`UIDPlusMailbox`** enables `UIDPLUS` (RFC 4315). It adds `UIDValidity`,
  `AppendUID` and `CopyUID` — the UID-returning forms of `Append`/`Copy` — so the
  server can emit `[APPENDUID uidvalidity uid]` on `APPEND`, `[COPYUID …]` on
  `COPY`/`MOVE`, report the real `UIDVALIDITY` in `SELECT`, and honour
  `UID EXPUNGE`.
- **`Mover`** makes `MOVE`/`UID MOVE` (RFC 6851) a single atomic backend
  operation via `MoveUID`. `MOVE` is a baseline capability either way — without
  `Mover` it works by calling `Mailbox.Move` per message; with it (plus
  `UIDPlusMailbox`) it reports the destination UID in a `COPYUID` response code.

```go
// Opt in by adding methods to your existing Mailbox type — no base changes.
func (a *account) UIDValidity(folder string) (uint32, error)                            { /* per-folder UIDVALIDITY */ return 1, nil }
func (a *account) AppendUID(dest string, f imap.FlagUpdate, raw []byte) (uint32, error) { /* store, return new UID */ return 0, nil }
func (a *account) CopyUID(srcUID uint32, dest string) (uint32, error)                   { /* copy, return new UID */ return 0, nil }
func (a *account) MoveUID(srcUID uint32, dest string) (uint32, error)                   { /* atomic move, return new UID */ return 0, nil }
```

`CONDSTORE`/`QRESYNC` (RFC 7162) are not implemented. `ENABLE` ignores unknown
or non-enable-able capabilities and enables `UTF8=ACCEPT` when the backend
implements `UTF8Mailbox`. Every successful ENABLE sends an `ENABLED` response,
even if its list is empty. Malformed lists receive BAD before any mode change.
`UNSELECT` returns BAD if no mailbox is selected; UNSELECT and IDLE reject
extra arguments while preserving the selection.

## API highlights

- `NewServer(hostname, backend, tlsConfig, limiter) *Server` and
  `(*Server).ListenAndServe(Ports)` — run the listeners; `(*Server).Shutdown(ctx)`
  stops accepting and drains in-flight sessions (or returns `ctx.Err()`), while
  `(*Server).Close()` is the immediate hard stop that force-closes live
  connections — the same split as `net/http.Server`.
- `NewSession(conn, backend, hostname, tlsConfig, limiter) *Session` and
  `(*Session).Handle()` — drive one already-accepted connection.
- `Backend` and `Mailbox` — the seam you implement; `Message`, `Address`,
  `Folder` and `FlagUpdate` are the neutral value types they exchange.
- `UIDPlusMailbox` and `Mover` — the optional interfaces that gate `UIDPLUS` and
  atomic `MOVE`.
- `Limiter` (with `NopLimiter`) — the per-IP connection and auth-failure guard;
  a small structural interface (`Accept`/`Release`/`RecordAuthFail`/`IsBanned`/
  `ResetAuth`) any type can satisfy.

## RFC conformance

The current checklist has 332 requirements: 284 mapped to wire tests and 48
explicit gaps or backend/deployment responsibilities. A backend that returns
`ErrNoSuchMailbox` for a missing destination makes APPEND, COPY and MOVE answer
`NO [TRYCREATE]`; other backend errors stay a plain NO. A connection the `Limiter`
refuses is greeted with an untagged BYE (RFC 3501 §7.1.5) before it closes. Twenty rows come from a
line-by-line pass over RFC 3501 §6.5 and §7 server responses (status responses
and codes, BYE, continuation, the EXPUNGE timing rule). Twenty-two rows come from a
line-by-line pass over RFC 3501 §§6.1, 6.2 (AUTHENTICATE has its own rows),
6.3.1, 6.3.2, 6.3.10, 6.3.11 and 6.4.1–6.4.3, 6.4.7–6.4.8; LIST now honours its
reference argument. Twenty-two earlier rows come from §§6.3.3–6.3.9 (CREATE,
DELETE, RENAME, SUBSCRIBE, UNSUBSCRIBE, LIST, LSUB). Folders are implicit, so
permanent DELETE, subscription storage and RENAME INBOX are
recorded gaps. Sixteen earlier rows cover
APPENDUID and ordinary LIST special-use attributes, including explicit boundaries
for unsupported extended LIST and role assignment. Sent, Drafts, Trash and Junk
roles are inferred from those exact folder names; the host owns their contents.
NAMESPACE and ID are not implemented. Thirteen earlier rows prove
ENABLE, UNSELECT and IDLE command exchanges, syntax refusal and state recovery.
Five formerly unmapped rows now cover composed APPEND/FETCH grammar, address groups, malformed headers
and malformed nested envelopes. APPEND accepts empty quoted and counted mailbox
strings as syntax and passes them unchanged to the backend for naming policy.

[CHECKLIST.md](CHECKLIST.md) is a **partial**, CI-checked list of RFC 3501
and supported-extension requirements, each mapped to a wire test in `internal/rfctest` that drives a
public `Session`. The selection-state slice includes the RFC SELECT/EXAMINE
examples and records Recent, persistent UID metadata and permission gaps.
SELECT/EXAMINE require one mailbox astring; malformed or extra arguments get BAD
before backend access and leave no mailbox selected. Checked tests cover exact
quoted/literal name decoding, significant spaces, empty strings, INBOX folding,
command framing and recovery without stale deletion marks. Literal and raw
quoted octets share the parser representation, so strict wire-form validation
remains unaudited. The STORE slice includes the RFC 3501 §6.4.6 example and
records the keyword gap.
STORE and UID STORE reject malformed sequence sets with BAD before writing flags
or changing session state, including .SILENT and empty mailboxes. Wire tests
check reversed/star/32-bit boundary sets, sparse UIDs and recovery after rejection.
A malformed flag operand is also refused with BAD before any change; out-of-range
message sequence numbers and overlapping UID-set semantics remain unaudited.
The STATUS slice checks the §6.3.10 example, requested metadata, mailbox-name
astrings and preservation of the selected mailbox and read-only state. Malformed
STATUS arguments get BAD before metadata reads, including trailing text and
invalid list separators. Independent Recent tracking and persistent UIDNEXT
remain explicit gaps; literal and raw quoted input share the existing parser
representation, so this is not a complete wire-form or mailbox-name audit.
The SEARCH slice checks Boolean/UID criteria, calendar dates, charsets and
errors against wire responses, including the first two §6.4.4 examples. Both
forms reject malformed bare sequence sets and UID operands before evaluating
content, including in Boolean/group keys and empty mailboxes; valid reversed,
star, overlapping and unsigned-32-bit boundary sets retain membership semantics.
ANSWERED/UNANSWERED and DRAFT/UNDRAFT search current system flags, including
STORE/UID STORE changes and read-only selections. LARGER/SMALLER compare the
host-supplied RFC822.SIZE octet count strictly, accept unsigned 32-bit numbers
(including zero and leading zeroes), and reject malformed operands before
evaluation. Size-only searches do not read content or change flags. Other missing
standard keys, MIME decoding and independent Recent search remain gaps. Quoted
search strings handle escaped quotes/backslashes without splitting the value;
malformed quote framing and escapes fail before content evaluation. Checked
literal tests preserve UTF-8, CRLF and quoted-specials in synchronizing and
LITERAL+ arguments. A grammar-based quote-token fuzz target runs in CI. The
shared representation still prevents a complete wire quoted-character audit;
bare astring and separator grammar also remain gaps.
The FETCH slice checks literal octets, header subsets, partial ranges, RFC822
aliases, metadata/macros, UID replies, backend refusal and read-only Seen flags,
including the §§6.4.5/6.4.8 examples. FETCH and UID FETCH reject malformed
sequence sets with BAD before reading content or changing Seen, while accepting
reversed ranges, star and 32-bit bounds. BODY/BODY.PEEK partial ranges require
`<origin.count>` with an unsigned 32-bit origin and a nonzero 32-bit count
(without leading zeroes in the count). Malformed ranges receive BAD before any
item is dispatched, including for empty/nonmatching sets. Checked wire tests
cover boundaries, recovery and Seen behavior; a grammar-based fuzz target runs
for 30 seconds in CI. Outer data-item lists now require exact SP separators,
known item names and a nonempty list; FAST/ALL/FULL are accepted only as
standalone macros. Invalid outer syntax receives BAD before any item dispatch,
and a separate fuzz target checks the flat-item grammar and section framing.
HEADER.FIELDS[.NOT] now requires a nonempty, exactly SP-separated astring list.
Quoted and literal names are decoded for exact case-insensitive matching, and
response labels preserve valid wire tokens. Checked tests cover escaped names,
folded fields, partial-after-subset ranges and Seen rules. Numeric MIME paths,
quoted header names and partials are also checked together in mixed item lists; a malformed item anywhere is refused before reads or Seen
writes. Literal names retain the command parser's ASCII/NUL/line-break restrictions.
SEARCH's six calendar-date keys require RFC 3501 date tokens: one/two day digits,
an ASCII month and four year digits, bare or enclosed in DQUOTEs. Invalid dates,
padding and quoted escapes receive BAD before content evaluation. Checked wire
tests cover Boolean/groups, empty mailboxes, recovery and calendar boundaries;
a grammar/calendar oracle fuzz target runs for 30 seconds in CI. General SEARCH
string/literal and separator grammar remain separate gaps.
The EXPUNGE/CLOSE slice checks removal, sequence renumbering, retry after backend
refusal, arguments and selection state, including the RFC 3501 examples. A
partial RFC 4315 UID EXPUNGE slice checks UID filtering, preserved non-target
marks, backend refusal, read-only operation and RFC 3501 §9 UID-set syntax
(malformed sets receive BAD before deletion). A CLOSE whose deletion is
refused answers NO and stays selected. Persistent Deleted flags remain an
explicit gap.
The APPEND slice checks the §6.3.11 example, exact message octets, supported
flags/dates, selected-mailbox notifications, backend refusal and terminal literal
framing, including the accepted non-synchronizing form. Early refusal of a
valid `{n+}` APPEND sends BYE and closes without dispatching unread payload;
backend refusal after consuming the command keeps the session usable. A
malformed flag-list, argument separator or date-time receives BAD before the
continuation. Date-times use fixed-width fields and space-padded or two-digit
days, without quoted escapes or fractional seconds; explicit dates, including
Go's zero time, reach a DateAppender without losing their UTC offset. Backend
atomicity, Recent/Deleted/keywords, default dates
remain explicit gaps; a complete grammar audit is still outstanding. A mailbox
name sent as a synchronizing or LITERAL+ literal is accepted before the message
literal.
Unquoted mailbox names require RFC 3501 ASTRING-CHAR octets: controls, non-ASCII
octets, wildcards and quoted-specials receive BAD before a continuation or
backend call. Checked wire tests enumerate all octets except CR/LF framing,
including accepted `]`, `}` and `+`, and cover flags, both literal modes and
pipelined payload refusal.
The command-argument literal slice checks individual/aggregate size refusal,
assembled-command limits, overlong continuation lines and accepted LOGIN framing.
Ambiguous refusals send BYE and end the session; synchronizing refusals and
fully consumed commands preserve the connection. An overlong initial command
line ending in a `{n+}` marker sends BYE and closes; other overlong lines keep
the session. Overlong AUTHENTICATE responses and IDLE lines are refused or
ignored without closing, since neither admits a literal. Malformed markers
remain unaudited. LITERAL+ is not advertised.
The COPY/UID COPY slice checks the RFC 3501 §6.4.7 and RFC 4315 §3 examples,
sparse/overlapping UID sets, COPYUID correspondence, read-only sources, destination
names, backend errors, selection state and RFC 3501 §9 argument syntax
(malformed sets or mailbox astrings receive BAD before dispatch). Errors stop
dispatch and return NO without COPYUID. Destination rollback, storage/flag/date
preservation and destination permission/UID validation remain explicit gaps.
Earlier copies may remain after failure; the per-message API cannot undo them.
The MOVE/UID MOVE slice checks the RFC 6851 UID MOVE response order (COPYUID,
EXPUNGEs, tagged OK), sparse/overlapping sets, read-only refusal, destination
names, selection state, backend errors and argument syntax (malformed sets or
mailbox astrings receive BAD before any move). A failure stops the move with NO
and reports EXPUNGE only for messages already moved. Per-message atomicity,
QRESYNC VANISHED and backend UID validation remain explicit gaps.
The STARTTLS slice follows the RFC 3501 §6.2.1 example over a real handshake:
LOGINDISABLED until TLS, changed capabilities afterwards, no implicit
authentication, BAD for arguments or a repeated STARTTLS, discarded pipelined
cleartext and a closed session after a failed negotiation. Cleartext LOGIN with
no TLS config and SASL EXTERNAL remain explicit gaps.
The BODY/BODYSTRUCTURE slice reproduces both RFC 3501 §7.4.2 example
structures and checks transfer-encoded sizes/line counts through nested MIME
parts. Quoted-printable parts retain their encoding, octets and soft-break lines.
Valid Content-Transfer-Encoding tokens drop surrounding whitespace and nested/escaped
comments at each entity level. Malformed values retain the opaque-string policy;
this metadata operation does not decode or validate payloads.
BODY omits extensions; BODYSTRUCTURE reports supplied Content-MD5, disposition,
Content-Language and Content-Location in the defined order, including nested
entities. Languages are one string or a list of tags with comments dropped.
Missing MD5/language/location remain NIL; digests are not generated or verified and relative
locations are not resolved.
Both are metadata and leave Seen unchanged. MIME parameter values preserve empty
strings, escape quoted-specials and use counted literals for decoded 8-bit/CRLF
values; IDs, descriptions and encoding strings also use literal framing when
needed, including in nested parts and disposition parameters. Ordinary entities
default to TEXT/PLAIN, US-ASCII and 7BIT when the relevant MIME fields are absent;
valid explicit types and charsets win. Invalid Content-Type syntax, including a
bare type without the mandatory subtype, uses the plain-text fallback in ordinary
entities, mixed children and encapsulated messages.
Content-Type comments and whitespace between tokens are checked against the RFC
2045 charset example, including nested/escaped comments and quoted parameter
data through multipart boundaries and encapsulated messages. A differential fuzz
target compares comment-bearing types with Go MIME parsing of equivalent
comment-free fields. Content-Disposition shares that comment lexer: comments
between the disposition type and parameters are ignored per RFC 2183, split
tokens and type/subtype pairs yield NIL. Unparseable header blocks, comments in
Content-ID/Description and malformed/empty multiparts remain gaps. FETCH refuses
raw message NUL and NUL decoded into envelope/MIME strings before response bytes
or Seen writes; BINARY is not implemented.
The top-level ENVELOPE slice checks the RFC 3501 §8 example, ten-field order,
header values over cached summaries, missing/empty headers, Sender/Reply-To
defaults, read-only metadata, and quoted/literal scalar and address framing.
Address groups preserve start/end markers, empty groups and member order, with
quoted punctuation, nested comments and encoded names. Source routes and legal
address syntax rejected by net/mail remain gaps. Malformed header blocks produce
ten NIL envelope fields rather than cached summaries. The MESSAGE/RFC822 slice
checks encapsulated envelope/body/line ordering, raw Date and empty scalar headers, quoted/literal scalar framing,
recursive messages and multipart bodies, and metadata side effects. Nested
envelopes share the top-level address and scalar encoders. Malformed encapsulated
headers retain a ten-field envelope and the existing opaque TEXT/PLAIN body
recovery. Prohibited transfer encodings and recursion limits remain explicit gaps.
Untyped multipart/digest children are described as MESSAGE/RFC822.
It is growing; it is not yet a full conformance claim.

The coverage badge shows the enforced minimum; measured statement coverage is
printed in each CI run's **Require at least 95% statement coverage** log (see
[COVERAGE.md](COVERAGE.md)). The checklist badge counts requirements mapped to
tests over all rows of this partial checklist, including the `n/a` rows with
documented reasons; it is not a count of every applicable RFC requirement. CI
checks both badges against the enforced gate, the actual checklist and its test
mappings. The CI badge reports whether verification passed.

## Documentation

Full API reference:
[pkg.go.dev/github.com/rest-mail/go-imap](https://pkg.go.dev/github.com/rest-mail/go-imap).

## Changelog

Recent releases — see [CHANGELOG.md](CHANGELOG.md) for the complete history.

- **v0.4.0** (2026-07-26) — `ENVELOPE` populates To/Cc/Bcc/In-Reply-To/Message-ID and uses the message `Date:` header; `SEARCH BODY`/`TEXT`/`RECENT` match for real; `BODYSTRUCTURE` emits Content-Disposition and MESSAGE/RFC822 encapsulation.
- **v0.3.0** (2026-07-25) — breaking: `Shutdown(ctx) error` + `Close()`; general synchronizing literals, IDLE expunge reporting, SEARCH/STATUS/FETCH correctness, response-injection escaping, panic recovery.
- **v0.2.3** (2026-07-25) — FETCH/EXAMINE/sequence-set correctness: `RFC822.SIZE` no longer sets `\Seen`, bounded UID-range expansion, read-only `EXAMINE` enforcement, `BODY`/`BODYSTRUCTURE` fetches.
- **v0.2.2** (2026-07-24) — refuse `AUTHENTICATE` before `STARTTLS` when a TLS config is present; README and godoc polish.
- **v0.2.1** (2026-07-23) — renamed the module to `github.com/rest-mail/go-imap`.
- **v0.2.0** (2026-07-23) — `UIDPLUS`, atomic server-side `MOVE`, `UNSELECT` and `ENABLE`, gated on optional `Mailbox` interfaces.
- **v0.1.0** (2026-07-23) — initial IMAP4rev1 server engine on a neutral `Backend` seam, standard library only.

## License

[MIT](LICENSE) © 2026 rest-mail
