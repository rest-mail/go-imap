# go-imap

[![CI](https://github.com/rest-mail/go-imap/actions/workflows/ci.yml/badge.svg)](https://github.com/rest-mail/go-imap/actions/workflows/ci.yml)
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
numbers, flags, `ENVELOPE` and `SEARCH` results from it, calling `Fetch` only
when a body is requested. Folders are implicit — there is no folder-management
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

`CONDSTORE`/`QRESYNC` (RFC 7162) are not implemented; the `ENABLE` command is
accepted and, having no enable-able extension yet, is a no-op.

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

## Documentation

Full API reference:
[pkg.go.dev/github.com/rest-mail/go-imap](https://pkg.go.dev/github.com/rest-mail/go-imap).

## License

[MIT](LICENSE) © 2026 rest-mail
