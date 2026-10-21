# IMAP RFC checklist — PARTIAL checked foundation

This is an explicitly **PARTIAL** checklist: the first RFC 3501 rows for the
connection greeting, the any-state commands (CAPABILITY, NOOP, LOGOUT), LOGIN,
tagged completion, selection-state (SELECT/EXAMINE), STORE, SEARCH, FETCH,
EXPUNGE, CLOSE and APPEND slices, plus partial RFC 4315 UID EXPUNGE and
RFC 7888 non-synchronizing APPEND and command-argument refusal slices. It is not
an inventory of all applicable RFC requirements, a full conformance claim, or
a coverage badge. A mapped row means its named wire test
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

## RFC 3501: FETCH and UID FETCH — partial data/flag slice

The wire reader consumes literal octet counts independently of CRLF and tagged
text in the message. `TestFetchHeaderExample` uses §6.4.5's A654 command and
three concrete header payloads; `TestUIDFetchExample` reproduces §6.4.8's A999
UID/sequence-number example. This does not inventory all MIME or grammar rules.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-FETCH-01 | RFC 3501 §§4.3, 6.4.5, 7.4.2 | Normative | BODY[] returns the full raw message and BODY[HEADER]/BODY[TEXT] return the header including its blank line / body excluding the header, framed by exact literal octet counts. | TestFetchSections |
| IMAP-FETCH-02 | RFC 3501 §6.4.5 | Normative | Non-peek BODY sections implicitly set Seen in a writable selection and report changed flags; BODY.PEEK leaves flags unchanged and answers with the BODY item name. | TestFetchSections |
| IMAP-FETCH-03 | RFC 3501 §6.4.5 | Normative | RFC822 and RFC822.TEXT behave like BODY[] and BODY[TEXT]; RFC822.HEADER behaves like BODY.PEEK[HEADER], retaining their RFC822 response labels. | TestFetchSections |
| IMAP-FETCH-04 | RFC 3501 §§6.4.5, 7.4.2 | Normative | Partial text fetches slice zero-based octets, truncate at EOF and return empty beyond it; response labels contain only the origin, including zero, while nonpartial responses have no origin. | TestFetchPartialOctets |
| IMAP-FETCH-05 | RFC 3501 §6.4.5 | Normative | HEADER.FIELDS includes and HEADER.FIELDS.NOT excludes exactly matching case-insensitive field names, preserving folded continuations and the delimiting blank line. | TestFetchHeaderExample |
| IMAP-FETCH-06 | RFC 3501 §6.4.5 | Normative | Apply a partial range after header-field subsetting. | TestFetchHeaderExample |
| IMAP-FETCH-07 | RFC 3501 §§2.3.3, 2.3.4, 6.4.5 | Normative | INTERNALDATE and RFC822.SIZE report stored internal-date and octet-size metadata; metadata and peek items do not set Seen. | TestFetchMetadataAndMacros |
| IMAP-FETCH-08 | RFC 3501 §6.4.5 | Normative | FAST expands to FLAGS/INTERNALDATE/RFC822.SIZE; ALL adds ENVELOPE and FULL additionally adds non-extensible BODY. | TestFetchMetadataAndMacros |
| IMAP-FETCH-09 | RFC 3501 §6.4.8 | MUST | UID FETCH responses use sequence-number prefixes and include UID even if not requested; sparse ranges select existing UIDs and nonexistent UIDs cause no error or data response. | TestUIDFetchExample |
| IMAP-FETCH-10 | RFC 3501 §§6.4.5, 6.4.8 | Normative | Failure to load requested message data yields tagged NO, without a partial data response or Seen write for the failed message; earlier completed message responses remain valid. | TestFetchBackendFailure, TestFetchPartialFailure |
| IMAP-FETCH-11 | RFC 3501 §§6.4.5, 6.4.8 | Normative | Refused implicit Seen writes yield NO without advertising or caching the failed change; successful writes precede the response, and already Seen messages need no redundant write. | TestFetchSeenFailure, TestFetchPartialFailure |
| IMAP-FETCH-12 | RFC 3501 §§6.3.2, 6.4.5 | Normative | A non-peek body response after EXAMINE reports unchanged flags rather than a phantom Seen change. | TestFetchExamineEcho |
| IMAP-FETCH-13 | RFC 3501 §§3, 6.4 | Normative | FETCH and UID FETCH require a selected mailbox, including after UNSELECT. | TestFetchRequiresSelection |
| IMAP-FETCH-14 | RFC 3501 §6.4.5 | MUST | Numeric MIME part paths retrieve the designated part, with nested MESSAGE/RFC822 and MIME section rules. | n/a: not supported yet; bodySection falls back to the whole raw message for numeric/unsupported sections. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-FETCH-15 | RFC 3501 §§6.4.5, 9 | Normative | Invalid or unknown data items, malformed section/partial syntax and illegal macro combinations receive BAD. | n/a: not supported yet; unknown/malformed items are ignored, macros in singleton lists are expanded, and lenient partial forms are accepted. Tracked in rest-mail/rest-mail-server#374. |

This slice does not prove full ENVELOPE/BODYSTRUCTURE semantics, static attribute
immutability across sessions, absent-part NIL handling, header-only messages
without a delimiting blank line, sequence-set grammar/overlap behavior, UID
exhaustion or concurrent EXPUNGE suppression. Hosts must provide correct raw
message bytes, octet size, internal date and persistent UID/flag metadata.
Backend data-load failure now refuses ENVELOPE instead of falling back to partial
Message metadata. A failure stops a multi-message command without undoing earlier
successful messages. Network write errors remain best-effort; an accepted Seen
write can outlive a lost connection. Coverage gates, fuzzing, independent
comparison and a release remain separate work under the parent issue.

## RFC 3501: EXPUNGE and CLOSE — partial removal slice

`TestExpungeExample` uses §6.4.3's marked positions 3, 4, 7 and 11. The engine
sends descending sequence numbers 11, 7, 4, 3, which §7.4.1 permits, rather
than the example's ascending-removal responses 3, 3, 5, 8. Tests also check the
remaining backend UIDs and renumbered FETCH replies. `TestCloseExample` uses
the §6.4.2 A341 command/completion, with both marked and unmarked messages.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-EXPUNGE-01 | RFC 3501 §6.4.3 | Normative | Remove all marked messages, leave unmarked messages intact and send one EXPUNGE per successful removal before tagged OK; no matches succeeds without removals. | TestExpungeExample, TestExpungeNoMatches |
| IMAP-EXPUNGE-02 | RFC 3501 §7.4.1 | MUST | Each EXPUNGE reports the removed message's current sequence number, immediately reducing later sequence numbers; surviving messages retain their UIDs. | TestExpungeExample, TestUIDExpungeSubset |
| IMAP-EXPUNGE-03 | RFC 3501 §§6.4.3, 7.1.2 | Normative | A refused deletion returns NO; report successful removals, keep failed messages and their deletion marks, and allow retry without another STORE. | TestExpungeBackendFailure |
| IMAP-EXPUNGE-04 | RFC 3501 §§3, 6.4, 7.1.3 | Normative | EXPUNGE outside the selected state receives BAD and makes no changes. | TestExpungeRequiresSelection |
| IMAP-EXPUNGE-05 | RFC 3501 §§6.4.3, 9 | Normative | EXPUNGE takes no arguments; reject extra arguments with BAD before deletion. | TestExpungeAndCloseArguments |
| IMAP-EXPUNGE-06 | RFC 3501 §6.3.2 | Normative | EXPUNGE cannot permanently remove messages from a mailbox selected read-only by EXAMINE. | TestReadOnlyCloseAndExpunge |
| IMAP-CLOSE-01 | RFC 3501 §§3, 6.4, 7.1.3 | Normative | CLOSE outside the selected state receives BAD. | TestExpungeRequiresSelection |
| IMAP-CLOSE-02 | RFC 3501 §§6.4.2, 9 | Normative | CLOSE takes no arguments; invalid arguments receive BAD without deletion or deselection. | TestExpungeAndCloseArguments |
| IMAP-CLOSE-03 | RFC 3501 §6.4.2 | Normative | Successful writable CLOSE removes marked messages without untagged EXPUNGE, enters authenticated state and clears old selection marks before reselection. | TestCloseExample |
| IMAP-CLOSE-04 | RFC 3501 §6.4.2 | Normative | CLOSE after EXAMINE succeeds silently and deselects without removing messages. | TestReadOnlyCloseAndExpunge |
| IMAP-CLOSE-05 | RFC 3501 §6.4.2 | Normative | LOGOUT implicitly closes a selected mailbox without expunging marked messages. | TestLogoutDoesNotExpunge |
| IMAP-CLOSE-06 | RFC 3501 §6.4.2 | Normative | Writable CLOSE removes every marked message before reporting successful completion. | n/a: not supported yet on backend deletion failure; CLOSE ignores Delete errors and deselects with OK. The mapped success case above assumes all deletes succeed. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-EXPUNGE-07 | RFC 3501 §§2.3.2, 6.4.3 | Normative | The Deleted flag is permanent mailbox state visible across sessions until removal. | n/a: not supported yet; Message and FlagUpdate have no Deleted field, so marks exist only in the selecting session. Cross-session deletion and concurrent EXPUNGE suppression remain unaudited. Tracked in rest-mail/rest-mail-server#374. |

On EXPUNGE/UID EXPUNGE backend errors, this engine continues trying other
marked messages and returns NO after reporting successful removals. This is
not an atomic multi-message operation. Hosts must make Delete's success/error
accurately reflect whether the message was permanently removed. Disconnected
clients may miss responses after successful backend deletion.

## RFC 4315: UID EXPUNGE — partial UIDPLUS slice

Requirements paraphrase [RFC 4315](https://www.rfc-editor.org/rfc/rfc4315.txt).
`TestUIDExpungeSubset` uses §2.1's `A003 UID EXPUNGE 3000:3002` command with
those three UIDs at positions 3–5; descending responses 5, 4, 3 remove the same
messages as the example's repeated sequence 3. Other UIDPLUS commands and
response codes are not covered by this slice.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-UIDEXPUNGE-01 | RFC 4315 §2.1 | Normative | Remove only messages both marked Deleted and in the specified UID set; other messages and deletion marks are unaffected, and nonexistent UIDs cause no removals. | TestUIDExpungeSubset |
| IMAP-UIDEXPUNGE-02 | RFC 4315 §2.1 | Normative | Backend expunge failure returns NO; successful removals are reported and failed messages remain marked for retry. | TestExpungeBackendFailure |
| IMAP-UIDEXPUNGE-03 | RFC 4315 §2.1; RFC 3501 §6.3.2 | Normative | UID EXPUNGE cannot remove messages from a mailbox selected read-only by EXAMINE. | TestReadOnlyCloseAndExpunge |
| IMAP-UIDEXPUNGE-04 | RFC 4315 §§1, 2.1 | Normative | Without UIDPLUS support or the required UID set, UID EXPUNGE receives BAD without changing deletion marks or messages. | TestUIDExpungeCapabilityAndMissingSet |
| IMAP-UIDEXPUNGE-05 | RFC 4315 §§2.1, 4 | Normative | Invalid UID sequence sets receive BAD before deletion. | n/a: not supported yet; parseUIDSet ignores numeric errors and accepts malformed sets. Strict grammar and boundary checks remain required. Tracked in rest-mail/rest-mail-server#374. |

## RFC 3501: APPEND — partial message-literal slice

`TestAppendExample` sends the §6.3.11 message as exactly 310 octets, using its
A003 tag, destination and Seen flag. The remaining tests check the engine's
callback inputs and wire framing; they do not establish durable storage or
rollback by arbitrary backends. Draft/incomplete message headers are accepted.
The RFC 7888 rows cover the already accepted `{n+}` form only; this engine does
not advertise LITERAL+ and this slice does not claim full extension support.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-APPEND-01 | RFC 3501 §6.3.11 | Normative | Pass the RFC example's literal unchanged to the destination mailbox and return tagged OK after backend success. | TestAppendExample |
| IMAP-APPEND-02 | RFC 3501 §§4.3, 6.3.11 | Normative | Send a continuation before receiving synchronizing APPEND message octets. | TestAppendExample |
| IMAP-APPEND-03 | RFC 3501 §§4.3, 6.3.11 | Normative | Count message octets exactly, including zero-length and 8-bit content; keep command-looking text inside the literal and preserve the next command. | TestAppendLiteralOctets |
| IMAP-APPEND-04 | RFC 3501 §§4.3, 6.3.11 | Normative | Honor a quoted mailbox name and supply no requested flags when the optional flag list is absent. | TestAppendLiteralOctets |
| IMAP-APPEND-05 | RFC 3501 §6.3.11 | SHOULD | Pass explicitly requested Seen, Answered, Flagged and Draft flags to the backend. | TestAppendFlagsAndDate |
| IMAP-APPEND-06 | RFC 3501 §§5.1, 6.3.11 | SHOULD | With DateAppender, pass the explicit INTERNALDATE including its timezone; fold mixed-case INBOX to the canonical mailbox. | TestAppendFlagsAndDate |
| IMAP-APPEND-07 | RFC 3501 §6.3.11 | SHOULD | Notify a selected destination with EXISTS before completion and refresh sequence-number resolution; do not notify an unrelated selection. | TestAppendSelectedMailbox |
| IMAP-APPEND-08 | RFC 3501 §§3.3, 6.3.2, 6.3.11 | Normative | APPEND remains available in selected state, including after EXAMINE: read-only selection does not prohibit appending to a mailbox. | TestAppendSelectedMailbox |
| IMAP-APPEND-09 | RFC 3501 §6.3.11 | Normative | Backend append failure produces NO without a success notification; a later retry can succeed. | TestAppendBackendFailure, TestAppendNonSynchronizingBackendFailure |
| IMAP-APPEND-10 | RFC 3501 §§6.3.11, 9 | Normative | Invalid terminal literal declarations and extra arguments receive BAD without inviting message bytes or calling the backend. | TestAppendInvalidArguments |
| IMAP-APPEND-11 | RFC 3501 §§4.3, 6.3.11 | Normative | Refuse unsupported flags and oversized synchronizing message literals before sending a continuation, preserving command synchronization. | TestAppendRefusedBeforeContinuation |
| IMAP-APPEND-12 | RFC 7888 §3 | Normative | For an accepted non-synchronizing APPEND message literal, send no continuation and preserve a following pipelined command. | TestAppendNonSynchronizing, TestAppendNonSynchronizingBackendFailure |
| IMAP-APPEND-13 | RFC 3501 §§2.2, 6.3.11, 9 | Normative | Only a CRLF after the counted message completes APPEND; reject malformed termination without backend delivery and close the ambiguous stream. | TestAppendMalformedTerminator |
| IMAP-APPEND-14 | RFC 3501 §§2.2, 6.3.11, 9 | Normative | Disconnect before all message bytes and the command terminator arrive cannot deliver an incomplete APPEND to the backend. | TestAppendDisconnectBeforeCompletion |
| IMAP-APPEND-15 | RFC 3501 §2.3.2 | Normative | Treat supported system flag names case-insensitively on APPEND. | TestAppendFlagsCaseInsensitive |
| IMAP-APPEND-16 | RFC 3501 §§3.1, 6.3 | Normative | Refuse APPEND before authentication without inviting message bytes; permit it after successful LOGIN. | TestAppendRequiresAuthentication |
| IMAP-APPEND-17 | RFC 3501 §6.3.11 | MUST | Restore the mailbox after an unsuccessful append with no partially stored message. | n/a: the backend must append atomically; Mailbox.Append offers no rollback interface and the engine cannot undo a backend that mutates before returning an error. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-APPEND-18 | RFC 3501 §6.3.11 | MUST | Reject a nonexistent destination without implicitly creating it and supply TRYCREATE unless creation is known to be impossible. | n/a: not supported yet; folders are implicit, and the engine neither checks existence nor distinguishes nonexistent destinations from other backend errors. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-APPEND-19 | RFC 3501 §6.3.11 | Normative | Set Recent on the new message and preserve requested Deleted and keyword flags. | n/a: not supported yet; Recent is approximated from unread messages, FlagUpdate has no Deleted or keyword fields, and APPEND accepts but drops Deleted. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-APPEND-20 | RFC 3501 §6.3.11 | SHOULD | Honor explicit INTERNALDATE and use current time by default for every backend. | n/a: explicit dates reach only DateAppender; other backends drop them, and default timestamps are assigned by the host rather than the engine. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-APPEND-21 | RFC 3501 §§4.3, 6.3.11 | Normative | Accept a literal mailbox-name astring before the message literal. | n/a: not supported yet; APPEND bypasses readLiteralArgs and only parses atom or quoted mailbox names. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-APPEND-22 | RFC 7888 §§3–4 | MUST | For a syntactically valid non-synchronizing literal marker, early APPEND refusal sends BYE and ends the session without dispatching unread octets; backend refusal after consuming the complete command preserves synchronization. | TestAppendRejectedNonSynchronizing, TestAppendNonSynchronizingBackendFailure |
| IMAP-APPEND-23 | RFC 3501 §§4.3, 9 | Normative | Enforce the complete APPEND grammar, including separators, quoted escapes and legal CHAR8 message bytes. | n/a: not supported yet; the shared parsers are permissive about separators and quoting, and message bytes are not checked for NUL. This slice validates only the terminal message literal and its CRLF. Tracked in rest-mail/rest-mail-server#374. |

Early APPEND refusals with a valid `{n+}` marker choose RFC 7888 §4's
BYE-and-close policy instead of draining potentially large payloads. This
includes malformed preceding arguments and unauthenticated commands. Invalid
markers outside the unsigned 32-bit literal grammar are not a supported
non-synchronizing form. Initial overlong command lines still need their own
refusal audit. LITERAL+ remains unadvertised.

## RFC 3501 / RFC 7888: command-argument literals — partial refusal slice

These rows cover the existing non-APPEND literal materialization path and its
resource limits. They do not prove full command grammar or LITERAL+ support.
RFC 7888 §§3–4 permit BYE-and-close instead of consuming refused in-flight
literal data. Once a continuation line exceeds the line cap, its suffix and
any next literal marker are lost; this path closes conservatively as well.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-LITERAL-01 | RFC 3501 §4.3 | Normative | Refuse oversized synchronizing argument literals before requesting their octets, including aggregate limits across multiple literals; preserve the next command. | TestCommandLiteralSafeRefusal |
| IMAP-LITERAL-02 | RFC 7888 §§3–4 | MUST | Refusing an individual or aggregate oversized non-synchronizing argument literal sends BYE and ends the session without dispatching unread payload. | TestCommandLiteralRefusalCloses |
| IMAP-LITERAL-03 | RFC 7888 §§3–4 | MUST | Refusing an oversized assembled command with another non-synchronizing argument literal sends BYE and ends the session without dispatching that literal's unread payload. | TestCommandLiteralRefusalCloses |
| IMAP-LITERAL-04 | RFC 7888 §3 | MUST | Reject an overlong literal-continuation line with BYE and end the session because a discarded trailing marker may leave in-flight literal octets. | TestCommandLiteralRefusalCloses |
| IMAP-LITERAL-05 | RFC 3501 §§2.2.1, 4.3 | Normative | Refusing oversized assembled arguments preserves the next command if no literal remains unread or the next literal is synchronizing and no continuation has been sent. | TestCommandLiteralSafeRefusal |
| IMAP-LITERAL-06 | RFC 3501 §4.3; RFC 7888 §3 | Normative | Count accepted LOGIN argument octets, support empty literals, send continuations only for synchronizing forms, and preserve a pipelined next command. | TestCommandLiteralAccepted |
| IMAP-LITERAL-07 | RFC 3501 §§2.2.1, 4.3 | Normative | A read error before all argument octets or the continuation line arrive ends the session without dispatching the incomplete command or unread suffix. | TestCommandLiteralReadFailureEnds |

Initial command-line overflow, malformed markers, complete quoting/separator
and CHAR8 validation, binary literals, literal mailbox names in APPEND, and
other extension interactions remain unaudited or unsupported. In particular,
this slice does not fix initial overlong lines discarding a `{n+}` suffix.
The accepted non-synchronizing form remains unadvertised compatibility behavior.
