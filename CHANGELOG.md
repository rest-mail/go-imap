# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Note: pre-1.0, breaking changes may ship in a minor release.

## [Unreleased]

### Added

- **`SEARCH BODY`, `TEXT` and `RECENT` now match a real subset** (RFC 3501
  §6.4.4). `BODY <string>` matches a case-insensitive substring in the message
  body; `TEXT <string>` matches the header or the body; `RECENT` matches the
  `\Recent` messages, which this engine models as the unseen set (the same set it
  reports as RECENT in `SELECT`/`EXAMINE` and `STATUS`). The message body is
  loaded lazily, so a metadata-only search never fetches one.

### Fixed

- **The `ENVELOPE` recipient and reference fields are populated from the message
  headers** (RFC 3501 §7.4.2). `To`, `Cc`, `Bcc`, `In-Reply-To` and `Message-ID`
  were previously always `NIL`; they are now parsed from the stored message (an
  absent header stays `NIL`, so a message with no `Bcc` reports `NIL` as before).
- **The `ENVELOPE` date now comes from the message's `Date:` header** rather than
  its arrival time (RFC 3501 §7.4.2). `INTERNALDATE` still reports the arrival
  time — the two are distinct — and a missing or unparseable `Date:` header falls
  back to the arrival time.

## [0.3.0] - 2026-07-25

A minor release that collects the IMAP4rev1 correctness pass across the server's
command set — `STORE`/`UID STORE` flag semantics (RFC 3501 §6.4.6), `APPEND`
argument parsing (§6.3.11), general synchronizing-literal handling (§4.3) so a
`{n}` literal works as any string argument (not only `APPEND`'s message), `FETCH`
sections and partial ranges (§6.4.5), `SEARCH` key validation (§6.4.4), `STATUS`
item selection (§6.3.10), `LIST`/`LSUB` case-sensitivity and response encoding
(§5.1, §4.3), `IDLE` change reporting (RFC 2177), the connection greeting's
advertised capabilities, and `AUTHENTICATE` continuation/abort handling — together
with denial-of-service hardening (bounded command lines and literals, a
linear-time wildcard matcher, and per-connection panic recovery). It adds two
exported fields (`Message.Answered`, `FlagUpdate.Answered`), one optional interface
(`DateAppender`), and two tunable package variables (`MaxLiteralSize`,
`MaxCommandLineLength`); these are backward-compatible additions. It makes one
breaking change to `Server.Shutdown` (see Breaking).

### Breaking

- **`Server.Shutdown` now takes a `context.Context` and returns an `error`, and
  actually drains in-flight sessions.** It previously waited only on the
  accept-loop goroutines — which return the moment their listener closes — so it
  returned while client sessions were still being served, contradicting its
  "waits for in-flight sessions to finish" documentation. Each accepted connection
  is now tracked in the server's `sync.WaitGroup`, and `Shutdown(ctx)` closes the
  listeners and then blocks until every session finishes, or until `ctx` is done
  (returning `ctx.Err()`), mirroring `net/http.Server.Shutdown`. Callers of the
  old no-argument `Shutdown()` must pass a context (e.g.
  `srv.Shutdown(context.Background())`).
- **New `Server.Close() error` for an immediate hard stop:** it force-closes live
  connections without waiting, for callers that want the old fire-and-forget
  behaviour rather than a graceful drain.

### Added

- `Message.Answered` and `FlagUpdate.Answered` so the `\Answered` system flag,
  already advertised in `FLAGS`/`PERMANENTFLAGS`, can actually be stored and
  reported.
- `DateAppender`, an optional `Mailbox` interface. When the concrete mailbox
  implements it, a client-supplied `APPEND` date-time is passed through so the
  store can set the message's `INTERNALDATE`; backends that do not implement it
  are unchanged (the date is parsed and validated but not stored).
- `MaxLiteralSize`, a tunable package variable bounding a synchronizing literal
  used as an ordinary string/astring command argument (see the `Fixed` note on
  general literal handling). Defaults to 8 KiB; `APPEND`'s message literal keeps
  its own separate, larger bound.
- `MaxCommandLineLength`, a tunable package variable bounding a single command
  line (defaults to 8 KiB), so an unauthenticated client cannot exhaust server
  memory with one unbounded line.

### Fixed

- `LIST`/`LSUB` pattern matching is now case-sensitive for every mailbox name
  except `INBOX` (RFC 3501 §5.1). The matcher previously lower-cased both the
  pattern and the folder name, so a pattern such as `sent` wrongly matched a
  folder named `Sent`. `INBOX` — and an `INBOX`-spelled pattern — is still folded
  to the canonical spelling, so any-case `inbox` continues to list the one real
  `INBOX`.
- `SUBSCRIBE` and `UNSUBSCRIBE` (RFC 3501 §6.3.6/§6.3.7), mandatory IMAP4rev1
  commands, are now accepted instead of drawing a `BAD "Unknown command"`. This
  engine keeps no separate subscription list — `LSUB` reports every existing
  folder as subscribed — so the commands validate their mailbox argument and
  acknowledge with a tagged `OK`.
- `APPEND` into the mailbox the session currently has selected now emits an
  untagged `* n EXISTS` (RFC 3501 §6.3.11) and refreshes the cached message set,
  so the client is told a message arrived and later sequence-number references
  stay correct. Appends to any other mailbox remain invisible to the current
  selection.
- `IDLE` now reports external mailbox changes, not only new-message growth
  (RFC 2177). While a client idles, the poll diffs the mailbox against its cached
  message set and pushes an untagged `* n EXPUNGE` for each message another
  session removed — highest sequence number first, per the RFC 3501 §7.4.1
  renumbering rule — an `* n EXISTS` for new arrivals, and an `* n FETCH (FLAGS …)`
  when a surviving message's flags change, then updates the cache. Previously the
  poll only announced a larger count, so an external expunge was never reported
  and the cached set (which backs sequence-number resolution for every later
  command) went stale.
- A synchronizing literal (`{n}`, RFC 3501 §4.3) is now accepted as any
  string/astring argument of any command — `LOGIN`, `SELECT`, `CREATE`, `STATUS`,
  `SEARCH`, and so on — not only as `APPEND`'s message. The command reader detects
  a trailing `{n}`, sends the required `+` continuation, reads exactly `n` octets
  as the argument value (8-bit clean; an embedded space or quote is preserved),
  then continues parsing the rest of the command. Previously only `APPEND` handled
  a literal, so a literal anywhere else drew no continuation and its octets were
  misparsed as a new command, desyncing the connection. The non-synchronizing
  `LITERAL+` form (`{n+}`, RFC 2088) is accepted without a continuation. A literal
  used as an ordinary argument is bounded by `MaxLiteralSize` (checked before the
  continuation, so an over-large one is refused without its octets being read);
  `APPEND`'s large-message literal path is unchanged.
- `STORE FLAGS (...)` (replace mode) now sets the message's flags to exactly the
  given set, per RFC 3501 §6.4.6. Previously bare `FLAGS` was mishandled as a
  removal, so `STORE 1 FLAGS (\Flagged)` cleared `\Flagged` instead of setting
  it. `+FLAGS`/`-FLAGS` continue to add/remove.
- The `.SILENT` suffix now suppresses the untagged `FETCH` response; it was
  previously ignored so the untagged `FETCH` was always emitted.
- `STORE` now handles `\Answered` and `\Draft` instead of silently dropping them
  (only `\Seen`, `\Flagged` and `\Deleted` were handled before).
- A pending `\Deleted` mark now appears in `FETCH`/`STORE` flag lists; `buildFlags`
  previously omitted the session-local `\Deleted` state.
- `UID STORE` shares the same corrected flag logic as `STORE`.
- `APPEND` now accepts an unquoted (atom) mailbox name, per the `astring` grammar
  of RFC 3501 §6.3.11. Previously only a quoted name was recognised, so `APPEND
  Archive {n}` was ignored and the message misdelivered to `INBOX`. Quoted and
  literal names continue to work.
- `APPEND`'s optional flag-list and date-time are now parsed positionally after
  the mailbox rather than by scanning for the first `(`/`"`, so a parenthesis or
  quote inside a mailbox name is no longer mistaken for them. `\Answered` is now
  applied (it was silently dropped), and a flag the store cannot honour is
  rejected with a tagged `NO` instead of being discarded.
- `APPEND`'s optional date-time is now parsed and validated; with a `DateAppender`
  backend it sets the stored message's `INTERNALDATE`. It was previously ignored.
- The reserved mailbox name `INBOX` is now matched case-insensitively, per RFC
  3501 §5.1. Previously only the exact spelling `INBOX` was recognised, so
  `DELETE inbox` bypassed the standard-folder guard, `CREATE INBOX` (any case)
  was wrongly accepted, and `SELECT`/`EXAMINE`/`STATUS` of `inbox` depended on
  the backend to normalise. Any-case `INBOX` now resolves to the one real INBOX
  across `SELECT`/`EXAMINE`/`STATUS`/`CREATE`/`DELETE`/`RENAME`/`APPEND`/`COPY`/
  `MOVE`. All other mailbox names remain case-sensitive.
- Quoted-string arguments now have their backslash escapes decoded on input, per
  RFC 3501 §4.3: `\"` becomes a literal `"` and `\\` a literal `\`, and an escaped
  `\"` no longer terminates the string early. A `LOGIN` password (or mailbox name)
  containing a quote or backslash previously reached the backend mangled, so
  authentication failed silently.
- `SEARCH`/`UID SEARCH` no longer treat an unrecognised search key as `ALL`
  (RFC 3501 §6.4.4). An unknown key, a missing key argument, unbalanced
  parentheses, or a malformed `BEFORE`/`SINCE`/`ON` date (e.g. `SINCE 99-Foo-2020`)
  are now answered with a tagged `BAD` instead of silently matching every message.
  An unsupported `CHARSET` is answered `NO [BADCHARSET (US-ASCII UTF-8)]`;
  `US-ASCII` and `UTF-8` are accepted. Parenthesized key groups such as
  `(FROM "a" SUBJECT "b")` are parsed as the AND of the enclosed keys rather than
  mis-tokenised, and a bare message sequence set (e.g. `1,3:5`) is honoured as a
  valid search key.
- `STATUS` now honours the requested status-item list and returns exactly those
  items (RFC 3501 §6.3.10). Previously the list was ignored and a fixed
  `MESSAGES RECENT UNSEEN` set was always returned; `UIDNEXT` and `UIDVALIDITY`
  were unsupported. All five items — `MESSAGES`, `RECENT`, `UNSEEN`, `UIDNEXT`
  (highest UID + 1), `UIDVALIDITY` (the mailbox's real value on a UIDPLUS backend,
  else 1, matching `SELECT`/`EXAMINE`) — are now reported, an unknown item is
  rejected with `BAD`, and `STATUS` neither selects the mailbox nor sets `\Seen`.
- `AUTHENTICATE`'s continuation request is now the grammar-required `"+" SP`
  form (RFC 3501 §7.5 / §6.2.2, RFC 4959): `+ ` followed by the (empty, for
  PLAIN) base64 server challenge, rather than a bare `+`. A client that aborts
  the SASL exchange by sending a lone `*` is now answered with a tagged `BAD`
  ("authentication aborted"), per RFC 4959; previously the `*` was base64-decoded,
  the decode failed, and the abort was mis-reported as `NO "Invalid base64"`,
  conflating a deliberate cancel with a bad-credential decode error. A genuine
  wrong-credential response is still a tagged `NO [AUTHENTICATIONFAILED]`.
- `CHECK` (RFC 3501 §6.4.1) and `CLOSE` (§6.4.2) are now rejected with a tagged
  `BAD` when no mailbox is selected, rather than answering `OK`. Both are
  Selected-State commands: `CHECK` in the non-authenticated or authenticated
  (no mailbox selected) state, and `CLOSE` with nothing selected, previously
  completed `OK`. When a mailbox is selected `CHECK` still completes `OK` and
  `CLOSE` still expunges `\Deleted` messages (unless read-only) and returns to
  the authenticated state.
- The connection greeting's `[CAPABILITY ...]` code is now built from the live
  session state instead of a hardcoded `IMAP4rev1 STARTTLS AUTH=PLAIN`, and
  `LOGINDISABLED` is advertised whenever plaintext `LOGIN`/`AUTHENTICATE` is
  refused until TLS (RFC 3501 §7.1.1, RFC 2595). Previously the greeting offered
  `STARTTLS` even with no TLS configured (where it is refused) and offered
  `AUTH=PLAIN` while omitting `LOGINDISABLED` on a cleartext link to a
  TLS-requiring server, so the advertised capabilities disagreed with what the
  server would accept. The greeting, the untagged `CAPABILITY` response, and the
  post-login `[CAPABILITY ...]` code now share one dynamic list; after `STARTTLS`
  the `STARTTLS`/`LOGINDISABLED` tokens drop and `AUTH=PLAIN` appears.
- Mailbox names in `LIST`, `LSUB`, `STATUS`, and `QUOTAROOT` responses are now
  encoded per the `astring` grammar (RFC 3501 §4.3): a name containing a space,
  quote, backslash, or other special character is quoted or sent as a literal
  rather than emitted bare, where a client would otherwise mis-parse the response.
- `FETCH BODY[...]<start.count>` now returns only the requested partial range of
  the section and labels the response with the origin octet as
  `BODY[...]<start>` (RFC 3501 §6.4.5). Previously the partial specifier was
  parsed but ignored and the whole section was returned.
- `FETCH BODY[HEADER.FIELDS.NOT (...)]` now returns every header except those
  listed, and both `HEADER.FIELDS` and `HEADER.FIELDS.NOT` preserve multi-line
  (folded) header values instead of dropping the continuation lines.
- A `FETCH` of a body section that implicitly sets `\Seen` now reflects the new
  flag in the same response's `FLAGS` (RFC 3501 §6.4.5), so the client learns the
  message became read without issuing a separate fetch.
- `SELECT`/`EXAMINE` now report `UIDNEXT` as the highest existing UID plus one
  (RFC 3501 §2.3.1.1) instead of a placeholder, so a client can predict the UID
  the next appended message will receive.
- A failed `SELECT` or `EXAMINE` now leaves the session in the authenticated,
  no-mailbox-selected state rather than half-selecting the target, so a later
  command cannot operate on a mailbox that was never successfully opened.
- The server now sends an untagged `* BYE` before closing a connection that has
  passed the inactivity autologout timer (RFC 3501 §5.4), so the client is told
  the session was terminated rather than seeing an abrupt disconnect.
- A single command line is now bounded by `MaxCommandLineLength` (8 KiB by
  default), so an unauthenticated client can no longer exhaust server memory by
  sending one unbounded line.
- The `LIST`/`LSUB` wildcard matcher now runs in linear time, removing a
  super-linear matching path an authenticated client could exploit with a crafted
  pattern to pin CPU.
- A panic while serving one connection is now recovered and confined to that
  connection, so a single malformed client can no longer crash the whole server.

## [0.2.3] - 2026-07-25

Correctness fixes for FETCH, EXAMINE and sequence-set handling. No exported API
changes — this is a drop-in patch release.

### Fixed

- `FETCH RFC822.SIZE` now returns only the message size and never sets `\Seen`.
  Previously it could fetch the body and mark the message read as a side effect.
- Bounded sequence-set and UID range expansion so an authenticated client can no
  longer force the server to materialize a huge range (e.g. `1:*` against a
  crafted set), which could pin CPU and memory. Ranges are now resolved against
  the mailbox's actual UID bounds.
- `SELECT` now resets per-session `\Deleted` state, so `\Deleted` marks from a
  previously selected folder can no longer leak into and expunge messages in a
  different folder.
- `EXAMINE` now enforces read-only selection: state-mutating commands (`STORE`,
  `EXPUNGE`, `UID EXPUNGE`, `MOVE`/`UID MOVE`, and `CLOSE`-driven expunge) are
  refused while a mailbox is selected read-only (RFC 3501 §6.3.2). `SELECT` after
  `EXAMINE` restores read-write access.
- `FETCH` now expands the `ALL`, `FAST` and `FULL` macros, implements `BODY` and
  `BODYSTRUCTURE` (single-part, non-text and multipart), and populates the `To`
  address in `ENVELOPE`. `BODYSTRUCTURE`/`BODY` structure fetches do not set
  `\Seen`.

## [0.2.2] - 2026-07-24

### Fixed

- Refuse `AUTHENTICATE` before `STARTTLS` when a TLS configuration is present, so
  credentials are never accepted over an unprotected connection.

### Documentation

- Polished README and godoc for the public release.

## [0.2.1] - 2026-07-23

### Changed

- Renamed the module to `github.com/rest-mail/go-imap`.

## [0.2.0] - 2026-07-23

### Added

- `UIDPLUS` (RFC 4315), atomic server-side `MOVE` (RFC 6851), `UNSELECT`
  (RFC 3691) and `ENABLE` (RFC 5161), lit up through optional `Mailbox`
  interfaces (`UIDPlusMailbox`, `Mover`) so capabilities are advertised only when
  the backend implements them.

## [0.1.0] - 2026-07-23

### Added

- Initial IMAP4rev1 server engine (RFC 3501) built on a neutral `Backend` seam,
  standard library only, with no external dependencies.
