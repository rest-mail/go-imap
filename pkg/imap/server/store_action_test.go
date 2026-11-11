package server

import (
	"regexp"
	"strings"
	"testing"
)

// storeActionGrammar spells out ASCII case pairs: regexp's (?i) would also
// match Unicode long s, precisely the alias this grammar must exclude.
var storeActionGrammar = regexp.MustCompile(`^[+-]?[Ff][Ll][Aa][Gg][Ss](\.[Ss][Ii][Ll][Ee][Nn][Tt])?$`)

func checkStoreAction(t *testing.T, action string) {
	t.Helper()
	mode, silent, ok := parseStoreAction(action)
	wantOK := storeActionGrammar.MatchString(action)
	if ok != wantOK {
		t.Fatalf("parseStoreAction(%q) ok=%t, grammar=%t", action, ok, wantOK)
	}
	if !ok {
		if mode != 0 || silent {
			t.Fatalf("invalid action %q leaked mode=%d silent=%t", action, mode, silent)
		}
		return
	}
	wantMode := storeReplace
	switch action[0] {
	case '+':
		wantMode = storeAdd
	case '-':
		wantMode = storeRemove
	}
	if mode != wantMode || silent != strings.Contains(action, ".") {
		t.Fatalf("parseStoreAction(%q) mode=%d silent=%t", action, mode, silent)
	}
}

// TestStoreActionGrammar checks every octet inserted at each action boundary,
// including raw invalid UTF-8 and framing octets unsuitable for a wire test.
func TestStoreActionGrammar(t *testing.T) {
	for _, action := range []string{"FLAGS", "+FLAGS", "-FLAGS", "FLAGS.SILENT", "+FLAGS.SILENT", "-FLAGS.SILENT"} {
		checkStoreAction(t, action)
		for i := 0; i <= len(action); i++ {
			for c := 0; c < 256; c++ {
				checkStoreAction(t, action[:i]+string([]byte{byte(c)})+action[i:])
			}
		}
	}
}

// FuzzStoreAction checks parsing and dispatch against the ASCII RFC 3501 §9
// grammar, including mode and .SILENT rather than acceptance alone.
func FuzzStoreAction(f *testing.F) {
	for _, s := range []string{"", "FLAGS", "+FLAGS", "-FLAGS", "FLAGS.SILENT", "+FLAGS.SILENT", "-FLAGS.SILENT", "fLaGs.sIlEnT", "FLAGſ", "FLAGS.ſILENT", "FLAGS\t", "\u00a0FLAGS", "FLAGS.SILENT.SILENT", "FLAGS\x00", "FLAGS\xff"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, action string) {
		if len(action) > 128 {
			return
		}
		checkStoreAction(t, action)
	})
}
