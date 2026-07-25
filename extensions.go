package imap

import (
	"log/slog"
	"sort"
	"strconv"
	"strings"
)

// capabilities returns the space-separated CAPABILITY tokens for the session's
// current state. Extensions that depend on optional backend interfaces are
// included only once the concrete [Mailbox] is known (post-authentication) and
// only when it implements them, so the advertised list never over-promises.
func (s *Session) capabilities() string {
	caps := []string{"IMAP4rev1", "IDLE", "MOVE", "QUOTA", "UNSELECT", "ENABLE"}
	if _, ok := s.mailbox.(UIDPlusMailbox); ok {
		caps = append(caps, "UIDPLUS")
	}
	if !s.usingTLS && s.tlsConfig != nil {
		caps = append(caps, "STARTTLS")
	}
	if s.usingTLS || s.tlsConfig == nil {
		caps = append(caps, "AUTH=PLAIN")
	}
	return strings.Join(caps, " ")
}

// uidValidity returns the UIDVALIDITY of folder when the mailbox implements
// [UIDPlusMailbox] and can report it. The bool is false for a base backend, a
// backend error, or a zero value — the caller then omits the UIDPLUS resp-code.
func (s *Session) uidValidity(folder string) (uint32, bool) {
	up, ok := s.mailbox.(UIDPlusMailbox)
	if !ok {
		return 0, false
	}
	v, err := up.UIDValidity(folder)
	if err != nil || v == 0 {
		return 0, false
	}
	return v, true
}

// copyMessages copies the messages at the given sequence numbers into dest and
// returns a COPYUID response code body (without the surrounding brackets) when
// the mailbox implements [UIDPlusMailbox], or "" otherwise. It is shared by COPY
// and UID COPY.
func (s *Session) copyMessages(seqNums []int, dest string) string {
	up, uidPlus := s.mailbox.(UIDPlusMailbox)
	var srcUIDs, dstUIDs []uint32

	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]
		if uidPlus {
			newUID, err := up.CopyUID(msg.UID, dest)
			if err != nil {
				slog.Warn("imap: copy failed", "uid", msg.UID, "error", err)
				continue
			}
			srcUIDs = append(srcUIDs, msg.UID)
			dstUIDs = append(dstUIDs, newUID)
			continue
		}
		if err := s.mailbox.Copy(msg.UID, dest); err != nil {
			slog.Warn("imap: copy failed", "uid", msg.UID, "error", err)
		}
	}

	if uidPlus && len(dstUIDs) > 0 {
		if v, ok := s.uidValidity(dest); ok {
			return "COPYUID " + strconv.FormatUint(uint64(v), 10) + " " +
				joinUIDs(srcUIDs) + " " + joinUIDs(dstUIDs)
		}
	}
	return ""
}

// moveMessages relocates the messages at the given sequence numbers to dest and
// writes the full MOVE response: an untagged COPYUID resp-code first (RFC 6851,
// when the mailbox supports UIDPLUS and the atomic [Mover]), then an untagged
// EXPUNGE per moved message, then the tagged completion. It is shared by MOVE
// and UID MOVE. A backend that implements [Mover] moves atomically; otherwise
// the move falls back to [Mailbox.Move] per message.
func (s *Session) moveMessages(tag, cmdName string, seqNums []int, dest string) {
	// MOVE expunges the source messages, so it changes the mailbox's permanent
	// state and is refused on a read-only (EXAMINE) selection (RFC 3501 §6.3.2).
	if s.refuseReadOnly(tag) {
		return
	}
	mover, hasMover := s.mailbox.(Mover)

	var srcUIDs, dstUIDs []uint32
	var doneSeq []int
	for _, seq := range seqNums {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		msg := s.messages[seq-1]
		var newUID uint32
		var err error
		if hasMover {
			newUID, err = mover.MoveUID(msg.UID, dest)
		} else {
			err = s.mailbox.Move(msg.UID, dest)
		}
		if err != nil {
			slog.Warn("imap: move failed", "uid", msg.UID, "error", err)
			continue
		}
		srcUIDs = append(srcUIDs, msg.UID)
		dstUIDs = append(dstUIDs, newUID)
		doneSeq = append(doneSeq, seq)
	}

	// RFC 6851: send COPYUID in an untagged OK *before* the EXPUNGEs, so a client
	// never sees a message expunged before it learns the copy's UID.
	if hasMover && len(dstUIDs) > 0 {
		if v, ok := s.uidValidity(dest); ok {
			s.send("* OK [COPYUID %d %s %s]", v, joinUIDs(srcUIDs), joinUIDs(dstUIDs))
		}
	}

	// Expunge from the cached view in descending order so earlier sequence
	// numbers stay valid as later messages are removed.
	sort.Sort(sort.Reverse(sort.IntSlice(doneSeq)))
	for _, seq := range doneSeq {
		if seq < 1 || seq > len(s.messages) {
			continue
		}
		s.send("* %d EXPUNGE", seq)
		s.messages = append(s.messages[:seq-1], s.messages[seq:]...)
	}
	if s.selected != nil {
		s.selected.total = int64(len(s.messages))
	}
	s.tagged(tag, "OK", cmdName+" completed")
}

// handleUIDExpunge implements UID EXPUNGE (RFC 4315 §2.1): it permanently removes
// only the messages that both carry the \Deleted flag and fall in the given UID
// set, leaving other \Deleted messages in place. It is only reachable when the
// mailbox implements [UIDPlusMailbox].
func (s *Session) handleUIDExpunge(tag, args string) {
	if s.refuseReadOnly(tag) {
		return
	}
	if _, ok := s.mailbox.(UIDPlusMailbox); !ok {
		s.tagged(tag, "BAD", "UID EXPUNGE requires UIDPLUS")
		return
	}
	uidSetStr := strings.TrimSpace(args)
	if uidSetStr == "" {
		s.tagged(tag, "BAD", "UID EXPUNGE requires a uid set")
		return
	}

	targets := make(map[int]bool)
	for _, seq := range s.parseUIDSet(uidSetStr) {
		targets[seq] = true
	}

	// Descending so sequence numbers stay valid as messages are removed.
	for i := len(s.messages) - 1; i >= 0; i-- {
		msg := s.messages[i]
		seq := i + 1
		if !targets[seq] || !s.deleted[msg.UID] {
			continue
		}
		if err := s.mailbox.Delete(msg.UID); err != nil {
			slog.Warn("imap: uid expunge failed", "uid", msg.UID, "error", err)
			continue
		}
		s.send("* %d EXPUNGE", seq)
		s.messages = append(s.messages[:i], s.messages[i+1:]...)
		delete(s.deleted, msg.UID)
	}

	if s.selected != nil {
		s.selected.total = int64(len(s.messages))
	}
	s.tagged(tag, "OK", "UID EXPUNGE completed")
}

// handleUnselect implements UNSELECT (RFC 3691): it returns the session to the
// authenticated state like CLOSE, but does not expunge \Deleted messages.
func (s *Session) handleUnselect(tag string) {
	if s.selected == nil {
		s.tagged(tag, "NO", "No mailbox selected")
		return
	}
	s.selected = nil
	s.readOnly = false
	s.messages = nil
	s.deleted = make(map[uint32]bool)
	s.tagged(tag, "OK", "UNSELECT completed")
}

// handleEnable implements ENABLE (RFC 5161). No extension currently requires
// enabling (CONDSTORE/QRESYNC are future work), so every requested capability is
// unknown and silently ignored per §3.1; the command simply acknowledges. A
// future enable-able extension would echo the enabled names in an untagged
// "* ENABLED ..." response here.
func (s *Session) handleEnable(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}
	s.tagged(tag, "OK", "ENABLE completed")
}

// joinUIDs renders a UID slice as a comma-separated IMAP sequence set, preserving
// order so the nth source UID lines up with the nth destination UID in COPYUID.
func joinUIDs(uids []uint32) string {
	parts := make([]string, len(uids))
	for i, u := range uids {
		parts[i] = strconv.FormatUint(uint64(u), 10)
	}
	return strings.Join(parts, ",")
}
