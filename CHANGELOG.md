# Changelog

All notable changes to this project are documented here. This project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html); while the module
is pre-1.0, patch releases carry correctness fixes and minor releases add or
change the exported API.

## Unreleased

Correctness fixes for `STORE`/`UID STORE` flag handling (RFC 3501 §6.4.6) and
`APPEND` argument parsing (§6.3.11). Adds two exported fields (`Message.Answered`,
`FlagUpdate.Answered`) and one optional interface (`DateAppender`); struct
literals using field names are unaffected, so these are backward-compatible
additions. One breaking change: `Server.Shutdown` now takes a `context.Context`
and actually drains in-flight sessions — callers pass a context and can switch
to the new `Server.Close` for an immediate stop.

### Added

- `Message.Answered` and `FlagUpdate.Answered` so the `\Answered` system flag,
  already advertised in `FLAGS`/`PERMANENTFLAGS`, can actually be stored and
  reported.
- `DateAppender`, an optional `Mailbox` interface. When the concrete mailbox
  implements it, a client-supplied `APPEND` date-time is passed through so the
  store can set the message's `INTERNALDATE`; backends that do not implement it
  are unchanged (the date is parsed and validated but not stored).

### Changed

- **`Server.Shutdown` now takes a `context.Context` and actually drains
  in-flight sessions.** It previously waited only on the accept-loop goroutines —
  which return the moment their listener closes — so it returned while client
  sessions were still being served, contradicting its "waits for in-flight
  sessions to finish" documentation. Each accepted connection is now tracked in
  the server's `sync.WaitGroup`, and `Shutdown(ctx)` closes the listeners and
  then blocks until every session finishes, or until `ctx` is done (returning
  `ctx.Err()`), mirroring `net/http.Server.Shutdown`. A new `Server.Close()`
  provides the immediate hard stop: it force-closes live connections without
  waiting. Callers of the old no-argument `Shutdown()` must pass a context (e.g.
  `srv.Shutdown(context.Background())`).

### Fixed

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

## v0.2.3 - 2026-07-25

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

## v0.2.2 - 2026-07-24

### Fixed

- Refuse `AUTHENTICATE` before `STARTTLS` when a TLS configuration is present, so
  credentials are never accepted over an unprotected connection.

### Documentation

- Polished README and godoc for the public release.

## v0.2.1 - 2026-07-23

### Changed

- Renamed the module to `github.com/rest-mail/go-imap`.

## v0.2.0 - 2026-07-23

### Added

- `UIDPLUS` (RFC 4315), atomic server-side `MOVE` (RFC 6851), `UNSELECT`
  (RFC 3691) and `ENABLE` (RFC 5161), lit up through optional `Mailbox`
  interfaces (`UIDPlusMailbox`, `Mover`) so capabilities are advertised only when
  the backend implements them.

## v0.1.0 - 2026-07-23

### Added

- Initial IMAP4rev1 server engine (RFC 3501) built on a neutral `Backend` seam,
  standard library only, with no external dependencies.
