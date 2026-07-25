# Changelog

All notable changes to this project are documented here. This project adheres
to [Semantic Versioning](https://semver.org/spec/v2.0.0.html); while the module
is pre-1.0, patch releases carry correctness fixes and minor releases add or
change the exported API.

## Unreleased

Correctness fixes for `STORE`/`UID STORE` flag handling (RFC 3501 §6.4.6). Adds
two exported fields (`Message.Answered`, `FlagUpdate.Answered`); struct literals
using field names are unaffected, so this is a backward-compatible addition.

### Added

- `Message.Answered` and `FlagUpdate.Answered` so the `\Answered` system flag,
  already advertised in `FLAGS`/`PERMANENTFLAGS`, can actually be stored and
  reported.

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
