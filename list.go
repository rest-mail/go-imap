package imap

import (
	"strings"
)

// LIST and LSUB (RFC 3501 §§6.3.8-6.3.9), with LIST-EXTENDED (RFC 5258):
// selection options before the reference, several patterns in parentheses, and
// return options after them. RFC 6154's SPECIAL-USE and RFC 5819's STATUS are
// options of the same syntax.
//
// This engine keeps no subscription list (see handleSubscription): every
// existing folder counts as subscribed, which is what LSUB answers too.

// listRequest is one LIST or LSUB command, parsed.
type listRequest struct {
	extended  bool            // any LIST-EXTENDED syntax was used
	selection map[string]bool // SUBSCRIBED, REMOTE, RECURSIVEMATCH, SPECIAL-USE
	reference string
	patterns  []string
	returns   map[string]bool // SUBSCRIBED, CHILDREN, SPECIAL-USE, STATUS
	status    []string        // STATUS return option items
}

// listSelectionOptions are the selection options this server knows: RFC
// 5258's and RFC 6154's.
var listSelectionOptions = map[string]bool{"SUBSCRIBED": true, "REMOTE": true, "RECURSIVEMATCH": true, "SPECIAL-USE": true}

// parseList parses LIST's or LSUB's arguments. refusal is the BAD text for a
// command this server cannot take.
func parseList(args, cmd string) (req listRequest, refusal string) {
	req.selection, req.returns = map[string]bool{}, map[string]bool{}
	args = strings.TrimSpace(args)
	if cmd == "LIST" && strings.HasPrefix(args, "(") {
		end := strings.IndexByte(args, ')')
		if end < 0 {
			return req, "Unterminated LIST selection options"
		}
		req.extended = true
		for _, opt := range strings.Fields(args[1:end]) {
			opt = strings.ToUpper(opt)
			if !listSelectionOptions[opt] {
				return req, "Unsupported LIST selection option " + opt
			}
			req.selection[opt] = true
		}
		// RECURSIVEMATCH alone, or only with REMOTE, is BAD (RFC 5258 §3.1).
		if req.selection["RECURSIVEMATCH"] && !req.selection["SUBSCRIBED"] && !req.selection["SPECIAL-USE"] {
			return req, "RECURSIVEMATCH needs another selection option"
		}
		args = args[end+1:]
	}

	parts := parseIMAPArgs(args)
	req.reference, req.patterns = "", []string{"*"}
	if len(parts) >= 1 {
		last := parts[len(parts)-1]
		if len(parts) >= 2 {
			req.reference, last = unquote(parts[0]), parts[1]
		}
		if cmd == "LIST" && strings.HasPrefix(last, "(") {
			// Several patterns, any of which a name may match (RFC 5258 §3).
			if len(last) < 2 || !strings.HasSuffix(last, ")") {
				return req, "Unterminated LIST patterns"
			}
			req.extended = true
			req.patterns = nil
			for _, p := range parseIMAPArgs(last[1 : len(last)-1]) {
				req.patterns = append(req.patterns, unquote(p))
			}
		} else {
			req.patterns = []string{unquote(last)}
		}
	}
	if cmd == "LIST" && len(parts) > 2 {
		ret := parts[2:]
		if len(ret) != 2 || !strings.EqualFold(ret[0], "RETURN") || !strings.HasPrefix(ret[1], "(") || !strings.HasSuffix(ret[1], ")") {
			return req, "Invalid LIST arguments"
		}
		req.extended = true
		var problem string
		if req.status, problem = listReturnOptions(ret[1][1:len(ret[1])-1], req.returns); problem != "" {
			return req, problem
		}
	}
	// The SUBSCRIBED selection option implies the return option (RFC 5258 §3.1).
	if req.selection["SUBSCRIBED"] {
		req.returns["SUBSCRIBED"] = true
	}
	return req, ""
}

// listReturnOptions reads LIST's return options, the inside of RETURN's
// parentheses (RFC 5258 §6), into returns: SUBSCRIBED and CHILDREN (RFC 5258
// §3.2), SPECIAL-USE (RFC 6154), which every LIST reports anyway, and STATUS
// with a list of STATUS items (RFC 5819 §4), which it gives back. refusal is
// the BAD text for anything else.
func listReturnOptions(list string, returns map[string]bool) (status []string, refusal string) {
	for list != "" {
		var opt string
		opt, list, _ = strings.Cut(list, " ")
		switch up := strings.ToUpper(opt); {
		case up == "SUBSCRIBED", up == "CHILDREN", up == "SPECIAL-USE":
			returns[up] = true
		case up == "STATUS" && status == nil:
			end := strings.IndexByte(list, ')')
			items, ok := parseStatusItems(list[:end+1])
			for _, item := range items {
				ok = ok && statusItems[item]
			}
			if end < 0 || !ok {
				return nil, "Invalid STATUS return option"
			}
			returns["STATUS"] = true
			status, list = items, list[end+1:]
			if list != "" {
				if list, ok = strings.CutPrefix(list, " "); !ok || list == "" {
					return nil, "Invalid LIST return options"
				}
			}
		default:
			return nil, "Unsupported LIST return option " + opt
		}
	}
	return status, ""
}

// listEntry is a name LIST may answer: a folder, or a level of hierarchy above
// one that is not itself a mailbox.
type listEntry struct {
	folder Folder
	wire   string
	exists bool
}

func (s *Session) handleList(tag, args, cmd string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}
	req, refusal := parseList(args, cmd)
	if refusal != "" {
		s.tagged(tag, "BAD", refusal)
		return
	}

	// An empty pattern asks for the hierarchy delimiter and the reference's root
	// (RFC 3501 §6.3.8): the reference up to and including its first delimiter,
	// or the empty root. An extended LIST never does; it ignores an empty
	// pattern instead (RFC 5258 §3).
	if !req.extended && req.patterns[0] == "" {
		root := ""
		if i := strings.Index(req.reference, "/"); i >= 0 {
			root = req.reference[:i+1]
		}
		s.send(`* %s (\Noselect) "/" %s`, cmd, encodeMailboxName(root))
		s.tagged(tag, "OK", cmd+" completed")
		return
	}
	// A non-empty reference is prefixed to each pattern, unless the pattern
	// already starts at the hierarchy root (RFC 3501 §6.3.8 examples).
	var patterns []string
	for _, p := range req.patterns {
		if p == "" {
			continue
		}
		if req.reference != "" && !strings.HasPrefix(p, "/") {
			p = req.reference + p
		}
		patterns = append(patterns, p)
	}

	folders, err := s.mailbox.Folders()
	if err != nil {
		s.tagged(tag, "NO", "Failed to list folders")
		return
	}
	entries := s.listEntries(folders)
	matched := func(wire string) bool {
		for _, p := range patterns {
			if matchIMAPPattern(p, wire) {
				return true
			}
		}
		return false
	}
	// meets reports whether an existing folder satisfies the selection options:
	// every folder is subscribed, and SPECIAL-USE asks for a role (RFC 6154 §3).
	meets := func(e listEntry) bool {
		return e.exists && (!req.selection["SPECIAL-USE"] || specialUses[e.folder.SpecialUse])
	}

	// One reply per matched name (RFC 5258 §3.3): one that meets the selection,
	// or one that does not but has a descendant that does and is not matched
	// itself, with CHILDINFO under RECURSIVEMATCH (§3.5), and otherwise, for a
	// level that is not a mailbox, flagged so the client can open the hierarchy
	// (RFC 3501 §6.3.8; RFC 5258 example 11).
	for _, e := range entries {
		if !matched(e.wire) {
			continue
		}
		// qualifying: a descendant meets the selection and is not matched, so
		// it is not answered itself.
		qualifying := false
		for _, d := range entries {
			if strings.HasPrefix(d.wire, e.wire+"/") && meets(d) && !matched(d.wire) {
				qualifying = true
				break
			}
		}
		childInfo := req.selection["RECURSIVEMATCH"] && qualifying
		if !meets(e) && (!qualifying || e.exists && !childInfo) {
			continue
		}

		var attrs []string
		if !e.exists {
			if req.extended {
				attrs = append(attrs, `\NonExistent`)
			} else {
				attrs = append(attrs, `\Noselect`)
			}
		}
		// The role is the store's, reported on every LIST (RFC 6154 §2 allows it
		// on the ordinary command too); an attribute the RFC does not define is
		// not reported.
		if specialUses[e.folder.SpecialUse] {
			attrs = append(attrs, e.folder.SpecialUse)
		}
		if req.returns["SUBSCRIBED"] && e.exists {
			attrs = append(attrs, `\Subscribed`)
		}
		hasChildren := false
		for _, d := range entries {
			if d.exists && strings.HasPrefix(d.wire, e.wire+"/") {
				hasChildren = true
				break
			}
		}
		switch {
		case req.returns["CHILDREN"] && hasChildren, !e.exists && req.extended && !childInfo:
			attrs = append(attrs, `\HasChildren`)
		case req.returns["CHILDREN"]:
			attrs = append(attrs, `\HasNoChildren`)
		}

		line := `* ` + cmd + ` (` + strings.Join(attrs, " ") + `) "/" ` + encodeMailboxName(e.wire)
		if childInfo {
			var criteria []string
			for _, opt := range []string{"SUBSCRIBED", "SPECIAL-USE"} {
				if req.selection[opt] {
					criteria = append(criteria, `"`+opt+`"`)
				}
			}
			line += ` ("CHILDINFO" (` + strings.Join(criteria, " ") + `))`
		}
		s.send("%s", line)
		// A listed folder is selectable, so its STATUS follows its LIST; a
		// level that is not a mailbox gets none, and one the store cannot count
		// has its STATUS dropped (RFC 5819 §2).
		if req.status != nil && e.exists {
			if data, err := s.statusData(e.folder.Name, req.status); err == nil {
				s.send(`* STATUS %s (%s)`, encodeMailboxName(e.wire), data)
			}
		}
	}
	s.tagged(tag, "OK", cmd+" completed")
}

// listEntries are the folders as this session writes their names, each
// preceded by the levels of hierarchy above it that are not folders.
func (s *Session) listEntries(folders []Folder) []listEntry {
	exists := make(map[string]bool, len(folders))
	for _, f := range folders {
		exists[s.wireName(f.Name)] = true
	}
	seen := map[string]bool{}
	var entries []listEntry
	for _, f := range folders {
		wire := s.wireName(f.Name)
		for i := 0; i < len(wire); i++ {
			if wire[i] != '/' || i == 0 {
				continue
			}
			if parent := wire[:i]; !exists[parent] && !seen[parent] {
				seen[parent] = true
				entries = append(entries, listEntry{wire: parent})
			}
		}
		if !seen[wire] {
			seen[wire] = true
			entries = append(entries, listEntry{folder: f, wire: wire, exists: true})
		}
	}
	return entries
}
