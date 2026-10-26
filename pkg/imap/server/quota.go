package server

import (
	"strings"

	"github.com/rest-mail/go-imap/pkg/imap/internal/wire"
)

// handleGetQuota returns quota for a named quota root (RFC 2087).
func (s *Session) handleGetQuota(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	used, limit, err := s.mailbox.Quota()
	if err != nil {
		s.tagged(tag, "NO", "Failed to get quota")
		return
	}

	// Report in KB (IMAP QUOTA uses 1024-byte units)
	s.send("* QUOTA \"\" (STORAGE %d %d)", used/1024, limit/1024)
	s.tagged(tag, "OK", "GETQUOTA completed")
}

// handleGetQuotaRoot returns the quota root for a mailbox (RFC 2087).
func (s *Session) handleGetQuotaRoot(tag, args string) {
	if !s.auth.authenticated {
		s.tagged(tag, "NO", "Not authenticated")
		return
	}

	mailbox := strings.Trim(args, "\" ")
	if mailbox == "" {
		s.tagged(tag, "BAD", "Missing mailbox name")
		return
	}
	folder, ok := s.folderName(mailbox)
	if !ok {
		s.tagged(tag, "NO", "No such mailbox")
		return
	}

	used, limit, err := s.mailbox.Quota()
	if err != nil {
		s.tagged(tag, "NO", "Failed to get quota")
		return
	}

	s.send("* QUOTAROOT %s \"\"", wire.EncodeMailboxName(s.wireName(folder)))
	s.send("* QUOTA \"\" (STORAGE %d %d)", used/1024, limit/1024)
	s.tagged(tag, "OK", "GETQUOTAROOT completed")
}
