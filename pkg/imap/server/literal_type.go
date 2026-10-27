package server

import (
	"strings"

	"github.com/rest-mail/go-imap/pkg/imap/internal/wire"
)

// literalArgumentAllowed checks a literal's source type before replacing it
// with an internal quoted string. That representation alone cannot distinguish
// a legal string from a date whose grammar permits quotes but not literals.
func literalArgumentAllowed(command, prefix string) bool {
	if wire.NoArguments[command] || command == "ENABLE" || command == "AUTHENTICATE" {
		return false
	}
	if command == "UID" {
		subcommand, rest, found := strings.Cut(prefix, " ")
		if !found || subcommand == "" {
			return false // the UID subcommand itself is an atom
		}
		if !strings.EqualFold(subcommand, "SEARCH") {
			return true // other UID grammars validate their materialized arguments
		}
		command, prefix = "SEARCH", rest
	}
	if command != "SEARCH" {
		return true
	}
	// Probe the real criterion parser with a valid, nonempty string token.
	// Only string-operand branches mark it accepted. Later missing criteria or
	// closing parentheses do not invalidate a legal literal at this position:
	// the client supplies those in the continuation after its literal data.
	_, query, ok := parseSearchReturn(prefix + `"literal-type-probe"`)
	if !ok {
		return false
	}
	tokens, ok := tokenizeSearch(query)
	if !ok || len(tokens) == 0 {
		return false
	}
	probe := &searchLiteralProbe{index: len(tokens) - 1}
	if strings.EqualFold(tokens[0], "CHARSET") {
		if probe.index == 1 {
			return true // RFC3501 SEARCH's charset is an astring
		}
		if len(tokens) < 2 {
			return false
		}
		tokens = tokens[2:]
		probe.index -= 2
	}
	for i := 0; i < len(tokens); {
		_, next, err := parseSearchCriterion(tokens, i, 0, probe)
		if err != nil {
			return probe.allowed
		}
		if next <= i {
			return false
		}
		i = next
	}
	return probe.allowed
}

type searchLiteralProbe struct {
	index   int
	allowed bool
}

func (p *searchLiteralProbe) stringOperand(indices ...int) {
	if p == nil {
		return
	}
	for _, index := range indices {
		if p.index == index {
			p.allowed = true
		}
	}
}
