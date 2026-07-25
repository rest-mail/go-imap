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
additions.

### Added

- `Message.Answered` and `FlagUpdate.Answered` so the `\Answered` system flag,
  already advertised in `FLAGS`/`PERMANENTFLAGS`, can actually be stored and
  reported.
- `DateAppender`, an optional `Mailbox` interface. When the concrete mailbox
  implements it, a client-supplied `APPEND` date-time is passed through so the
  store can set the message's `INTERNALDATE`; backends that do not implement it
  are unchanged (the date is parsed and validated but not stored).

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
