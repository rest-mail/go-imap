package server

import (
	"strings"
	"testing"

	"github.com/rest-mail/go-imap/pkg/imap/internal/wire"
)

// FuzzLiteralTypeBoundaries checks that arbitrary earlier literal data cannot
// change a later operand's source type. Quoting here is the same representation
// the reader uses after consuming literal octets; malformed UTF-8 remains legal
// literal data, while NUL is never a legal ordinary string.
func FuzzLiteralTypeBoundaries(f *testing.F) {
	for _, value := range []string{"", "ON", `a"b\c`, "\r\n) ON HEADER", "\xff", "\x00", "Grüße"} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value string) {
		if len(value) > 2048 {
			t.Skip()
		}
		quoted := wire.QuoteLiteral([]byte(value))
		for _, command := range []string{"SEARCH", "UID"} {
			prefix := ""
			if command == "UID" {
				prefix = "SEARCH "
			}
			if literalArgumentAllowed(command, prefix+"SUBJECT "+quoted+" ON ") {
				t.Fatal("earlier literal data turned a date into a string slot")
			}
			if !strings.ContainsRune(value, 0) && !literalArgumentAllowed(command, prefix+"(OR SUBJECT "+quoted+" HEADER ON ") {
				t.Fatal("earlier literal data hid a valid HEADER string slot")
			}
		}
		for _, command := range []string{"NOOP", "ENABLE", "AUTHENTICATE"} {
			if literalArgumentAllowed(command, value) {
				t.Fatal("data created a literal slot in an atom-only command")
			}
		}
	})
}
