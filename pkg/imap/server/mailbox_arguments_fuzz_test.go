package server

import (
	"slices"
	"strings"
	"testing"

	"github.com/rest-mail/go-imap/pkg/imap/internal/wire"
)

func FuzzMailboxArguments(f *testing.F) {
	for _, args := range []string{"Work", `"Old space" "New space"`, `"a\"b\\c"`, `"" ""`, "Work New extra", " Work", "Work  New", `"bad\q"`, "\"line\r\nbreak\"", "\"\x00\""} {
		f.Add(args, false)
		f.Add(args, true)
	}
	f.Fuzz(func(t *testing.T, args string, pair bool) {
		count := 1
		if pair {
			count = 2
		}
		names, ok := mailboxArguments(args, count)
		if !ok {
			return
		}
		if len(names) != count {
			t.Fatal("accepted arguments have the wrong arity")
		}
		encoded := make([]string, count)
		for i, name := range names {
			if strings.ContainsRune(name, 0) {
				t.Fatal("a mailbox astring contains NUL")
			}
			encoded[i] = wire.QuoteLiteral([]byte(name))
		}
		again, ok := mailboxArguments(strings.Join(encoded, " "), count)
		if !ok || !slices.Equal(names, again) {
			t.Fatal("re-encoding decoded astrings changes their identities")
		}
	})
}
