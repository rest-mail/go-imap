# IMAP RFC checklist — PARTIAL checked foundation

This is an explicitly **PARTIAL** checklist: the first RFC 3501 rows for the
connection greeting, the any-state commands (CAPABILITY, NOOP, LOGOUT), LOGIN,
tagged completion, a selection-state slice (SELECT/EXAMINE) and a STORE
slice. It is not an inventory of all applicable RFC requirements, a full
conformance claim, or a coverage badge. A mapped row means its named wire test
in `internal/rfctest` asserts the stated behavior through the public
`Session`; it does not prove the entire referenced section.

Requirements below paraphrase [RFC 3501](https://www.rfc-editor.org/rfc/rfc3501.txt).
`Normative` denotes protocol text without an explicit RFC 2119 keyword.
CI runs `go run ./internal/checklist internal/rfctest`, which fails on an item
with no test, a test naming an unknown item, or a duplicate ID.

## RFC 3501: connection, any-state commands and LOGIN

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-GREET-01 | RFC 3501 §§3.1, 7.1.1 | Normative | Greet with an untagged OK, which starts the not authenticated state; refuse SELECT before authentication. | TestGreeting |
| IMAP-CAP-01 | RFC 3501 §6.1.1 | MUST | Send a single untagged CAPABILITY response listing IMAP4rev1 before the tagged OK, in every state. | TestCapability |
| IMAP-NOOP-01 | RFC 3501 §6.1.2 | Normative | NOOP always succeeds, in any state. | TestNoop |
| IMAP-LOGOUT-01 | RFC 3501 §6.1.3 | MUST | Send an untagged BYE before the tagged OK to LOGOUT, then close the connection. | TestLogout |
| IMAP-LOGIN-01 | RFC 3501 §6.2.3 | Normative | LOGIN with valid credentials returns OK and enters the authenticated state; invalid credentials return NO and leave the state unchanged. | TestLogin |
| IMAP-TAG-01 | RFC 3501 §§2.2.1, 2.2.2 | Normative | The tagged completion result carries the tag of the command it completes. | TestTaggedCompletion |
| IMAP-BAD-01 | RFC 3501 §7.1.3 | Normative | An unknown command receives a tagged BAD. | TestTaggedCompletion |

## RFC 3501: SELECT/EXAMINE — partial selection-state slice

The examples in §§6.3.1/6.3.2 supply the counts, first-unseen sequence numbers
and UID metadata in `TestSelectionExamples`. Flag lists vary with the store.
These fixtures deliberately align the current unread-based RECENT approximation
with the examples; they do not prove independent Recent tracking.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-SELECT-01 | RFC 3501 §6.3.1 | MUST | Send FLAGS and the mailbox's EXISTS count before successful tagged completion of SELECT/EXAMINE. | TestSelectionExamples |
| IMAP-SELECT-02 | RFC 3501 §§6.3.1, 7.1 | Normative | UNSEEN, if present, identifies the first unseen message by sequence number, not the unread count; omit it for an empty or fully seen mailbox. | TestSelectionExamples, TestSelectionUnseen |
| IMAP-SELECT-03 | RFC 3501 §6.3.1 | MUST | Send UIDNEXT before successful SELECT/EXAMINE completion; the examples report a value greater than all existing UIDs. | TestSelectionExamples |
| IMAP-SELECT-04 | RFC 3501 §6.3.1 | MUST | Send the backend's UIDVALIDITY before successful SELECT/EXAMINE completion when UIDPlusMailbox supplies it. | TestSelectionExamples |
| IMAP-SELECT-05 | RFC 3501 §6.3.1 | SHOULD | Identify a writable SELECT with a READ-WRITE response code and send its PERMANENTFLAGS before completion. | TestSelectionExamples |
| IMAP-SELECT-06 | RFC 3501 §§6.3.1, 6.3.2 | Normative | A backend selection failure returns NO and leaves no prior mailbox selected; a subsequent valid selection can recover. | TestSelectionFailureDropsOldMailbox |
| IMAP-SELECT-07 | RFC 3501 §§6.3.1, 6.4.2 | Normative | Selecting another mailbox replaces the old selection without expunging it; old folder-scoped deletion marks must not affect the new mailbox. | TestSelectionSwitchDoesNotExpunge |
| IMAP-SELECT-08 | RFC 3501 §§2.3.2, 6.3.1, 6.3.2 | MUST | Report independent Recent state and preserve it on EXAMINE. | n/a: not supported yet; Message has no Recent field and the engine approximates Recent as unseen. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-SELECT-09 | RFC 3501 §§2.3.1.1, 6.3.1 | MUST | UIDNEXT must track past UID assignments even after messages are expunged; UIDVALIDITY must reflect persistent mailbox identity. | n/a: not supported yet for the base Mailbox interface; UIDNEXT is derived from current messages and UIDVALIDITY defaults to 1 without UIDPlusMailbox. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-SELECT-10 | RFC 3501 §6.3.1 | MUST | SELECT must return READ-ONLY when access permits reading but forbids modification. | n/a: not supported yet; Mailbox exposes no per-folder write permission. Host can refuse Messages with an error but cannot select read-only through SELECT. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-EXAMINE-01 | RFC 3501 §6.3.2 | MUST | Successful EXAMINE starts tagged OK text with READ-ONLY and reports no permanent flags. | TestSelectionExamples |
| IMAP-EXAMINE-02 | RFC 3501 §6.3.2 | Normative | EXAMINE forbids permanent writes through STORE, EXPUNGE and MOVE (including UID variants) and CLOSE; a later SELECT restores writable access. | TestExaminePreventsPermanentWrites |
| IMAP-EXAMINE-03 | RFC 3501 §6.3.2 | Normative | Non-peek FETCH and UID FETCH under EXAMINE return the body without persisting or locally setting Seen. | TestExamineBodyFetchDoesNotSetSeen |

Other selection requirements remain unaudited, including syntax validation,
permanent keyword support, UID exhaustion, backend error/fallback behavior and
concurrent sessions. These rows do not claim full §§6.3.1/6.3.2 conformance.

## RFC 3501: STORE and UID STORE — partial flag-update slice

`TestStoreExample` replays the §6.4.6 `A003 STORE 2:4 +FLAGS (\Deleted)`
example. Flag lists are sets; this engine orders system flags as \Seen,
\Answered, \Flagged, \Draft, then \Deleted.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-STORE-01 | RFC 3501 §6.4.6 | Normative | Without .SILENT, answer each changed message with an untagged FETCH carrying its full resulting flag list before the tagged OK. | TestStoreExample |
| IMAP-STORE-02 | RFC 3501 §6.4.6 | Normative | FLAGS replaces the flag list, +FLAGS adds to it and -FLAGS removes from it; system flags persist through the backend and \Deleted stays visible to later FETCH. | TestStoreExample |
| IMAP-STORE-03 | RFC 3501 §6.4.6 | SHOULD | With .SILENT, apply the change but send no untagged FETCH, for STORE and UID STORE. | TestStoreSilent |
| IMAP-STORE-04 | RFC 3501 §6.4.8 | MUST | An untagged FETCH caused by UID STORE includes the UID data item. | TestUIDStoreReportsUID |
| IMAP-STORE-05 | RFC 3501 §6.4.6 | Normative | A refused backend write answers NO (§6.4.6 "store error") and leaves the session's flags for that message unchanged, for STORE and UID STORE. | TestStoreBackendFailure |
| IMAP-STORE-06 | RFC 3501 §§2.3.2, 6.4.6, 7.1 | Normative | Keywords a client may create (PERMANENTFLAGS lists `\*`) are stored and reported back by FETCH FLAGS. | n/a: not supported yet; FlagUpdate and Message model only system flags, so keywords are accepted and dropped. Tracked in rest-mail/rest-mail-server#374. |

A NO from a multi-message set reports the first refused write; earlier
messages in the set keep their applied changes, which RFC 3501 permits. Flag-list
syntax validation, sequence-set edge cases and STORE under concurrent sessions
remain unaudited.

## RFC 3501: SEARCH and UID SEARCH — partial criteria slice

`TestSearchExample` replays §6.4.4's A282 and A283 examples, with a mailbox
containing exactly the three expected matches and near misses for each A282
predicate. It does not replay A284's UTF-8 literal. Calendar-date tests supply
`Message.Date` as the internal date, including positive/negative timezone
offsets that cross UTC midnight. Hosts must supply that metadata correctly.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-SEARCH-01 | RFC 3501 §§6.4.4, 7.2.5 | MUST | Successful SEARCH sends one untagged SEARCH listing matching sequence numbers before tagged OK; no matches yields an empty SEARCH response. | TestSearchExample, TestSearchRequiresSelection |
| IMAP-SEARCH-02 | RFC 3501 §6.4.4 | Normative | Multiple keys and parenthesized groups intersect their matches; OR takes the union of two operands and NOT complements its operand. | TestSearchExample, TestSearchBooleanKeys |
| IMAP-SEARCH-03 | RFC 3501 §6.4.4 | Normative | ALL matches every message; SEEN/UNSEEN and FLAGGED filter flags; FROM email and SUBJECT match case-insensitive substrings in metadata. | TestSearchExample, TestSearchBooleanKeys |
| IMAP-SEARCH-04 | RFC 3501 §§6.4.8, 7.2.5 | MUST | UID SEARCH applies the same criteria but returns UIDs rather than sequence numbers, including sparse UID assignments. | TestSearchBooleanKeys, TestSearchSequenceAndUIDKeys |
| IMAP-SEARCH-05 | RFC 3501 §§6.4.4, 6.4.8 | Normative | Bare sequence-set keys select sequence numbers even inside UID SEARCH; UID keys select UIDs. Reversed ranges, star and nonexistent UIDs respect the selected mailbox. | TestSearchSequenceAndUIDKeys |
| IMAP-SEARCH-06 | RFC 3501 §6.4.4 | Normative | BEFORE, ON and SINCE compare internal calendar dates, disregarding time and timezone; BEFORE excludes and SINCE includes the specified day. | TestSearchCalendarDates |
| IMAP-SEARCH-07 | RFC 3501 §6.4.4 | MUST | Accept US-ASCII; supported UTF-8 matches UTF-8 subject metadata. Unsupported CHARSET produces tagged NO with BADCHARSET and supported alternatives, without a SEARCH result. | TestSearchCharsets |
| IMAP-SEARCH-08 | RFC 3501 §§6.4.4, 9 | Normative | Require at least one key, including inside groups; reject unknown keys, missing operands, invalid dates and unbalanced groups with tagged BAD and no partial SEARCH result. | TestSearchInvalidCriteria |
| IMAP-SEARCH-09 | RFC 3501 §§3, 6.4 | Normative | SEARCH and UID SEARCH require a selected mailbox; authenticating alone does not permit searching. | TestSearchRequiresSelection |
| IMAP-SEARCH-10 | RFC 3501 §6.4.4 | MUST | Decode MIME transfer encodings and encoded headers to the selected character set before searching. | n/a: not supported yet; BODY/TEXT search raw fetched bytes and header keys use host metadata without MIME/charset decoding. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-SEARCH-11 | RFC 3501 §6.4.4 | Normative | Support the remaining standard search keys, including ANSWERED/UNANSWERED, DRAFT/UNDRAFT, BCC/CC, HEADER, KEYWORD/UNKEYWORD, LARGER/SMALLER and SENTBEFORE/SENTON/SENTSINCE. | n/a: not supported yet; parser rejects these keys; Message does not expose all required metadata. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-SEARCH-12 | RFC 3501 §6.4.4 | Normative | RECENT, NEW and OLD search independent Recent state. | n/a: not supported yet; RECENT approximates unseen, NEW/OLD are rejected and Message has no Recent field. Tracked in rest-mail/rest-mail-server#374. |

This is not a full search grammar or conformance audit. Search literals,
quoted-string escapes and malformed sequence sets remain unaudited; the existing
parser does not reliably validate all of these. FROM display-name matching,
backend Fetch errors, BODY/TEXT matching beyond the empty-result example,
malformed charset data, concurrency, EXPUNGE suppression and the RFC's UID SEARCH
example remain for later slices. No coverage gate, parser fuzz target or
independent implementation comparison is added here.
