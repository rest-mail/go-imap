# IMAP RFC checklist — PARTIAL checked foundation

This is an explicitly **PARTIAL** checklist: the first RFC 3501 rows for the
connection greeting, the any-state commands (CAPABILITY, NOOP, LOGOUT), LOGIN,
tagged completion, STARTTLS, selection-state (SELECT/EXAMINE), STATUS, STORE, SEARCH, FETCH/ENVELOPE,
EXPUNGE, CLOSE, APPEND, COPY and encoded BODYSTRUCTURE slices, plus partial
RFC 4315 UID EXPUNGE/COPYUID/APPENDUID, RFC 6154 ordinary LIST attributes,
RFC 6851 MOVE, RFC 3691 UNSELECT, RFC 5161 ENABLE, RFC 2177 IDLE command exchanges
and RFC 7888 non-synchronizing APPEND and command-argument refusal slices. It is not
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
| IMAP-GREET-02 | RFC 3501 §§7.1.5, 9 | Normative | A connection the server is unwilling to accept (the Limiter refuses it) is greeted with an untagged BYE, then closed, rather than dropped without a greeting. | TestGreetingByeOnRefusal |
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
| IMAP-STARTTLS-08 | RFC 3501 §6.2.3 | SHOULD NOT | When TLS is configured, do not permit LOGIN or AUTHENTICATE before TLS is active: every credential form, including literals and SASL initial responses, gets NO [PRIVACYREQUIRED] with no continuation and no backend credential check. A session built without TLS is a development deployment outside this row. | TestStartTLSCleartextCredentialsRefused |
| IMAP-STARTTLS-09 | RFC 2595 §3.1; RFC 3501 §6.2.1 | MAY | Authenticate with SASL EXTERNAL from TLS client credentials. | n/a: optional (MAY) and not offered. AUTH=EXTERNAL is never advertised and AUTHENTICATE EXTERNAL gets NO, before and after TLS; the session has no client-certificate identity source. Tracked in rest-mail/rest-mail-server#374. |

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
| IMAP-SELECT-11 | RFC 3501 §§6.3.1, 6.3.2, 9 | Normative | SELECT/EXAMINE require exactly one mailbox astring: missing, malformed, extra or improperly separated arguments receive BAD without metadata reads or writes; a failed selection drops prior selection and deletion marks without expunging, and a later valid selection recovers. | TestSelectionInvalidArguments, TestSelectionInvalidLiteralArguments |
| IMAP-SELECT-12 | RFC 3501 §§4.3, 5.1, 6.3.1, 6.3.2, 9 | Normative | Decode bare, quoted and counted-literal mailbox names without trimming significant spaces, preserve escaped quoted-specials and quoted wildcard characters, canonicalize only INBOX, and preserve command framing for synchronizing and LITERAL+ names. An explicit empty string reaches the backend unchanged rather than selecting INBOX. | TestSelectionMailboxAstrings, TestSelectionLiteralMailbox |
| IMAP-EXAMINE-01 | RFC 3501 §6.3.2 | MUST | Successful EXAMINE starts tagged OK text with READ-ONLY and reports no permanent flags. | TestSelectionExamples |
| IMAP-EXAMINE-02 | RFC 3501 §6.3.2 | Normative | EXAMINE forbids permanent writes through STORE, EXPUNGE and MOVE (including UID variants) and CLOSE; a later SELECT restores writable access. | TestExaminePreventsPermanentWrites |
| IMAP-EXAMINE-03 | RFC 3501 §6.3.2 | Normative | Non-peek FETCH and UID FETCH under EXAMINE return the body without persisting or locally setting Seen. | TestExamineBodyFetchDoesNotSetSeen |

The argument rows check complete astring tokens after literal materialization.
Raw quoted and literal octets share that representation, so strict wire-form
validation (including raw quoted 8-bit octets), modified UTF-7 mailbox semantics,
permanent keyword support, UID exhaustion, backend error/fallback behavior and
concurrent sessions remain unaudited. These rows do not claim full
§§6.3.1/6.3.2 conformance.

## RFC 3501: STATUS — partial metadata and argument slice

`TestStatusExample` uses §6.3.10's A042 request, 231 messages and UIDNEXT
44292. The engine quotes the mailbox and returns items in request order, both
permitted by the response grammar. Other fixtures distinguish unseen counts
from selected-folder metadata and use mailbox-specific UIDVALIDITY values.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-STATUS-01 | RFC 3501 §§6.3.10, 7.2.4 | Normative | Return one untagged STATUS with the queried mailbox and requested MESSAGES/UNSEEN/UIDNEXT items before tagged OK, including the RFC example and empty mailboxes; item names are case-insensitive and no unrequested items are added. | TestStatusExample, TestStatusRequestedItems |
| IMAP-STATUS-02 | RFC 3501 §§5.1, 6.3.10, 7.2.4 | Normative | STATUS canonicalizes any-case INBOX and returns the queried mailbox's UIDVALIDITY when UIDPlusMailbox supplies it, rather than selected-folder values. | TestStatusRequestedItems |
| IMAP-STATUS-03 | RFC 3501 §§6.3.10, 9 | Normative | Reject malformed mailbox astrings, missing/unknown/quoted/non-ASCII items, non-SP or doubled/leading/trailing separators, unbalanced/nested lists and trailing arguments with BAD before metadata/content reads or flag changes; keep the session reusable. | TestStatusInvalidArguments |
| IMAP-STATUS-04 | RFC 3501 §6.3.10 | Normative | Successful and failed STATUS leave the selected mailbox, cached flags and SELECT/EXAMINE access mode unchanged; in the authenticated state STATUS does not select a mailbox or read message content. | TestStatusInvalidArguments, TestStatusPreservesSelection |
| IMAP-STATUS-05 | RFC 3501 §§3, 6.3.10 | Normative | Refuse STATUS before authentication without metadata access; backend lookup failure returns NO without an untagged STATUS, and a later valid query succeeds. | TestStatusPreservesSelection |
| IMAP-STATUS-06 | RFC 3501 §§4.3, 6.3.10, 7.2.4, 9 | Normative | Decode quoted and counted-literal mailbox astrings, echo names with escaped quotes/backslashes or exact counted literals, treat quoted wildcard characters as part of a name, and reject NUL without dispatching literal payload as commands. | TestStatusMailboxStrings |
| IMAP-STATUS-07 | RFC 3501 §§2.3.2, 6.3.10 | Normative | RECENT reports independent Recent state and STATUS preserves it. | n/a: not supported yet; Message has no Recent field and STATUS approximates RECENT as unseen, as in IMAP-SELECT-08. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-STATUS-08 | RFC 3501 §§2.3.1.1, 6.3.10 | MUST | UIDNEXT tracks past UID assignments and UIDVALIDITY reflects persistent mailbox identity. | n/a: not supported yet for the base Mailbox interface; UIDNEXT is current maximum plus one and UIDVALIDITY defaults to 1 without UIDPlusMailbox, as in IMAP-SELECT-09. UID exhaustion and UIDValidity backend-error fallback remain unaudited. Tracked in rest-mail/rest-mail-server#374. |

This is not a complete STATUS or mailbox-name audit. Literal arguments are
materialized as quoted strings before parsing, so raw quoted 8-bit/control
octets cannot be distinguished from literal contents here; strict wire-form
validation, modified UTF-7 mailbox semantics, concurrent updates and UID
exhaustion remain outside this slice. RECENT syntax remains accepted without a
claim that its current unread approximation conforms.

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
| IMAP-STORE-06 | RFC 3501 §§2.3.2, 6.4.6, 7.1 | Normative | PERMANENTFLAGS lists `\*` only if keywords a client creates are stored and reported back by FETCH FLAGS. The engine stores no keywords, so it never lists `\*` and never reports an unstored keyword. | TestKeywordsNotAdvertised |
| IMAP-STORE-07 | RFC 3501 §§6.4.6, 6.4.8, 9 | Normative | STORE and UID STORE reject malformed sequence sets with BAD before backend writes, session flag changes or FETCH responses, including .SILENT and empty mailboxes; the selection remains writable and reusable. | TestStoreInvalidSequenceSet |
| IMAP-STORE-08 | RFC 3501 §§6.4.6, 6.4.8, 9 | Normative | Accept reversed ranges, star, lists and unsigned-32-bit sequence-set bounds; resolve sparse or nonexistent UIDs by mailbox membership and preserve normal/silent flag updates and UID replies. | TestStoreValidSequenceSet |
| IMAP-STORE-09 | RFC 3501 §§6.4.6, 9 | Normative | STORE and UID STORE reject a malformed flag operand with BAD before backend writes, session flag changes or FETCH responses: unbalanced, nested or repeated parentheses, extra or non-SP separators, quoted strings, and flags that are not an atom with an optional leading `\` (atom-specials, `\*`, non-ASCII or control octets). | TestStoreInvalidFlagList |
| IMAP-STORE-10 | RFC 3501 §§6.4.6, 9 | Normative | Accept an empty flag-list, bare SP-separated flags, case-insensitive system flags, and keywords or flag-extensions using any ATOM-CHAR, for every action with and without .SILENT. | TestStoreValidFlagList |

A NO from a multi-message set reports the first refused write; earlier
messages in the set keep their applied changes, which RFC 3501 permits.
Out-of-range message sequence numbers, overlapping UID-set
semantics and STORE under concurrent sessions remain unaudited.

## RFC 3501: SEARCH and UID SEARCH — partial criteria slice

`TestSearchExample` replays §6.4.4's A282 and A283 examples, with a mailbox
containing exactly the three expected matches and near misses for each A282
predicate. It does not replay A284's UTF-8 literal. Calendar-date tests supply
`Message.Date` as the internal date, including positive/negative timezone
offsets that cross UTC midnight. Hosts must supply that metadata correctly.
Size searches similarly trust the host's nonnegative `Message.Size`, the full
RFC822.SIZE octet count, including headers. Synthetic boundary fixtures and a
UTF-8/quoted-printable message distinguish strict octet comparisons from decoded
body or character counts; size searches never fetch message content. HEADER,
CC, BCC and SENT* keys read the raw header block from the backend; HEADER, CC
and BCC values are matched after RFC 2047 encoded-word decoding (IMAP-SEARCH-10).

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
| IMAP-SEARCH-10 | RFC 3501 §6.4.4 | MUST | Decode MIME transfer encodings and encoded headers to the selected character set before searching: BODY/TEXT remove base64 and quoted-printable encodings in every MIME leaf, including encapsulated messages; TEXT, HEADER and SUBJECT decode RFC 2047 encoded words; UTF-8, US-ASCII and ISO-8859-1 content is compared as UTF-8. Malformed encodings, and parts in other charsets, are compared as stored. | TestSearchDecodesMIME |
| IMAP-SEARCH-11 | RFC 3501 §6.4.4 | Normative | Support the KEYWORD and UNKEYWORD search keys with a flag-keyword atom operand. No keyword can be stored, so KEYWORD matches nothing and UNKEYWORD matches every message; a missing or non-atom operand is BAD. | TestSearchKeywordKeys |
| IMAP-SEARCH-12 | RFC 3501 §6.4.4 | Normative | RECENT, NEW and OLD search independent Recent state. | n/a: not supported yet; RECENT approximates unseen, NEW/OLD are rejected and Message has no Recent field. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-SEARCH-13 | RFC 3501 §§6.4.4, 6.4.8, 9 | Normative | Reject malformed bare sequence-set keys and UID set operands with BAD and no partial SEARCH result or content evaluation, including inside NOT, OR and groups and in empty mailboxes; keep the session reusable. | TestSearchInvalidSequenceSet |
| IMAP-SEARCH-14 | RFC 3501 §§6.4.4, 6.4.8, 9 | Normative | Valid reversed, star, overlapping and unsigned-32-bit boundary sets retain sequence/UID membership semantics under SEARCH and UID SEARCH, with each match returned once and no content reads or flag writes. | TestSearchValidSequenceSet |
| IMAP-SEARCH-15 | RFC 3501 §§6.4.4, 6.4.8 | Normative | ANSWERED/UNANSWERED and DRAFT/UNDRAFT match the presence/absence of their respective system flag under SEARCH and UID SEARCH, including Boolean/group combinations and empty/read-only mailboxes, without content reads or flag changes. | TestSearchAnsweredDraftKeys |
| IMAP-SEARCH-16 | RFC 3501 §§6.4.4, 6.4.6, 6.4.8 | Normative | Flag searches use current selected-session Answered/Draft values after successful STORE and UID STORE addition, removal and replacement; a refused backend update leaves search membership unchanged. | TestSearchAnsweredDraftStore |
| IMAP-SEARCH-17 | RFC 3501 §§6.4.4, 6.4.8, 9 | Normative | LARGER and SMALLER strictly compare full RFC822.SIZE octets with an unsigned 32-bit number, accepting zero and leading zeroes; SEARCH and UID SEARCH retain Boolean/group/set semantics without content reads or flag changes. | TestSearchSizeKeys, TestSearchSizeOctets |
| IMAP-SEARCH-18 | RFC 3501 §§6.4.4, 6.4.8, 9 | Normative | Missing, signed, quoted, literal, nondecimal and overflowing size operands produce BAD before results or content evaluation, even inside Boolean/groups or empty mailboxes; the session remains reusable. | TestSearchInvalidSize |
| IMAP-SEARCH-19 | RFC 3501 §6.4.4 | Normative | HEADER, CC and BCC match a case-insensitive substring of the unfolded text after the colon of the named header fields (Cc/Bcc for CC/BCC), never the body; field names compare case-insensitively and an empty string matches any message with the field; a message whose content is unavailable matches none. | TestSearchHeaderKeys, TestSearchHeaderUnavailable |
| IMAP-SEARCH-20 | RFC 3501 §6.4.4 | Normative | SENTBEFORE, SENTON and SENTSINCE compare the Date: header's calendar date as written, disregarding time and timezone and ignoring the internal date; a message with no parseable Date: header matches none. | TestSearchSentDates, TestSearchHeaderUnavailable |
| IMAP-SEARCH-21 | RFC 3501 §§6.4.4, 9 | Normative | Missing or empty HEADER field names, missing HEADER/CC/BCC strings and missing or invalid SENT* dates produce BAD before results or content reads, even inside Boolean/groups or empty mailboxes; the session remains reusable. | TestSearchInvalidHeaderKeys |
| IMAP-SEARCH-22 | RFC 3501 §§6.4.4, 6.4.8, 9 | Normative | All six internal/sent-date keys accept bare or DQUOTE-enclosed dates with one/two day digits, a case-insensitive month and four year digits; calendar comparisons retain SELECT/EXAMINE and sequence/UID semantics. | TestSearchDateGrammar |
| IMAP-SEARCH-23 | RFC 3501 §§6.4.4, 6.4.8, 9 | Normative | Reject date-token padding, controls, non-ASCII digits/months, quoted escapes, malformed widths and invalid calendar dates with BAD before results, content reads or flag changes, including Boolean/groups and empty mailboxes; keep command framing and selection reusable. | TestSearchInvalidDateGrammar |
| IMAP-SEARCH-24 | RFC 3501 §§4.3, 6.4.4, 6.4.8, 9 | Normative | Quoted SEARCH operands decode escaped DQUOTE and backslash without ending the token early, including Boolean/groups; synchronizing and LITERAL+ string operands preserve quotes, backslashes, parentheses, CRLF, UTF-8 and empty values without flag writes. | TestSearchQuotedStrings, TestSearchQuotedLiterals |
| IMAP-SEARCH-25 | RFC 3501 §§4.3, 6.4.4, 6.4.8, 9 | Normative | Unterminated quotes, invalid escapes, embedded quotes in bare tokens, NUL and text adjoining a closing quote receive BAD before results or content evaluation, including Boolean/groups and empty/read-only mailboxes; selection and command framing remain reusable. | TestSearchInvalidQuotes |

This is not a full search grammar or conformance audit. Date tokens and
quoted-token framing/escapes are checked above, each with a grammar-based fuzz
target: FuzzSearchDate compares date tokens against an independent RFC grammar
and Gregorian calendar oracle, and FuzzSearchQuotes covers quoted-token framing;
CI runs each for 30 seconds. The shared literal reader materializes literals as
quoted strings, so this slice preserves CR/LF and 8-bit literal bytes and does
not distinguish them from illegal bytes sent directly in a wire quoted-string.
Literal date operands, full string character validation, bare astring grammar
and separators between keys remain unaudited. Out-of-range message sequence
number rejection is also outside this slice. FROM display-name matching,
backend Fetch errors, BODY/TEXT matching beyond the empty-result example,
malformed charset data, concurrency, EXPUNGE suppression and the RFC's UID SEARCH
example remain for later slices. No coverage gate or independent implementation
comparison is added here.

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
| IMAP-FETCH-14 | RFC 3501 §6.4.5 | MUST | Numeric MIME part paths retrieve the designated part, with nested MESSAGE/RFC822 and MIME section rules; malformed section-specs receive BAD. | TestFetchNumericSections, TestFetchNumericSectionSingle, TestFetchInvalidSection |
| IMAP-FETCH-15 | RFC 3501 §§6.4.5, 9 | Normative | Invalid or unknown data items, malformed section/partial syntax and illegal macro combinations receive BAD. | TestFetchComposedGrammar, TestFetchComposedGrammarRefusals |
| IMAP-FETCH-16 | RFC 3501 §§6.4.5, 6.4.8, 9 | Normative | FETCH and UID FETCH reject malformed sequence sets with BAD before loading message data, changing Seen or sending FETCH responses, including in empty mailboxes; the connection and selection remain usable. | TestFetchInvalidSequenceSet |
| IMAP-FETCH-17 | RFC 3501 §§6.4.5, 6.4.8, 9 | Normative | FETCH and UID FETCH accept reversed ranges, star and nonzero unsigned-32-bit bounds; sparse/nonexistent UIDs select only existing messages, with sequence-number response prefixes and UID attributes. | TestFetchValidSequenceSet |
| IMAP-FETCH-18 | RFC 3501 §§6.4.5, 9 | Normative | BODY and BODY.PEEK partial requests require unsigned 32-bit number.nz-number operands; invalid suffixes receive BAD before content reads, Seen writes or responses, even for empty/nonmatching results. | TestFetchInvalidPartial |
| IMAP-FETCH-19 | RFC 3501 §§6.4.5, 7.4.2, 9 | Normative | Valid partial bounds, including leading-zero origins and uint32 maxima, return truncated octets and origin-only labels with ordinary PEEK/EXAMINE Seen semantics. | TestFetchPartialBounds |
| IMAP-FETCH-20 | RFC 3501 §§6.4.5, 6.4.8, 9 | Normative | Require one known data item, a nonempty SP-separated parenthesized item list, or a standalone macro; reject unknown names, illegal macro combinations and malformed outer framing before reads, Seen writes or responses, including empty/nonmatching results; preserve session usability. | TestFetchInvalidItems |
| IMAP-FETCH-21 | RFC 3501 §§6.4.5, 6.4.8, 9 | Normative | Standalone case-insensitive FAST, ALL and FULL produce their explicit-list results; ordinary singleton and list forms preserve exact literals, metadata and Seen semantics for FETCH and UID FETCH in SELECT and EXAMINE. | TestFetchItemForms |
| IMAP-FETCH-22 | RFC 3501 §§6.4.5, 6.4.8, 9 | Normative | HEADER.FIELDS and HEADER.FIELDS.NOT require one SP and a nonempty list of valid astrings with exact SP separators; malformed forms receive BAD before content reads, Seen writes or responses, including empty/nonmatching sets; preserve command stream and selection. | TestFetchInvalidHeaderLists |
| IMAP-FETCH-23 | RFC 3501 §§4.3, 6.4.5, 7.4.2, 9 | Normative | Decode quoted and literal header names, preserve their exact values for case-insensitive include/exclude matching, emit valid astring labels and exact folded-header subsets, apply partials after selection and retain PEEK/EXAMINE Seen semantics. | TestFetchHeaderAstrings, TestFetchHeaderLiteralNames |

This slice does not prove full ENVELOPE/BODYSTRUCTURE semantics, static attribute
immutability across sessions, absent-part NIL handling, header-only messages
without a delimiting blank line, sequence-set overlap behavior, rejection of
out-of-range message sequence numbers, UID exhaustion or concurrent EXPUNGE
suppression. Hosts must provide correct raw message bytes, octet size, internal
date and persistent UID/flag metadata.
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
`TestCloseBackendFailure` refuses deletions during CLOSE: the engine answers NO,
stays selected, reports the removals that succeeded as EXPUNGE and keeps the
failed marks, so a retried CLOSE completes.

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
| IMAP-CLOSE-06 | RFC 3501 §6.4.2 | Normative | Writable CLOSE removes every marked message before reporting successful completion. | TestCloseBackendFailure |
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
| IMAP-UIDEXPUNGE-05 | RFC 4315 §§2.1, 4; RFC 3501 §9 | Normative | Invalid UID sequence sets (zero, leading zeros, values above 2^32-1, empty items or range bounds, extra separators or trailing arguments) receive BAD before deletion; valid reversed, `*` and maximum-UID sets are still accepted. | TestUIDExpungeInvalidSet |

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
| IMAP-APPEND-21 | RFC 3501 §§4.3, 6.3.11 | Normative | Accept a literal mailbox-name astring before the message literal. | TestAppendLiteralMailbox |
| IMAP-APPEND-22 | RFC 7888 §§3–4 | MUST | For a syntactically valid non-synchronizing literal marker, early APPEND refusal sends BYE and ends the session without dispatching unread octets; backend refusal after consuming the complete command preserves synchronization. | TestAppendRejectedNonSynchronizing, TestAppendNonSynchronizingBackendFailure |
| IMAP-APPEND-23 | RFC 3501 §§4.3, 9 | Normative | Enforce the complete APPEND grammar, including all astring and literal forms. | TestAppendGrammarCombinations, TestAppendQuotedMailboxOctets |
| IMAP-APPEND-24 | RFC 3501 §§6.3.11, 9 | Normative | Answer BAD before a continuation, without calling the backend, for a malformed flag-list: unbalanced or nested parentheses, a second list, a missing SP after the list, doubled, leading, trailing or tab separators, and flags that are not an atom with an optional leading `\`. | TestAppendInvalidFlagList, TestAppendRejectedNonSynchronizing |
| IMAP-APPEND-25 | RFC 3501 §§6.3.11, 9 | Normative | Accept an empty flag-list and single-SP-separated supported flags; a well-formed list naming a flag the store cannot keep stays NO, not BAD. | TestAppendValidFlagList |
| IMAP-APPEND-26 | RFC 3501 §§6.3.11, 9 | Normative | Separate mailbox, flag-list, date-time and message literal by exactly one SP: answer BAD before a continuation, without calling the backend, for a missing, doubled, leading or tab separator, and accept every single-SP combination. | TestAppendInvalidSeparators, TestAppendSingleSeparators, TestAppendRejectedNonSynchronizing |
| IMAP-APPEND-27 | RFC 3501 §§4.3, 9 | Normative | Reject NUL in a counted APPEND message literal with BAD, without calling the backend; after consuming its terminator, keep the next command framed for both synchronizing and non-synchronizing literals. | TestAppendRejectsNULMessageOctet |
| IMAP-APPEND-28 | RFC 3501 §9 | Normative | Decode quoted APPEND mailbox names with escaped DQUOTE or backslash, and reject any other escape, NUL or 8-bit octet before a message-literal continuation. | TestAppendQuotedMailboxGrammar |
| IMAP-APPEND-29 | RFC 3501 §§6.3.11, 9 | Normative | Accept fixed-width, DQUOTE-delimited date-times with space-padded or two-digit days and preserve explicit dates at DateAppender, including Go's zero time; reject malformed fields, escapes and invalid calendar/time/zone values before continuation or backend writes, preserving synchronizing recovery and non-synchronizing BYE refusal. | TestAppendInvalidDateTime, TestAppendValidDateTime |
| IMAP-APPEND-30 | RFC 3501 §§4.3, 9 | Normative | Unquoted APPEND mailbox names contain only ASTRING-CHAR octets (including `]`); reject controls, non-ASCII octets, wildcards and quoted-specials before a continuation or backend call, preserving synchronizing recovery and closing on unread non-synchronizing literals. | TestAppendUnquotedMailboxGrammar, TestAppendRejectedNonSynchronizing |

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
and CHAR8 validation, binary literals and
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
| IMAP-COPY-14 | RFC 3501 §§6.4.7, 9 | Normative | Reject invalid sequence sets and mailbox argument syntax with BAD. | TestCopyInvalidArguments |
| IMAP-COPY-15 | RFC 4315 §§3, 5 | SHOULD NOT | Suppress COPYUID for a destination the user cannot SELECT/EXAMINE and for UIDNOTSTICKY stores. | n/a: not supported yet; backend API exposes no destination readability or UIDNOTSTICKY marker. Hosts can suppress metadata by returning zero/error UIDValidity, but that policy is not independently checked here. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-COPY-16 | RFC 4315 §§3, 4 | Normative | COPYUID contains only valid nonzero destination UIDs, with no extraneous or repeated UIDs. | TestCopyUIDInvalidBackendUIDs |

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
| IMAP-MOVE-12 | RFC 6851 §3.1; RFC 3501 §9 | Normative | Reject invalid sequence sets and mailbox argument syntax with BAD. | TestMoveInvalidArguments |
| IMAP-MOVE-13 | RFC 6851 §4 | Normative | With QRESYNC enabled, report moved messages with VANISHED instead of EXPUNGE; other sessions selecting the source learn of the removal. | n/a: the QRESYNC half does not apply: QRESYNC is neither advertised nor enabled, so VANISHED is never due. Telling other sessions on the source mailbox about moved messages needs cross-session notification, which the engine does not have. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-MOVE-14 | RFC 4315 §§3, 4 | Normative | COPYUID contains only valid nonzero destination UIDs, with no extraneous or repeated UIDs. | TestMoveUIDInvalidBackendUIDs |

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
| IMAP-BS-08 | RFC 3501 §7.4.2; RFC 3282 §2 | MAY | BODYSTRUCTURE derives the language slot from each entity's own Content-Language list: comments dropped, tags trimmed with case kept; none is NIL, one a string, several a parenthesized list; multipart and MESSAGE/RFC822 values are not inherited and tags use valid string framing. Tags are not validated against the language-tag grammar. | TestBodyStructureLanguage, TestBodyStructureNestedLanguage, TestBodyStructureLanguageStrings |
| IMAP-BS-09 | RFC 3501 §§7.4.2, 9 | Normative | MESSAGE/RFC822 carries its encapsulated envelope, body structure and line count. | TestMessageStructureRFCEnvelope, TestMessageStructureHeaderDefaults, TestMessageStructureScalarStrings, TestMessageStructureRecursiveBody, TestMessageStructureNestedMessage |
| IMAP-BS-10 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Encode every body string using valid IMAP string framing for the source octets. | n/a: not fully audited; MIME strings are checked by IMAP-BS-13 through IMAP-BS-16, encapsulated envelope address components by IMAP-ENV-13, and complete binary-message handling remains unaudited; raw or decoded NUL in FETCH data now receives NO before output or Seen writes (IMAP-ENV-12). Tracked in rest-mail/rest-mail-server#374. |
| IMAP-BS-11 | RFC 3501 §7.4.2 | Normative | Default omitted MIME fields according to MIME rules, including defaults that depend on the enclosing multipart subtype. | n/a: not fully audited; fabricated text/plain fallback structures for unparseable header blocks and empty multiparts need independent verification. Ordinary Content-Type/charset defaults and missing subtypes are IMAP-BS-20/21; multipart/digest child defaults are IMAP-BS-12. Content-Type comments are IMAP-BS-22/23, Content-Transfer-Encoding IMAP-CTE-01..03 and Content-Disposition IMAP-CDP-01..03; comments in Content-ID/Description and malformed multipart framing remain unaudited. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-BS-12 | RFC 3501 §7.4.2; RFC 2046 §5.1.5 | Normative | A multipart/digest child without Content-Type is MESSAGE/RFC822 with its envelope, body structure and line count; explicit types still win, and the default does not leak into other multiparts or multiparts nested in the digest. | TestBodyStructureDigestDefault, TestBodyStructureDigestScope |
| IMAP-BS-13 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | MIME parameter values are strings: preserve empty values as quoted empty strings, escape quoted-specials, retain encoded words, and frame RFC 2231 decoded 8-bit/CRLF values with octet-counted literals. | TestBodyStructureParameterStrings |
| IMAP-BS-14 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Content-ID, description and transfer-encoding fields use valid quoted/literal framing; missing optional fields remain NIL and missing transfer encoding defaults to 7BIT. | TestBodyStructureHeaderStrings |
| IMAP-BS-15 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Single-part and multipart disposition parameters use string framing for empty, escaped and literal values; multipart content-type parameters preserve empty values and BODY omits extension data. | TestBodyStructureDispositionStrings |
| IMAP-BS-16 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | MIME string literals retain their byte counts and boundaries in multipart children and encapsulated MESSAGE/RFC822 bodies under FETCH/UID FETCH and SELECT/EXAMINE without Seen writes. | TestBodyStructureNestedStrings |
| IMAP-BS-17 | RFC 3501 §7.4.2; RFC 1864 §2 | Normative | BODYSTRUCTURE preserves supplied Content-MD5 in the single-part MD5 slot before disposition; absent/empty values remain NIL, multipart has no MD5 slot, and BODY omits extensions. No digest is generated or verified. | TestBodyStructureMD5Location, TestBodyStructureMultipartLocation, TestBodyStructureMessageLocation |
| IMAP-BS-18 | RFC 3501 §7.4.2 | Normative | BODYSTRUCTURE preserves Content-Location after the language slot on single-part, multipart and MESSAGE/RFC822 entities, without inheriting parent values or resolving relative URIs; absent/empty values remain NIL. | TestBodyStructureMD5Location, TestBodyStructureMultipartLocation, TestBodyStructureMessageLocation |
| IMAP-BS-19 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Supplied MD5/location strings escape quoted-specials and frame 8-bit octets as counted literals; metadata shares raw reads and leaves Seen unchanged under SELECT/EXAMINE and FETCH/UID FETCH. | TestBodyStructureExtensionStrings |
| IMAP-BS-20 | RFC 3501 §7.4.2; RFC 2045 §§5.2, 6.1; RFC 2046 §4.1.2 | Normative | An ordinary entity without Content-Type defaults to TEXT/PLAIN with US-ASCII charset and absent transfer encoding to 7BIT, regardless of MIME-Version; text without charset uses US-ASCII, explicit type/charset values win, and non-text types gain neither charset nor text line count. | TestBodyStructureMIMEDefaults, TestBodyStructureExplicitContentType |
| IMAP-BS-21 | RFC 2045 §§5.1, 5.2; RFC 3501 §7.4.2 | Normative | Content-Type requires type/subtype; a bare token must not invent a subtype. Apply the recommended TEXT/PLAIN US-ASCII fallback to absent/invalid type syntax and malformed parameters in ordinary entities, mixed children and encapsulated messages, discarding invalid type parameters but retaining independent content headers and encoded dimensions. | TestBodyStructureInvalidContentType, TestBodyStructureExplicitContentType |
| IMAP-BS-22 | RFC 2045 §5.1; RFC 822 §§3.4.2–3.4.3; RFC 3501 §7.4.2 | Normative | Content-Type ignores nested comments and quoted-pairs between lexical tokens, accepts whitespace around the slash, and preserves parentheses/escapes inside quoted parameter values. Comment-bearing multipart boundaries retain children; nested and encapsulated entities parse independently without changing encoded dimensions or metadata side effects. | TestBodyStructureContentTypeComments, TestBodyStructureNestedContentTypeComments |
| IMAP-BS-23 | RFC 2045 §§5.1–5.2; RFC 822 §3.4.3 | Normative | A comment separates tokens rather than joining them; split type/subtype/attribute/value tokens and unbalanced comments use invalid-type recovery, dropping invalid parameters and retaining independent Content-ID. | TestBodyStructureInvalidContentTypeComments |

This slice preserves quoted-printable source data via raw MIME part traversal.
It does not validate or decode transfer encodings, expose numbered MIME FETCH
sections, or claim complete BODYSTRUCTURE/MIME conformance.

## MIME transfer-encoding token — partial lexical slice

RFC 2045 §§1/6.1 apply RFC 822 §§3.4.2–3.4.3 comments and lexical
whitespace to a single encoding token. Fixtures are synthetic grammar cases,
not borrowed message vectors. Malformed values retain the prior opaque-string
behavior; this is not a full MIME validity or transfer-decoding check.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-CTE-01 | RFC 2045 §§1, 6.1; RFC 822 §§3.4.2–3.4.3 | Normative | BODY/BODYSTRUCTURE derive the case-insensitive encoding token without surrounding whitespace or nested/escaped comments; retain extension tokens and encoded body octets/line counts. | TestBodyStructureEncodingComments, TestBodyStructureEncodingCommentScope |
| IMAP-CTE-02 | RFC 3501 §§6.4.5, 7.4.2; RFC 2045 §6.1 | Normative | Parse each multipart or encapsulated entity's own encoding, default absent fields to 7BIT without inheritance, and preserve metadata-only behavior under SELECT/EXAMINE and FETCH/UID FETCH. | TestBodyStructureEncodingCommentScope |
| IMAP-CTE-03 | RFC 2045 §6.1; RFC 822 §3.4.2 | Normative | A single encoding token cannot be manufactured by joining token pieces across comments or whitespace; malformed values retain the existing safely framed opaque-string policy. | TestBodyStructureMalformedEncodingComments |

## MIME Content-Disposition comments — partial lexical slice

RFC 2183 §2 applies RFC 822 §§3.4.2–3.4.3 comments and lexical whitespace to
Content-Disposition. The comment lexer is shared with Content-Type
(IMAP-BS-22/23). Fixtures are synthetic grammar cases, not borrowed message
vectors. RFC 2183 parameter value syntax (dates, sizes) and RFC 2231
continuations beyond Go's MIME parser are not audited.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-CDP-01 | RFC 2183 §2; RFC 822 §§3.4.2–3.4.3; RFC 3501 §7.4.2 | Normative | BODYSTRUCTURE body-fld-dsp ignores nested/escaped and folded comments between the disposition type, parameter names and values, and preserves parentheses and quoted-pairs inside quoted parameter values. | TestBodyStructureDispositionComments |
| IMAP-CDP-02 | RFC 2183 §2; RFC 3501 §7.4.2 | Normative | Each single-part, multipart and encapsulated entity reports its own comment-bearing disposition without inheritance, under SELECT/EXAMINE and FETCH/UID FETCH. | TestBodyStructureDispositionCommentScope |
| IMAP-CDP-03 | RFC 2183 §2; RFC 822 §3.4.2 | Normative | A comment separates rather than joins tokens; split tokens, unbalanced comments or quotes, an empty type and a type/subtype pair yield NIL while independent fields are retained. | TestBodyStructureInvalidDispositionComments |

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
| IMAP-ENV-03 | RFC 3501 §7.4.2 | Normative | Address lists preserve mailbox order and personal-name/mailbox/host components; absent or empty From, To, Cc and Bcc are NIL, regardless of cached metadata. | TestEnvelopeExample, TestEnvelopeDistinctFields, TestEnvelopeAbsentAndEmpty, TestEnvelopeAddressStrings |
| IMAP-ENV-04 | RFC 3501 §7.4.2 | Normative | Explicit Sender and Reply-To take their own address lists rather than From. | TestEnvelopeExample, TestEnvelopeDistinctFields |
| IMAP-ENV-05 | RFC 3501 §§6.4.5, 6.4.8, 7.4.2 | Normative | FETCH and UID FETCH return ENVELOPE without setting or persisting Seen. | TestEnvelopeExample, TestEnvelopeDistinctFields, TestEnvelopeAbsentAndEmpty, TestEnvelopeScalarStrings, TestEnvelopeReadOnlyAndSharedLoad, TestEnvelopeAddressStrings |
| IMAP-ENV-06 | RFC 3501 §7.4.2 | Normative | Absent scalar headers are NIL; present empty Date, Subject, In-Reply-To and Message-ID are empty strings. | TestEnvelopeAbsentAndEmpty |
| IMAP-ENV-07 | RFC 3501 §7.4.2 | Normative | Absent or empty Sender and Reply-To default to the From member, including when From is NIL. | TestEnvelopeAbsentAndEmpty |
| IMAP-ENV-08 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Scalar header strings escape quote/backslash and use counted literals for 8-bit octets; encoded words remain header text. | TestEnvelopeScalarStrings |
| IMAP-ENV-09 | RFC 3501 §§2.3.3, 6.3.2, 6.4.5 | Normative | EXAMINE ENVELOPE remains read-only, INTERNALDATE remains arrival metadata and repeated items share a raw read. | TestEnvelopeReadOnlyAndSharedLoad |
| IMAP-ENV-10 | RFC 3501 §7.4.2 | Normative | Preserve RFC 2822 groups with group-start and group-end address structures. | TestEnvelopeGroups, TestEnvelopeMalformedGroups |
| IMAP-ENV-11 | RFC 3501 §7.4.2 | Normative | Preserve source routes and legal address syntax. | n/a: not supported yet; source routes are always NIL and net/mail rejects some obsolete syntax, so the whole list falls back to NIL. Component string framing is checked by IMAP-ENV-13. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-ENV-12 | RFC 3501 §§4.3.1, 7.4.2 | Normative | Treat malformed header blocks and prohibited NUL octets consistently without fabricating valid header information. | TestEnvelopeMalformedHeaders, TestFetchRejectsNUL |
| IMAP-ENV-13 | RFC 3501 §§4.3, 7.4.2, 9 | Normative | Address personal-name, mailbox and host components escape quoted-specials and use counted literals for 8-bit octets and CR/LF decoded from encoded words, in top-level and encapsulated envelopes. | TestEnvelopeAddressStrings, TestMessageStructureScalarStrings |

Malformed top-level header blocks yield ten NIL envelope fields rather than
cached summary data; malformed encapsulated headers retain the same ten-field
shape while their body description keeps the existing opaque TEXT/PLAIN recovery.
Raw message NUL and NUL decoded into fetched envelope/MIME strings receive NO
before any response fragment or Seen update. This is refusal, not BINARY support.
Address groups retain start/end markers, empty groups and member order, including
inside MESSAGE/RFC822; source-route and net/mail syntax limitations remain.

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
| IMAP-MSG-08 | RFC 3501 §§7.4.2, 9 | Normative | Return a valid envelope structure for malformed encapsulated header blocks. | TestMessageStructureMalformedEnvelope |
| IMAP-MSG-09 | RFC 3501 §§4.3.1, 7.4.2; RFC 2046 §5.2.1 | Normative | Handle invalid encodings, binary octets and excessive encapsulation consistently. | n/a: not supported yet; MESSAGE/RFC822 accepts prohibited base64/quoted-printable encodings without decoding, raw/decoded NUL is refused on FETCH (IMAP-ENV-12), and recursive MIME traversal has no depth limit. This slice checks ordinary 7BIT encapsulation only. Tracked in rest-mail/rest-mail-server#374. |

The nested address parser preserves groups (IMAP-ENV-10); source routes and
other legal address syntax rejected by net/mail remain IMAP-ENV-11 gaps.
Malformed MIME framing and complete extension-field semantics remain unaudited.

## RFC 3691 / RFC 5161 / RFC 2177: extension command exchanges

These rows cover command syntax, responses and session transitions, not a full
extension inventory. [RFC 3691](https://www.rfc-editor.org/rfc/rfc3691),
[RFC 5161](https://www.rfc-editor.org/rfc/rfc5161) and
[RFC 2177](https://www.rfc-editor.org/rfc/rfc2177) define the exchanges below.
The UNSELECT test uses §2's A341 tag; the ENABLE fixture uses
§3.1's CONDSTORE/X-GOOD-IDEA names, both unsupported by this server.
The IDLE test follows §3's A002/DONE sequence and pipelines a following NOOP.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-EXT-CAP-01 | RFC 3691 §1; RFC 5161 §1; RFC 2177 §3 | Normative | Advertise UNSELECT, ENABLE and IDLE in CAPABILITY, before and after authentication and selection. | TestExtensionCapabilities |
| IMAP-UNSELECT-01 | RFC 3691 §§2, 4 | Normative | UNSELECT (case-insensitive) returns tagged OK without expunging messages, including after marking messages Deleted or opening read-only. | TestUnselectWithoutExpunge |
| IMAP-UNSELECT-02 | RFC 3691 §2 | Normative | Successful UNSELECT leaves authentication intact and releases selection state; FETCH is refused until reselecting and old deletion marks/read-only mode do not affect a later writable selection. | TestUnselectWithoutExpunge |
| IMAP-UNSELECT-03 | RFC 3691 §2 | Normative | With no selected mailbox UNSELECT returns BAD and the connection remains usable. | TestUnselectRequiresSelection |
| IMAP-UNSELECT-04 | RFC 3691 §§2, 4 | Normative | Extra UNSELECT arguments receive BAD without dropping the selection or its deletion marks. | TestUnselectInvalidArgumentsPreserveSelection |
| IMAP-ENABLE-01 | RFC 5161 §3.1 | MUST | Ignore unknown names and known extensions not enabled through ENABLE, without enabling UTF8 as a side effect. | TestEnableUnknownAndCapabilityStability |
| IMAP-ENABLE-02 | RFC 5161 §§3.1–3.2 | MUST | Every successful ENABLE sends ENABLED before tagged OK, listing only newly enabled capabilities; send an empty list when none are new. | TestEnableUnknownAndCapabilityStability, TestEnableAdditiveAndPersistent |
| IMAP-ENABLE-03 | RFC 5161 §3.1 | MUST NOT | Enabling extensions leaves CAPABILITY unchanged, including empty and successful UTF8 enablement. | TestEnableUnknownAndCapabilityStability, TestEnableAdditiveAndPersistent |
| IMAP-ENABLE-04 | RFC 5161 §§3.1, 4 | MUST | Enable supported names case-insensitively; repeated/additive ENABLE retains mode across selection and UNSELECT without invoking EnableUTF8 twice. | TestEnableAdditiveAndPersistent |
| IMAP-ENABLE-05 | RFC 5161 §§3.1, 4 | Normative | Missing names or malformed capability atoms/separators receive BAD before any enablement; a valid retry succeeds. | TestEnableInvalidArguments |
| IMAP-ENABLE-06 | RFC 5161 §3.1; RFC 6855 §3 | Normative | Refuse ENABLE before authentication; a backend enablement failure returns NO without ENABLED or enabling mode, allowing a later retry. | TestEnableStateAndFailure |
| IMAP-IDLE-01 | RFC 2177 §§3–4 | Normative | IDLE takes no arguments: extra input receives BAD before any continuation and leaves the selection usable. | TestIdleInvalidArguments |
| IMAP-IDLE-02 | RFC 2177 §3 | MUST | Request a continuation, consume DONE, then immediately complete the original IDLE tag and process the following command; repeated IDLE exchanges preserve framing. | TestIdleDoneAndNextCommand |

The ENABLED list's newly-enabled-only behavior is a SHOULD in RFC 5161 §3.1;
its presence on success is a MUST. ENABLE does not implement CONDSTORE.
The supported UTF8 mode belongs to the session's backend; this proof observes
its enable callback, not a complete RFC 6855 audit. IDLE polling notifications,
concurrent mailbox changes, timeout policy, RFC 9051 IMAP4rev2 differences and
the remaining core/extension requirements still need a complete inventory.

## RFC 4315: APPENDUID — partial response-metadata slice

[RFC 4315](https://www.rfc-editor.org/rfc/rfc4315) §3 supplies the
38505/3955 pair and saved-messages destination; the message is synthetic.
Tests also distinguish the DateAppender result from AppendUID's result.
Persistent UID assignment remains the backend's responsibility.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-UIDPLUS-CAP-01 | RFC 4315 §1 | Normative | Advertise UIDPLUS when the authenticated backend implements it; preserve advertisement after selection. | TestUIDPlusAdvertisement |
| IMAP-APPENDUID-01 | RFC 4315 §3 | SHOULD | Return destination UIDVALIDITY and assigned UID in tagged APPEND OK, including date-aware appends. | TestAppendUIDResponse |
| IMAP-APPENDUID-02 | RFC 4315 §§3–4 | MUST NOT | A single-message APPENDUID contains one UID, never a range or set. | TestAppendUIDResponse |
| IMAP-APPENDUID-03 | RFC 4315 §§3–4 | Normative | Omit unavailable UID metadata; never fabricate zero values. Accept maximum nonzero uint32 values. | TestAppendUIDUnavailable |
| IMAP-APPENDUID-04 | RFC 4315 §3 | Normative | APPENDUID describes success only: backend failure returns NO without it; a retry can succeed. | TestAppendUIDFailureAndRetry |
| IMAP-APPENDUID-05 | RFC 4315 §§3, 5 | SHOULD NOT | Disclose APPENDUID for append-only destinations. | n/a: not supported yet; no destination-readability API, as in IMAP-COPY-15. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-APPENDUID-06 | RFC 4315 §3 | Normative | Suppress APPENDUID for UIDNOTSTICKY destinations. | n/a: not supported yet; backend exposes no UIDNOTSTICKY status. Tracked in rest-mail/rest-mail-server#374. |

## RFC 6154: ordinary LIST attributes — partial slice

[RFC 6154](https://www.rfc-editor.org/rfc/rfc6154) §2 permits these
attributes on ordinary LIST. The engine infers roles from exact folder names;
hosts own their contents. This is not extended LIST support.

| ID | Section | Level | Requirement | Tests |
|----|---------|-------|-------------|-------|
| IMAP-SPECIALUSE-01 | RFC 6154 §2 | MAY | Identify sent-message storage with `\Sent`; engine maps Sent. | TestSpecialUseOrdinaryList |
| IMAP-SPECIALUSE-02 | RFC 6154 §2 | MAY | Identify draft storage with `\Drafts`; engine maps Drafts. | TestSpecialUseOrdinaryList |
| IMAP-SPECIALUSE-03 | RFC 6154 §2 | MAY | Identify discarded-message storage with `\Trash`; engine maps Trash. | TestSpecialUseOrdinaryList |
| IMAP-SPECIALUSE-04 | RFC 6154 §2 | MAY | Identify junk-message storage with `\Junk`; engine maps Junk. | TestSpecialUseOrdinaryList |
| IMAP-SPECIALUSE-05 | RFC 6154 §§2–3 | Normative | Ordinary LIST attributes require no capability; omit unsupported SPECIAL-USE and CREATE-SPECIAL-USE capabilities. | TestSpecialUseCapabilityBoundary |
| IMAP-SPECIALUSE-06 | RFC 6154 §2 | MAY | Mailboxes may have no role; do not invent role folders or infer roles from near matches. | TestSpecialUseDoesNotInventFolders |
| IMAP-SPECIALUSE-07 | RFC 6154 §2 | MUST | Extended LIST selection filters special-use mailboxes; return option reports attributes. | n/a: not supported yet; extended LIST options are unimplemented. Tracked in rest-mail/rest-mail-server#374. |
| IMAP-SPECIALUSE-08 | RFC 6154 §3 | MAY | CREATE assigns requested special-use attributes. | n/a: optional and unsupported; no role-assignment backend API. |
| IMAP-SPECIALUSE-09 | RFC 6154 §4 | MAY | METADATA changes special-use assignments. | n/a: optional and unsupported; no METADATA command or backend API. |
