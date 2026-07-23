# imap

[![CI](https://github.com/rest-mail/imap/actions/workflows/ci.yml/badge.svg)](https://github.com/rest-mail/imap/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/rest-mail/imap.svg)](https://pkg.go.dev/github.com/rest-mail/imap)

An IMAP4rev1 ([RFC 3501](https://www.rfc-editor.org/rfc/rfc3501)) server engine
for Go, with zero external dependencies (standard library only).

You supply a `Backend` that authenticates users and returns a `Mailbox` view over
their folders and messages, expressed as neutral `Message` values; the `Server`
speaks the wire protocol — `CAPABILITY`, `STARTTLS`, `LOGIN`, `AUTHENTICATE
PLAIN`, `LIST`/`LSUB`, `SELECT`/`EXAMINE`, `STATUS`, `FETCH`, `UID
FETCH`/`STORE`/`COPY`/`MOVE`/`SEARCH`/`EXPUNGE`, `STORE`, `COPY`, `MOVE`,
`EXPUNGE`, `CLOSE`, `UNSELECT` (RFC 3691), `ENABLE` (RFC 5161), `APPEND`, `IDLE`
and `QUOTA` (RFC 2087). The engine holds no assumptions about where mail lives: a
`Backend` can be a database, a maildir, or a remote API.

Two extensions — `UIDPLUS` (RFC 4315: `APPENDUID`/`COPYUID` resp-codes and
`UID EXPUNGE`) and atomic server-side `MOVE` (RFC 6851) — light up only when the
concrete backend opts in through the optional interfaces below; a backend that
implements just `Mailbox` behaves exactly as before, and the advertised
`CAPABILITY` list reflects only what the backend actually supports.

`FETCH BODY[]` serves exactly the bytes `Mailbox.Fetch` returns; `BODY.PEEK[]`
never sets `\Seen` while a plain `BODY[]` fetch does (RFC 3501 §6.4.5). UID
ranges expand across non-contiguous UIDs and `UID FETCH *` resolves to the
newest message. The IDLE poller is fully stopped before the tagged completion is
written, so responses never interleave.

## Install

```sh
go get github.com/rest-mail/imap
```

## Usage

Implement `Backend` and `Mailbox`, then hand the server a listener config:

```go
package main

import (
	"crypto/tls"

	"github.com/rest-mail/imap"
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
		{UID: 101, Size: 4213, Seen: false, Subject: "hello",
			From: imap.Address{Name: "Alice", Email: "alice@example.com"}},
	}, nil
}

func (a *account) Fetch(uid uint32) ([]byte, error)                  { /* full RFC 5322 bytes */ return nil, nil }
func (a *account) Store(uid uint32, f imap.FlagUpdate) error         { /* \Seen/\Flagged changes */ return nil }
func (a *account) Move(uid uint32, dest string) error                { return nil }
func (a *account) Delete(uid uint32) error                           { /* EXPUNGE */ return nil }
func (a *account) Copy(uid uint32, dest string) error                { return nil }
func (a *account) Append(dest string, f imap.FlagUpdate, raw []byte) error { return nil }
func (a *account) Quota() (used, limit int64, err error)             { return 0, 0, nil }

func main() {
	cert, _ := tls.LoadX509KeyPair("cert.pem", "key.pem")
	tlsConfig := &tls.Config{Certificates: []tls.Certificate{cert}}

	// nil Limiter -> imap.NopLimiter (no per-IP limits).
	srv := imap.NewServer("mail.example.com", &store{}, tlsConfig, nil)
	if err := srv.ListenAndServe(imap.Ports{IMAP: 143, IMAPTLS: 993}); err != nil {
		panic(err)
	}
	select {} // serve until Shutdown
}
```

For a single accepted connection (e.g. behind your own listener), construct a
session directly with `imap.NewSession(conn, backend, hostname, tlsConfig,
limiter)` and call `Handle()`.

### Optional extensions (UIDPLUS, MOVE)

Richer extensions are opt-in through **optional interfaces** the server
type-asserts on your `Mailbox` at runtime. You never have to implement them: a
`Mailbox` that satisfies only the base interface keeps its exact behaviour and
the server advertises neither `UIDPLUS` nor a COPYUID-bearing `MOVE`. Implement
an interface and the matching capability and response codes turn on automatically
— the `CAPABILITY` list stays honest because it is derived from what your
concrete type implements.

- **`UIDPlusMailbox`** enables `UIDPLUS` (RFC 4315). It adds `UIDValidity`,
  `AppendUID` and `CopyUID` — the UID-returning forms of `Append`/`Copy` — so the
  server can emit `[APPENDUID uidvalidity uid]` on `APPEND`, `[COPYUID …]` on
  `COPY`/`MOVE`, report the real `UIDVALIDITY` in `SELECT`, and honour
  `UID EXPUNGE`.
- **`Mover`** enables an atomic server-side `MOVE`/`UID MOVE` (RFC 6851) via
  `MoveUID`. Without it, `MOVE` still works by calling `Mailbox.Move` per message;
  with it (plus `UIDPlusMailbox`) `MOVE` reports the destination UID in a
  `COPYUID` response code.

```go
// Opt in by adding methods to your existing Mailbox type — no base changes.
func (a *account) UIDValidity(folder string) (uint32, error)            { /* per-folder UIDVALIDITY */ return 1, nil }
func (a *account) AppendUID(dest string, f imap.FlagUpdate, raw []byte) (uint32, error) { /* store, return new UID */ return 0, nil }
func (a *account) CopyUID(srcUID uint32, dest string) (uint32, error)   { /* copy, return new UID */ return 0, nil }
func (a *account) MoveUID(srcUID uint32, dest string) (uint32, error)   { /* atomic move, return new UID */ return 0, nil }
```

`CONDSTORE`/`QRESYNC` (RFC 7162) are not implemented; the `ENABLE` command is
accepted and, having no enable-able extension yet, is a no-op.

### Rate limiting

`NewServer` accepts a `Limiter` — a small structural interface
(`Accept`/`Release`/`RecordAuthFail`/`IsBanned`/`ResetAuth`) the engine consults
for per-IP connection caps and auth-failure bans. Pass `nil` (or `imap.NopLimiter{}`)
for none, or wire in your own; any type with those methods satisfies it.

## License

[MIT](LICENSE) © 2026 rest-mail
