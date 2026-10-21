# IMAP RFC checklist — PARTIAL checked foundation

This is an explicitly **PARTIAL** checklist: the first RFC 3501 rows for the
connection greeting, the any-state commands (CAPABILITY, NOOP, LOGOUT), LOGIN,
tagged completion, STARTTLS, selection-state (SELECT/EXAMINE), STORE, SEARCH, FETCH/ENVELOPE,
EXPUNGE, CLOSE, APPEND, COPY and encoded BODYSTRUCTURE slices, plus partial
RFC 4315 UID EXPUNGE/COPYUID,
RFC 6851 MOVE and RFC 7888 non-synchronizing APPEND and command-argument refusal slices. It is not
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

## RFC 3501 / RFC 2595: STARTTLS — partial negotiation slice

`TestStartTLSExample` follows the RFC 3501 §6.2.1 example transcript over a real
TLS handshake on the public `Session`, configured with a TLS config. Implicit TLS
(port 993) is set only by `Server` and is not covered here.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-STARTTLS-01 | RFC 3501 §§6.1.1, 6.2.3, 7.2.1; RFC 2595 §3.2 | MUST | With TLS configured, a cleartext session advertises STARTTLS and LOGINDISABLED and withholds AUTH=PLAIN; LOGIN gets tagged NO even with valid credentials. AUTHENTICATE is likewise refused before any continuation, and no credentials reach the backend. | TestStartTLSExample |
| IMAP-STARTTLS-02 | RFC 3501 §6.2.1 | Normative | STARTTLS returns tagged OK and TLS negotiation begins immediately after its CRLF, with no further octets in cleartext. | TestStartTLSExample |
| IMAP-STARTTLS-03 | RFC 3501 §6.2.1 | MAY | After STARTTLS the server advertises different capabilities: STARTTLS and LOGINDISABLED disappear, AUTH=PLAIN appears and LOGIN succeeds. | TestStartTLSExample |
| IMAP-STARTTLS-04 | RFC 3501 §6.2.1; RFC 2595 §3.1 | Normative | The session remains not authenticated after TLS negotiation: SELECT is refused and no credentials are checked until LOGIN. | TestStartTLSNonAuthenticated |
| IMAP-STARTTLS-05 | RFC 3501 §§6.2.1, 9; RFC 2595 §3.1 | Normative | STARTTLS is refused with BAD when TLS is not configured, when arguments are supplied or when TLS is already active, including in the authenticated state; the session stays usable. | TestStartTLSRefusals |
| IMAP-STARTTLS-06 | RFC 3501 §6.2.1 | Normative | Cleartext pipelined behind STARTTLS is discarded and never runs inside the TLS layer (CVE-2011-0411 class). | TestStartTLSDiscardsPipelinedPlaintext |
| IMAP-STARTTLS-07 | RFC 3501 §6.2.1 | Normative | A failed TLS negotiation ends the session; cleartext sent after the STARTTLS OK never runs as commands. | TestStartTLSFailedNegotiation |
| IMAP-STARTTLS-08 | RFC 3501 §6.2.3 | SHOULD NOT | Do not permit LOGIN without protection against password snooping. | n/a: deployment responsibility; with no TLS config (development) the session advertises AUTH=PLAIN and accepts cleartext LOGIN. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-STARTTLS-09 | RFC 2595 §3.1; RFC 3501 §6.2.1 | MAY | Authenticate with SASL EXTERNAL from TLS client credentials. | n/a: not supported; only AUTH=PLAIN is offered. Tracked in rest-mail/rest-mail-server#374. |

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
non-synchronizing form. An overlong initial APPEND line ending in `+}` is
covered by IMAP-LITERAL-08. LITERAL+ remains unadvertised.

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
| IMAP-LITERAL-08 | RFC 7888 §§3–4 | MUST | An overlong initial command line ending in `+}` (a possible non-synchronizing marker) is refused with BAD, then BYE, and the session ends without dispatching in-flight octets, however the marker and terminator split across reads. | TestCommandLineOverflowLiteralCloses |
| IMAP-LITERAL-09 | RFC 3501 §§2.2.1, 4.3 | Normative | An overlong initial command line with no marker, or a synchronizing marker whose continuation was never sent, is refused with BAD and the next command still runs. | TestCommandLineOverflowSafeRefusal |
| IMAP-LITERAL-10 | RFC 3501 §§6.2.2, 9; RFC 4959 | Normative | An overlong AUTHENTICATE response is refused with tagged BAD before any credential check and the next command still runs, whatever the line ends in: a base64 response is not a literal position, so a trailing `{n}` or `{n+}` leaves nothing in flight. | TestAuthenticateResponseOverflow |
| IMAP-LITERAL-11 | RFC 2177 §3 | Normative | An overlong line while idling is ignored without ending IDLE, whatever it ends in: only DONE is legal input there, so no literal can be pending. A later command-shaped line is still not run, DONE ends IDLE, and the next command runs. | TestIdleLineOverflow |

The initial overlong line is discarded in constant memory, so only its last
octets are kept: any line ending in `+}` closes, even when the discarded text
would not parse as a valid marker. Malformed markers, complete quoting/separator
and CHAR8 validation, binary literals, literal mailbox names in APPEND, and
other extension interactions remain unaudited or unsupported. Overlong
AUTHENTICATE responses and IDLE lines keep the session because neither position
admits a literal (IMAP-LITERAL-10/11); a client that sends literal octets there
anyway is outside the grammar, the same as for a short line.
The accepted non-synchronizing form remains unadvertised compatibility behavior.

## RFC 3501 / RFC 4315: COPY and UID COPY — partial dispatch slice

`TestCopyExample` replays RFC 3501 §6.4.7's A003 command. The source UIDs and
assigned destination UIDs in `TestCopyUIDExample` reproduce RFC 4315 §3's
A004/A005 examples; comma lists are equivalent to ranges in COPYUID. The backend
fixture records dispatch and refuses selected messages. It does not prove a real
store preserves content/metadata or atomically copies a whole set.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-COPY-01 | RFC 3501 §6.4.7 | Normative | COPY passes each selected sequence number's UID and the destination to the backend, then returns tagged OK without command-specific untagged responses. Base backends omit COPYUID. | TestCopyExample |
| IMAP-COPY-02 | RFC 3501 §6.4.7 | Normative | COPY leaves source membership and source flags unchanged in the selected session. | TestCopyExample |
| IMAP-COPY-03 | RFC 4315 §3 | SHOULD | Successful COPY/UID COPY with destination UID metadata returns tagged OK with COPYUID containing destination UIDVALIDITY and corresponding source/destination UIDs in copy order. | TestCopyUIDExample, TestCopyUIDMapping |
| IMAP-COPY-04 | RFC 3501 §6.4.8; RFC 4315 §3 | Normative | UID COPY uses UIDs rather than sequence numbers and ignores nonexistent UIDs; an empty match succeeds without copying or reporting COPYUID. | TestCopyUIDExample, TestCopySets |
| IMAP-COPY-05 | RFC 3501 §§2.3.1.1, 6.4.8 | Normative | Reversed ranges denote the same members; a UID range ending in star includes the last message even if its other endpoint exceeds the greatest UID. | TestCopySets |
| IMAP-COPY-06 | RFC 3501 §9; RFC 4315 §3 | Normative | Overlapping sequence-set members identify each message once; UID COPY must not duplicate backend calls or COPYUID members. | TestCopySets, TestCopyUIDMapping |
| IMAP-COPY-07 | RFC 3501 §§6.4.7, 6.4.8 | Normative | A backend copy failure returns tagged NO without COPYUID and stops further dispatch; the session accepts a later retry. This does not establish rollback of earlier copies. | TestCopyBackendFailure |
| IMAP-COPY-08 | RFC 3501 §§5.1, 6.3.2, 6.4.7 | Normative | COPY/UID COPY may copy from an EXAMINE selection; quoted destination names retain spaces and INBOX is case-insensitive. | TestCopyReadOnlyAndNames |
| IMAP-COPY-09 | RFC 3501 §§3, 6.4 | Normative | COPY/UID COPY require a selected mailbox and dispatch no backend copy before selection. | TestCopyRequiresSelection |
| IMAP-COPY-10 | RFC 4315 §3 | Normative | Omit COPYUID when the destination UIDVALIDITY is unavailable rather than reporting guessed metadata; the completed copy still succeeds. | TestCopyUIDValidityFallback |
| IMAP-COPY-11 | RFC 3501 §6.4.7 | MUST | An unsuccessful COPY restores the destination to its state before the attempt. | n/a: not supported yet; per-message Copy/CopyUID cannot roll back earlier successful copies or a failed backend mutation. A batch transaction/rollback backend API is needed. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-COPY-12 | RFC 3501 §6.4.7 | SHOULD | Preserve flags and internal date and set Recent in the destination copy. | n/a: backend storage responsibility, not verified by these dispatch fixtures; session-local Deleted and keywords cannot be supplied through Copy. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-COPY-13 | RFC 3501 §6.4.7 | MUST | Refuse nonexistent destinations without auto-creation and prefix NO with TRYCREATE unless creation is known impossible. | n/a: not supported yet; backend errors expose neither missing destinations nor creation permissions, so COPY returns generic NO. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-COPY-14 | RFC 3501 §§6.4.7, 9 | Normative | Reject invalid sequence sets and mailbox argument syntax with BAD. | n/a: not supported yet; shared parsers accept malformed sequence sets and destination argument text. Full grammar, quoted escapes and NUL handling remain unaudited. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-COPY-15 | RFC 4315 §§3, 5 | SHOULD NOT | Suppress COPYUID for a destination the user cannot SELECT/EXAMINE and for UIDNOTSTICKY stores. | n/a: not supported yet; backend API exposes no destination readability or UIDNOTSTICKY marker. Hosts can suppress metadata by returning zero/error UIDValidity, but that policy is not independently checked here. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-COPY-16 | RFC 4315 §§3, 4 | Normative | COPYUID contains only valid nonzero destination UIDs, with no extraneous or repeated UIDs. | n/a: not supported yet for invalid backend results; CopyUID return values are trusted, including zero/repeated UIDs. Tracked in rest-mail/rest-mail-server#374. |

This slice stops at the first backend failure and suppresses COPYUID on NO;
earlier destination writes can remain and a retry may duplicate them. This is an
explicit RFC rollback gap, not atomic COPY conformance. Concurrent updates,
copy-to-self notifications and the remaining UIDPLUS requirements remain for
later slices. No batch backend interface or public capability changes are made.

## RFC 6851: MOVE and UID MOVE — partial dispatch slice

`TestMoveUIDExample` follows the shape of RFC 6851's UID MOVE example:
COPYUID in an untagged OK, then one EXPUNGE per moved message, then the tagged
OK. Fixtures record `Mailbox.Move` / `Mover.MoveUID` dispatch and refuse selected
messages. They do not prove a real store moves each message atomically.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-MOVE-01 | RFC 6851 §3.1 | Normative | MOVE passes each selected message's UID and the destination to the backend, sends an untagged EXPUNGE for each moved message and returns tagged OK; moved messages leave the selected view. Base backends omit COPYUID. | TestMoveExample |
| IMAP-MOVE-02 | RFC 6851 §§3.3, 4; RFC 4315 §3 | SHOULD | With UIDPLUS and destination UID metadata, send COPYUID in an untagged OK before any EXPUNGE, with corresponding source/destination UIDs in move order. | TestMoveUIDExample, TestMoveUIDMapping |
| IMAP-MOVE-03 | RFC 6851 §3.2; RFC 3501 §§2.3.1.1, 6.4.8 | Normative | UID MOVE uses UIDs and ignores nonexistent UIDs; reversed ranges and star denote the same members; an empty match succeeds without EXPUNGE or COPYUID. | TestMoveUIDExample, TestMoveSets |
| IMAP-MOVE-04 | RFC 3501 §9; RFC 6851 §3.3 | Normative | Overlapping sequence-set members identify each message once: one backend move, one EXPUNGE and one COPYUID member per message, and no unrelated message leaves the view. | TestMoveSets, TestMoveUIDMapping |
| IMAP-MOVE-05 | RFC 6851 §3.3 | MUST | A backend move failure stops dispatch and returns tagged NO; messages already moved are still reported (COPYUID when available, then EXPUNGE) and unmoved messages stay in the source view; the session accepts a later retry. | TestMoveBackendFailure |
| IMAP-MOVE-06 | RFC 6851 §3.1; RFC 3501 §6.3.2 | Normative | MOVE/UID MOVE expunge the source, so they are refused with NO on an EXAMINE selection without dispatching a backend move. | TestMoveReadOnly |
| IMAP-MOVE-07 | RFC 6851 §3.1; RFC 3501 §§3, 6.4 | Normative | MOVE/UID MOVE require a selected mailbox and dispatch no backend move before selection. | TestMoveRequiresSelection |
| IMAP-MOVE-08 | RFC 3501 §5.1; RFC 6851 §3.1 | Normative | Quoted destination names retain spaces and INBOX is case-insensitive. | TestMoveNames |
| IMAP-MOVE-09 | RFC 4315 §3; RFC 6851 §4 | Normative | Omit COPYUID when destination UIDVALIDITY is unavailable or the backend has no atomic Mover; the completed move still succeeds. | TestMoveUIDValidityFallback |
| IMAP-MOVE-10 | RFC 6851 §3.3 | SHOULD | Each message is either moved or unaffected, never left in both mailboxes; no message is lost. | n/a: backend responsibility; the base `Mailbox.Move` fallback and `Mover.MoveUID` are trusted per message and not verified by these dispatch fixtures. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-MOVE-11 | RFC 6851 §3.3; RFC 3501 §6.4.7 | MUST | Refuse nonexistent destinations without auto-creation and prefix NO with TRYCREATE unless creation is known impossible. | n/a: not supported yet; backend errors do not expose missing destinations, so MOVE returns generic NO. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-MOVE-12 | RFC 6851 §3.1; RFC 3501 §9 | Normative | Reject invalid sequence sets and mailbox argument syntax with BAD. | n/a: not supported yet; shared parsers accept malformed sequence sets and destination argument text. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-MOVE-13 | RFC 6851 §4 | Normative | With QRESYNC enabled, report moved messages with VANISHED instead of EXPUNGE; other sessions selecting the source learn of the removal. | n/a: not supported yet; QRESYNC is not implemented and other sessions are not notified. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-MOVE-14 | RFC 4315 §§3, 4 | Normative | COPYUID contains only valid nonzero destination UIDs, with no extraneous or repeated UIDs. | n/a: not supported yet for invalid backend results; MoveUID return values are trusted. Tracked in rest-mail/rest-mail-server#374. |

A refused move stops at the failing message and reports only what the backend
actually moved. No rollback is attempted: RFC 6851 permits partial moves as long
as every message ends up in exactly one mailbox, which is the backend's job.

## RFC 3501: BODY and BODYSTRUCTURE — partial encoded-part slice

The two §7.4.2 example structures are reproduced using synthetic messages with
exactly the RFC's octet/line dimensions. The RFC provides structures, not raw
messages; these are not borrowed message test vectors. Exact public-Session
responses check both FETCH and UID FETCH under SELECT and EXAMINE. Encoded body
fixtures specify their own octet and physical-line counts independently of the
MIME parser; quoted-printable soft breaks, invalid escapes, empty bodies, final
CRLF and nested binary attachments guard against accidental decoding.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-BS-01 | RFC 3501 §7.4.2 | Normative | Reproduce the simple text and two-part text/base64 example structures, with the stated octet and line counts. | TestBodyStructureRFCExample, TestBodyStructureRFCMultipartExample |
| IMAP-BS-02 | RFC 3501 §7.4.2 | Normative | Basic single-part fields appear in order: type, subtype, parameters, ID, description, transfer encoding and encoded octet size. | TestBodyStructureRFCExample, TestBodyStructureTransferEncoding, TestBodyStructureNestedEncoding, TestBodyStructureRFCMultipartExample |
| IMAP-BS-03 | RFC 3501 §7.4.2 | Normative | TEXT adds a line count measured in its content transfer encoding, including soft-break lines rather than the decoded text's lines. | TestBodyStructureRFCExample, TestBodyStructureTransferEncoding, TestBodyStructureRFCMultipartExample |
| IMAP-BS-04 | RFC 3501 §7.4.2 | Normative | Multipart children retain their transfer encoding and encoded dimensions; nested multipart structures precede the subtype, and non-text/non-message parts have no text-line field. | TestBodyStructureTransferEncoding, TestBodyStructureNestedEncoding, TestBodyStructureRFCMultipartExample |
| IMAP-BS-05 | RFC 3501 §7.4.2 | MUST NOT | BODY omits extension data; BODYSTRUCTURE appends single-part MD5/disposition/language/location or multipart parameters/disposition/language/location in that order. | TestBodyStructureTransferEncoding, TestBodyStructureNestedEncoding, TestBodyStructureRFCMultipartExample |
| IMAP-BS-06 | RFC 3501 §§6.4.5, 6.4.8 | Normative | BODY and BODYSTRUCTURE metadata do not set Seen, work under EXAMINE, and describe the same message through UID FETCH; requesting both shares one raw-message load. | TestBodyStructureRFCExample, TestBodyStructureTransferEncoding, TestBodyStructureNestedEncoding, TestBodyStructureRFCMultipartExample |
| IMAP-BS-07 | RFC 3501 §7.4.2 | Normative | Preserve Content-ID, Content-Description and MIME parameters; render attachment and multipart disposition types with their parameters in the extension slot. | TestBodyStructureNestedEncoding, TestBodyStructureRFCMultipartExample |
| IMAP-BS-08 | RFC 3501 §7.4.2 | MAY | Return MD5, language and location extension data derived from the message. | n/a: not supported yet; these extension slots are always NIL even when source headers exist. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-BS-09 | RFC 3501 §§7.4.2, 9 | Normative | MESSAGE/RFC822 carries its encapsulated envelope, body structure and line count. | TestMessageStructureRFCEnvelope, TestMessageStructureHeaderDefaults, TestMessageStructureScalarStrings, TestMessageStructureRecursiveBody, TestMessageStructureNestedMessage |
| IMAP-BS-10 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Encode every body string using valid IMAP string framing for the source octets. | n/a: not supported yet for arbitrary MIME/header values; existing quoted-string paths do not provide literal framing for every field. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-BS-11 | RFC 3501 §7.4.2 | Normative | Default omitted MIME fields according to MIME rules, including defaults that depend on the enclosing multipart subtype. | n/a: not fully audited; multipart/digest child defaults and fabricated text/plain fallback structures need independent verification. This slice checks valid multipart framing and opaque transfer-encoded bodies only. Tracked in rest-mail/rest-mail-server#374. |

This slice preserves quoted-printable source data via raw MIME part traversal.
It does not validate or decode transfer encodings, expose numbered MIME FETCH
sections, or claim complete BODYSTRUCTURE/MIME conformance.

## RFC 3501: ENVELOPE — partial header slice

`TestEnvelopeExample` uses the message headers and ENVELOPE in RFC 3501 §8
(a003/a004). The full ten-field response is checked through public `Session`
FETCH and UID FETCH. Cached metadata intentionally disagrees with the raw
headers. Empty/missing headers are also exercised as robustness cases, even
where RFC 2822 requires a nonempty Date or From.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-ENV-01 | RFC 3501 §§7.4.2, 8 | Normative | Return ten fields in date, subject, from, sender, reply-to, to, cc, bcc, in-reply-to, message-id order, including the RFC example. | TestEnvelopeExample, TestEnvelopeDistinctFields |
| IMAP-ENV-02 | RFC 3501 §§2.3.3, 2.3.5, 7.4.2 | Normative | Compute date and subject from raw headers, retaining header text and unfolding continuations; do not substitute INTERNALDATE or a cached subject. | TestEnvelopeExample, TestEnvelopeDistinctFields, TestEnvelopeAbsentAndEmpty, TestEnvelopeScalarStrings |
| IMAP-ENV-03 | RFC 3501 §7.4.2 | Normative | Address lists preserve mailbox order and personal-name/mailbox/host components; absent or empty From, To, Cc and Bcc are NIL, regardless of cached metadata. | TestEnvelopeExample, TestEnvelopeDistinctFields, TestEnvelopeAbsentAndEmpty |
| IMAP-ENV-04 | RFC 3501 §7.4.2 | Normative | Explicit Sender and Reply-To take their own address lists rather than From. | TestEnvelopeExample, TestEnvelopeDistinctFields |
| IMAP-ENV-05 | RFC 3501 §§6.4.5, 6.4.8, 7.4.2 | Normative | FETCH and UID FETCH return ENVELOPE without setting or persisting Seen. | TestEnvelopeExample, TestEnvelopeDistinctFields, TestEnvelopeAbsentAndEmpty, TestEnvelopeScalarStrings, TestEnvelopeReadOnlyAndSharedLoad |
| IMAP-ENV-06 | RFC 3501 §7.4.2 | Normative | Absent scalar headers are NIL; present empty Date, Subject, In-Reply-To and Message-ID are empty strings. | TestEnvelopeAbsentAndEmpty |
| IMAP-ENV-07 | RFC 3501 §7.4.2 | Normative | Absent or empty Sender and Reply-To default to the From member, including when From is NIL. | TestEnvelopeAbsentAndEmpty |
| IMAP-ENV-08 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Scalar header strings escape quote/backslash and use counted literals for 8-bit octets; encoded words remain header text. | TestEnvelopeScalarStrings |
| IMAP-ENV-09 | RFC 3501 §§2.3.3, 6.3.2, 6.4.5 | Normative | EXAMINE ENVELOPE remains read-only, INTERNALDATE remains arrival metadata and repeated items share a raw read. | TestEnvelopeReadOnlyAndSharedLoad |
| IMAP-ENV-10 | RFC 3501 §7.4.2 | Normative | Preserve RFC 2822 groups with group-start and group-end address structures. | n/a: not supported yet; net/mail flattens groups and drops empty groups, so group boundaries are lost. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-ENV-11 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Preserve source routes and legal address syntax, with literal framing for address components that quoted strings cannot carry. | n/a: not supported yet; source routes are always NIL, net/mail rejects some obsolete syntax, and addressPart still uses quoted strings for all nonempty components. This slice only frames scalar header strings. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-ENV-12 | RFC 3501 §§4.3.1, 7.4.2 | Normative | Treat malformed header blocks and prohibited NUL octets consistently without fabricating valid header information. | n/a: malformed header parsing still falls back to the Message summary; NUL header policy is not defined and the scalar encoder can emit NUL in a literal. Full malformed-message and binary handling remain unaudited. Tracked in rest-mail/rest-mail-server#374. |

The ENVELOPE rows above cover top-level FETCH. The MESSAGE/RFC822 slice below
checks the separate nested-envelope helper, sharing the scalar header encoder.
The scalar literal encoder also accepts malformed raw 8-bit header text for
compatibility; this does not advertise or implement UTF8=ACCEPT.

## RFC 3501: MESSAGE/RFC822 — partial encapsulation slice

These public-Session fixtures place encapsulated messages both at the top level
and inside a multipart attachment, requesting BODY and BODYSTRUCTURE together
through FETCH/UID FETCH under SELECT/EXAMINE. The RFC §8 ENVELOPE headers are
reused inside a synthetic wrapper; the RFC supplies no corresponding raw
MESSAGE/RFC822 vector. Line counts are explicitly specified per fixture.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-MSG-01 | RFC 3501 §§7.4.2, 9 | Normative | MESSAGE/RFC822 appends the encapsulated envelope, body structure and whole-message text-line count immediately after the basic fields; octet size includes the encapsulated headers and body. | TestMessageStructureRFCEnvelope, TestMessageStructureHeaderDefaults, TestMessageStructureScalarStrings, TestMessageStructureRecursiveBody, TestMessageStructureNestedMessage |
| IMAP-MSG-02 | RFC 3501 §7.4.2 | MUST NOT | BODY omits extension data recursively; BODYSTRUCTURE keeps single-part extensions after the encapsulated envelope/body/line fields, including attachment disposition. | TestMessageStructureRFCEnvelope, TestMessageStructureHeaderDefaults, TestMessageStructureScalarStrings, TestMessageStructureRecursiveBody, TestMessageStructureNestedMessage |
| IMAP-MSG-03 | RFC 3501 §§6.4.5, 6.4.8 | Normative | Encapsulated metadata works through FETCH and UID FETCH under SELECT and EXAMINE, shares a single raw read for both forms, and leaves Seen unchanged. | TestMessageStructureRFCEnvelope, TestMessageStructureHeaderDefaults, TestMessageStructureScalarStrings, TestMessageStructureRecursiveBody, TestMessageStructureNestedMessage |
| IMAP-MSG-04 | RFC 3501 §§7.4.2, 8 | Normative | Nested envelopes describe the encapsulated headers in ten-field order, preserving Date spelling and unfolded Subject rather than taking outer-message summaries. | TestMessageStructureRFCEnvelope, TestMessageStructureHeaderDefaults, TestMessageStructureScalarStrings, TestMessageStructureRecursiveBody, TestMessageStructureNestedMessage |
| IMAP-MSG-05 | RFC 3501 §7.4.2 | Normative | Missing nested scalar fields are NIL, present empty scalars are empty strings, and empty/absent Sender and Reply-To default to From. | TestMessageStructureHeaderDefaults |
| IMAP-MSG-06 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Nested scalar strings preserve encoded words, escape quoted-specials, and frame literal-only octets with counted literals. | TestMessageStructureScalarStrings |
| IMAP-MSG-07 | RFC 3501 §§7.4.2, 9 | Normative | Recursively describe encapsulated multipart and MESSAGE/RFC822 bodies with their own headers and encoded dimensions; other MESSAGE subtypes omit RFC822-specific fields. | TestMessageStructureRecursiveBody, TestMessageStructureNestedMessage |
| IMAP-MSG-08 | RFC 3501 §§7.4.2, 9 | Normative | Return a valid envelope structure for malformed encapsulated header blocks. | n/a: not supported yet; parse failure returns NIL in the required envelope position and a fabricated text/plain fallback body. Malformed-message policy needs a separate audit. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-MSG-09 | RFC 3501 §§4.3.1, 7.4.2; RFC 2046 §5.2.1 | Normative | Handle invalid encodings, binary octets and excessive encapsulation consistently. | n/a: not supported yet; MESSAGE/RFC822 accepts prohibited base64/quoted-printable encodings without decoding, NUL policy is undefined, and recursive MIME traversal has no depth limit. This slice checks ordinary 7BIT encapsulation only. Tracked in rest-mail/rest-mail-server#374. |

The nested address parser retains the group/source-route/address-string gaps
listed under IMAP-ENV-10/11. Multipart/digest defaults, malformed MIME framing,
optional extension values and numbered MIME FETCH sections remain separate gaps.
