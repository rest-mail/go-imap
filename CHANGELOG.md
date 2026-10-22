# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Note: pre-1.0, breaking changes may ship in a minor release.

## [Unreleased]

### Fixed

- A MESSAGE/RFC822 entity with a Content-Transfer-Encoding other than 7bit,
  8bit or binary, which RFC 2046 §5.2.1 prohibits, is now one opaque
  APPLICATION/OCTET-STREAM leaf in BODY, BODYSTRUCTURE and numeric FETCH
  sections, and SEARCH BODY matches its decoded text. Its encoded octets used
  to be parsed as if they were the encapsulated message (RFC 3501 §7.4.2).
- MIME nesting is now bounded at depth 64 for BODY, BODYSTRUCTURE, numeric
  FETCH sections and SEARCH alike. A multipart or MESSAGE/RFC822 entity at that
  depth is one opaque APPLICATION/OCTET-STREAM leaf. BODYSTRUCTURE and section
  FETCH used to recurse without a limit, while SEARCH stopped at a different
  depth (RFC 3501 §7.4.2).
- SEARCH BODY and TEXT now decode the encapsulated message of a message whose
  own Content-Type is message/rfc822, as BODYSTRUCTURE already describes it;
  its body used to be searched as stored (RFC 3501 §6.4.4).

- The FETCH section fuzz check now allows filtered headers to join selected
  lines and supply their terminating blank line instead of incorrectly requiring
  a contiguous substring of the stored message.
- A command line with no tag and command now gets an untagged `BAD`; it used to
  be dropped without a reply, so the client waited forever (RFC 3501 §7.1.3).
- LIST and LSUB now prefix a non-empty reference name to the mailbox pattern,
  and an empty pattern returns the reference's root name; the reference used to
  be ignored (RFC 3501 §6.3.8).
- AUTHENTICATE PLAIN now refuses an authorization identity naming another user
  with NO; it used to log in as the authentication identity and ignore the
  requested one (RFC 3501 §6.2.2, RFC 4616 §2).
- LOGIN and AUTHENTICATE now get BAD once the session is authenticated; a
  second LOGIN used to switch user mid-session (RFC 3501 §6.2).
- AUTHENTICATE without a mechanism name now gets BAD, not NO, and CHECK with an
  argument gets BAD instead of OK (RFC 3501 §§6.2.2, 6.4.1).
- LSUB now answers with untagged `LSUB` replies and `OK LSUB completed`; it
  used to send `LIST` replies (RFC 3501 §7.2.3).
- RENAME now carries inferior hierarchical names with the renamed folder, so
  `RENAME Work Play` also moves `Work/Q1` to `Play/Q1` (RFC 3501 §6.3.5).

### Added

- `ErrNoSuchMailbox`: a backend returns it, possibly wrapped, from Append, Copy
  or Move (or their UIDPLUS, Mover and DateAppender forms) when the destination
  does not exist and will not be created implicitly. APPEND, COPY, UID COPY,
  MOVE and UID MOVE then answer `NO [TRYCREATE]` (RFC 3501 §§6.3.11, 6.4.7;
  RFC 6851 §3.3); other backend errors keep a plain NO. IMAP-APPEND-18,
  IMAP-COPY-13, IMAP-MOVE-11 and IMAP-RESP-09 are now mapped; partial
  checklist is 281/331.
- IMAP-GREET-02 (RFC 3501 §§7.1.5, 9): a wire test through the public `Server`
  proves a `Limiter`-refused connection gets a BYE greeting. Partial checklist
  is 277/331 mapped.
- Twenty RFC 3501 §6.5/§7 checklist rows from a dated line-by-line audit of
  server responses: ten mapped (four new wire tests), ten explicit gaps or
  client-only duties. Combined partial checklist is 276/330 mapped.
- Twenty-two RFC 3501 checklist rows from a dated line-by-line audit of §§6.1,
  6.2 (AUTHENTICATE now has its own rows), 6.3.1, 6.3.2, 6.3.10, 6.3.11 and the
  CHECK, CLOSE, EXPUNGE, COPY and UID parts of §6.4: seventeen mapped to four
  new wire tests and existing ones, five n/a with reasons. LIST-04 is now
  mapped. Partial checklist is 266/310 mapped.
- Twenty-two RFC 3501 §§6.3.3–6.3.9 checklist rows from a dated line-by-line
  audit of CREATE, DELETE, RENAME, SUBSCRIBE, UNSUBSCRIBE, LIST and LSUB: ten
  mapped to five new wire tests, twelve explicit gaps from the implicit-folder
  model. Partial checklist is 248/288 mapped.
- Sixteen RFC 4315/6154 checklist rows for APPENDUID and ordinary LIST
  special-use attributes: eleven mapped to seven new public-Session wire tests,
  five explicit API/extension gaps. Partial checklist is 238/266 mapped.
- Thirteen new checked RFC 2177/3691/5161 requirements with public-Session
  wire proofs for IDLE, UNSELECT and ENABLE; partial checklist is 227/250 mapped.
- README badges for the 95% coverage gate and the partial checklist's mapped
  count. `TestReadmeCoverageBadge` reads the minimum from
  `.github/scripts/check_coverage.py`; `TestReadmeChecklistBadge` runs the real
  checker before deriving the count, so neither badge can go stale.
- CI now measures race-test statement coverage across all packages, including
  the external RFC wire proofs, and fails below 95%. Offline fixtures exercise
  the gate's boundary, malformed reports and failed coverage pipelines.
  `COVERAGE.md` records the measurement policy and remaining uncovered branches.
- Session wire tests cover quota and LIST backend failures, mailbox-management
  validation and moves, state refusals, malformed FETCH/STORE/UID requests and
  authentication syntax recovery. Sequential package coverage reaches 96.1%
  on Go 1.25.10, up from the initial CI baseline of 93.0%.
- SEARCH supports KEYWORD and UNKEYWORD (RFC 3501 §6.4.4). No keyword is
  stored, so KEYWORD matches nothing and UNKEYWORD matches every message; a
  missing or non-atom operand is BAD.
- SEARCH BODY, TEXT, HEADER, CC, BCC and SUBJECT match decoded text: base64 and
  quoted-printable parts (including inside MESSAGE/RFC822), ISO-8859-1 content
  and RFC 2047 encoded words are decoded to UTF-8 first (RFC 3501 §6.4.4).
- Wire proofs map IMAP-STARTTLS-08, IMAP-STORE-06, IMAP-SEARCH-10 and
  IMAP-SEARCH-11; the checklist reaches 214/237 mapped rows.

### Fixed

- A connection the `Limiter` refuses is greeted with `* BYE` before the
  server closes it (RFC 3501 §7.1.5), instead of being dropped with no
  greeting. The write runs off the accept loop under a 5-second deadline.
- ENABLE always sends an ENABLED response on success, even when empty, and
  rejects missing or malformed capability atoms before enabling any mode.
- UNSELECT returns BAD without a selected mailbox, as RFC 3691 requires.
  UNSELECT and IDLE reject extra arguments without changing selection state.

- SELECT no longer lists `\*` in PERMANENTFLAGS. Keywords were accepted and
  dropped, so advertising that clients may create them was false
  (RFC 3501 §7.1).

- Five previously unmapped RFC checklist rows now have public-Session wire
  proofs (APPEND-23, FETCH-15, ENV-10/12 and MSG-08): 210 of 237 rows mapped.
  APPEND accepts empty quoted and literal mailbox names without substituting
  INBOX; backend naming policy still applies. ENVELOPE preserves address-group
  boundaries and empty groups, including inside MESSAGE/RFC822. Malformed raw
  headers no longer fabricate fields from cached summaries, and malformed
  encapsulated headers retain the required ten-field envelope structure.
  FETCH refuses raw or decoded NUL before response bytes or Seen writes.
  Composed grammar tests cover mixed MIME sections/header names/partials and
  all APPEND optional-argument layouts, mailbox forms and literal modes.

- SELECT and EXAMINE require exactly one valid mailbox astring. Missing names
  no longer select INBOX implicitly; malformed or extra arguments receive BAD
  before backend access. Explicit empty strings reach the backend unchanged.
  Checked wire tests cover deselection on refusal, recovery without stale
  deletion marks, escaped names and both synchronizing and LITERAL+ mailboxes.

- CLOSE no longer answers OK when the backend refuses to delete a marked
  message (RFC 3501 §6.4.2). It answers NO and keeps the mailbox selected.
  Removals that succeeded are reported as EXPUNGE so sequence numbers stay
  valid, and failed messages keep their Deleted mark so CLOSE can be retried.
  A CLOSE whose deletions all succeed still sends no EXPUNGE.
- FETCH BODY[section] and BODY.PEEK[section] resolve numeric MIME part
  paths (RFC 3501 §6.4.5) instead of returning the whole message: multipart
  parts, part 1 of a non-multipart message, MESSAGE/RFC822 encapsulation
  (N.HEADER, N.TEXT, N.HEADER.FIELDS and nested part numbers) and N.MIME
  headers. Absent parts and message-text specifiers on non-message parts
  return an empty string. Malformed section-specs (zero or leading-zero part
  numbers, overflow, empty components, MIME without a part, unknown names)
  receive BAD before any message is read. Checked wire tests fetch every
  section of the RFC 3501 §6.4.5 example; CI fuzzes the section-spec grammar.

- SEARCH and UID SEARCH validate all six date-key operands as RFC 3501 §9
  date tokens before calendar parsing, instead of trimming malformed padding
  or decoding quoted escapes. Invalid dates receive BAD before content reads
  or results, including Boolean/groups and empty mailboxes. Checked wire
  proofs and a grammar/calendar oracle fuzz target cover valid bare/quoted
  forms, leap years and strict day/month/year shapes. General SEARCH literal
  and separator grammar remain separate gaps.

- SEARCH and UID SEARCH retain escaped quotes and backslashes inside string
  operands rather than splitting at an escaped quote. Unterminated quotes,
  invalid escapes, NUL, quotes embedded in bare tokens and text adjoining a
  closing quote receive BAD before evaluation. Checked wire tests cover
  Boolean/groups, empty/read-only mailboxes, recovery and literal preservation;
  CI fuzzes the quoted-token grammar. Full wire character and separator
  validation remain separate gaps.

- APPEND validates the RFC 3501 fixed-width date-time grammar before accepting
  message bytes. Bare single-digit days, extra spaces, fractional seconds,
  backslash escapes and invalid zone values now receive BAD. Space-padded and
  zero-padded days remain accepted. Explicit dates equal to Go's zero time now
  reach DateAppender instead of being mistaken for an omitted argument. Adds
  checked wire proofs for acceptance, refusal, recovery and LITERAL+ framing.

- APPEND rejects invalid unquoted mailbox-name octets before inviting message
  data or calling the backend (RFC 3501 §9). Controls, non-ASCII octets,
  wildcards and quoted-specials require a string rather than an unquoted name;
  valid astring punctuation remains accepted. Checked by IMAP-APPEND-30, with
  octet-by-octet wire tests and non-synchronizing payload refusal tests.

- APPEND accepts a mailbox name sent as a synchronizing or LITERAL+ literal
  before the message literal (RFC 3501 §§4.3, 9), instead of misreading the
  name's octets. An empty, oversized or NUL-containing name receives BAD, and a
  refusal with an unread non-synchronizing message literal still closes the
  session. Checked by IMAP-APPEND-21.

- FETCH and UID FETCH strictly validate HEADER.FIELDS[.NOT] lists before any
  reads, Seen writes or responses, including empty/nonmatching sets. Quoted and
  literal names now match decoded values instead of their quote characters;
  significant spaces remain significant. Response labels preserve valid astring
  tokens, including escaped quotes, backslashes and bracket characters. Adds
  checked wire tests and a grammar-based header-list fuzz target. Numeric MIME
  sections and other section names remain separate gaps.

- FETCH and UID FETCH reject unknown data-item names, empty/malformed outer
  lists, multiple bare items and macros inside lists or mixed with other items.
  Validation precedes content access, Seen writes and responses, including with
  no matching messages. Checked RFC 3501 wire tests compare standalone macros
  and singleton items with their explicit lists, including exact literals and
  side effects; a new parser fuzz target also checks the flat-item grammar.
  Numeric MIME sections and complete header-field-list grammar remain separate
  gaps.

- SEARCH and UID SEARCH now support HEADER, CC, BCC, SENTBEFORE, SENTON and
  SENTSINCE, previously rejected as unknown keys. They read the message's raw
  header block: header strings match the unfolded field text after the colon,
  and the SENT keys compare the Date: header's calendar date. Malformed
  operands receive BAD before evaluation. Checked wire tests cover
  IMAP-SEARCH-19/20/21. KEYWORD/UNKEYWORD remain a documented gap.

- FETCH and UID FETCH now reject malformed BODY/BODY.PEEK partial ranges with
  BAD before reading content, changing Seen or sending a FETCH response, even
  with no matching messages. Requests require `<origin.count>` with unsigned
  32-bit operands and a nonzero count without leading zeroes, per RFC 3501 §9.
  Response-only `<origin>` forms and signed/overflowing operands are rejected.
  Valid ranges keep their exact octets, truncation and origin-only response
  labels, including on 32-bit hosts. Checked wire tests and a grammar-based
  parser fuzz target cover this slice; other FETCH grammar gaps remain.

- STATUS now validates its complete mailbox/item-list syntax before reading
  metadata. Trailing arguments, malformed astrings and doubled, leading,
  trailing or non-SP separators receive BAD instead of being ignored or
  accepted. Checked RFC 3501 wire tests cover the STATUS example, requested
  items, quoted/literal names, recovery and preserved SELECT/EXAMINE state.
  Recent tracking and persistent UIDNEXT remain documented gaps.

- SEARCH and UID SEARCH now support LARGER and SMALLER using the complete
  host-supplied RFC822.SIZE octet count. Comparisons are strict; zero, leading
  zeroes and the unsigned 32-bit operand boundary follow RFC 3501. Malformed
  operands receive BAD before evaluation, including inside Boolean/group keys
  and empty mailboxes. SEARCH no longer strips trailing non-SP whitespace
  before numeric validation. Checked wire tests cover IMAP-SEARCH-17/18.

- SEARCH and UID SEARCH now support ANSWERED, UNANSWERED, DRAFT and UNDRAFT
  using current selected-session system flags, including after STORE updates.
  Checked RFC 3501 wire tests cover Boolean combinations, sparse UIDs, empty
  and read-only mailboxes, and refused backend flag changes.

- COPY, UID COPY, MOVE and UID MOVE now omit COPYUID when the backend reports
  a zero or repeated destination UID, instead of quoting it. RFC 4315 allows
  only nonzero, distinct UIDs, and `Mover.MoveUID` may return 0 when the
  backend cannot report one. The copy or move itself still succeeds. Checked
  wire tests cover IMAP-COPY-16 and IMAP-MOVE-14.

- APPEND now validates quoted mailbox names against RFC 3501's escape and
  character grammar before inviting the message literal. Invalid escapes,
  NUL and 8-bit octets receive BAD; escaped quotes and backslashes still
  deliver to their decoded mailbox names.

- APPEND now rejects NUL inside a counted message literal with BAD before
  calling the backend. RFC 3501's CHAR8 grammar excludes NUL; checked wire
  tests cover both literal modes and keep the following command synchronized.

- APPEND now requires exactly one SP between the mailbox, flag-list, date-time
  and message literal, answering BAD before sending a continuation. Doubled,
  leading and missing separators (for example `INBOX{3}` or
  `INBOX(\Seen) {3}`) were previously skipped and the message stored. Checked
  RFC 3501 wire tests keep every single-SP combination working.

- APPEND now rejects a malformed flag-list with BAD before sending a
  continuation. Nested parentheses, doubled, leading, trailing or tab
  separators, a list not followed by SP, and flags that are not an atom with an
  optional leading `\` were previously split on whitespace and stored or
  refused as an unsupported flag. Checked RFC 3501 wire tests keep empty lists
  and supported flags working.

- STORE and UID STORE now reject a malformed flag operand with BAD before
  changing flags. Unbalanced or nested parentheses, doubled or tab separators,
  quoted strings and flags that are not an atom with an optional leading `\`
  (such as `\*`, `k]` or non-ASCII names) were previously split on whitespace
  and applied. Checked RFC 3501 wire tests cover every action with and without
  .SILENT, and preserve empty lists, bare flags, keywords and flag-extensions.

- SEARCH and UID SEARCH now reject malformed bare sequence-set keys and UID
  operands with BAD before evaluating content, including inside NOT, OR and
  parenthesized groups. Invalid list/range members previously could be ignored
  or resolved as zero. Adds checked RFC 3501 wire tests for rejection, session
  recovery and valid reversed/star/overlapping/unsigned-32-bit boundary sets.
- STORE and UID STORE now reject malformed sequence sets with BAD before
  changing flags, rather than silently applying valid portions of malformed
  lists/ranges. Checked RFC 3501 wire tests prove rejection without backend or
  session side effects, including .SILENT, empty selections and recovery, and
  preserve valid reversed/star/unsigned-32-bit boundary sets. Flag-list parsing
  and out-of-range sequence-number policy are unchanged.

- FETCH and UID FETCH now reject malformed sequence sets with BAD before loading
  content or marking messages Seen, rather than treating invalid numbers as zero
  or accepting malformed list/range members. Adds checked RFC 3501 wire tests
  for rejection without side effects, session recovery and valid reversed/star/
  unsigned-32-bit boundary sets. Data-item parsing is unchanged.

- COPY, UID COPY, MOVE and UID MOVE now answer BAD before dispatch when the
  sequence-set does not match the RFC 3501 §9 grammar, the destination is not a
  well-formed astring (bad quoted escapes, NUL, list wildcards or other
  atom-specials, non-ASCII atoms) or text follows it. Malformed sets used to be
  skipped silently and could copy or move the wrong messages. Adds checked
  RFC 3501/6851 wire tests.

- UID EXPUNGE now rejects a UID set that does not match the RFC 3501 §9
  sequence-set grammar (zero, leading zeros, values above 2^32-1, empty items
  or bounds, trailing arguments) with BAD before deleting anything, instead of
  silently treating malformed numbers as zero. Adds checked RFC 4315 wire tests
  for invalid and boundary-valid sets.

- BODYSTRUCTURE now ignores Content-Disposition comments between lexical
  tokens (RFC 2183 §2) instead of reporting NIL for valid comment-bearing
  fields, sharing the Content-Type comment lexer. A type/subtype pair is no
  longer accepted as a disposition type. Adds checked RFC 2183/822 wire tests
  for nested/folded comments, quoted parameter data, per-entity scope and
  malformed-token recovery.

- BODY/BODYSTRUCTURE now remove surrounding lexical whitespace and comments
  from valid Content-Transfer-Encoding tokens rather than advertising them as
  part of the encoding name. Adds checked RFC 2045/822 wire tests for nested
  comments, per-entity scope, malformed-value compatibility and encoded sizes,
  plus a parser fuzz target.

- BODY/BODYSTRUCTURE now ignore Content-Type comments between lexical tokens
  and accept whitespace around the type/subtype slash. Valid comment-bearing
  fields no longer fall back to TEXT/PLAIN or lose multipart children. Quoted
  parameter data stays intact; malformed comments and split tokens retain
  invalid-type recovery. Adds checked RFC 2045/822 wire tests and a differential
  parser fuzz target run for 30 seconds in CI.

- BODY/BODYSTRUCTURE no longer invent a PLAIN subtype for a bare Content-Type
  token such as `application`. Missing subtypes now use the existing invalid-type
  fallback. Adds checked RFC 2045/2046/3501 wire tests for ordinary MIME defaults,
  explicit types/charsets, and invalid type recovery through multipart children
  and encapsulated messages, preserving unrelated headers and encoded sizes.

- BODYSTRUCTURE now reports Content-Language in the language extension slot
  instead of always returning NIL: one tag as a string, several as a list,
  comments dropped. Adds checked RFC 3501/RFC 3282 wire tests, including
  per-entity values in multiparts and encapsulated messages.

- BODYSTRUCTURE now reports supplied Content-MD5 and Content-Location in their
  extension slots instead of always returning NIL, including multipart locations
  and encapsulated bodies. BODY still omits extensions. Adds checked RFC 3501
  wire tests for field order, per-entity values, quoted/literal framing and
  metadata side effects. Digests are not generated or verified.

- ENVELOPE address personal names, mailboxes and hosts now use counted
  literals for 8-bit octets and CR/LF decoded from RFC 2047 encoded words,
  instead of placing them in quoted strings where a decoded CRLF could inject
  a response line. Applies to top-level and MESSAGE/RFC822 envelopes. Adds
  checked RFC 3501 row IMAP-ENV-13 with public-Session wire tests.

- BODY/BODYSTRUCTURE MIME strings now use counted literals for 8-bit and
  decoded CRLF values instead of placing those octets in quoted strings.
  Empty content-type and disposition parameter values remain empty strings
  instead of NIL. Adds checked RFC 3501 public-Session wire tests for header
  fields, RFC 2231 parameters, dispositions and recursive MIME structures.

- BODY/BODYSTRUCTURE now describe a multipart/digest child without
  Content-Type as MESSAGE/RFC822 (RFC 2046 §5.1.5) instead of TEXT/PLAIN,
  including its encapsulated envelope, body structure and line count. Adds an
  RFC 3501 checklist row and public-Session tests for the default and its scope.

- MESSAGE/RFC822 nested envelopes now retain original Date text, distinguish
  empty scalar headers from missing headers, and use counted literals for
  scalar octets that cannot appear in quoted strings. Adds partial RFC 3501
  checklist rows and public-Session tests for encapsulated envelope/body/line
  ordering, recursive structures, extension ordering and metadata side effects.

- Multipart BODY/BODYSTRUCTURE now retain quoted-printable transfer encoding,
  encoded octet sizes and physical line counts instead of silently decoding
  parts and reporting them as 7BIT. Adds partial RFC 3501 §7.4.2 checklist rows,
  public-Session wire tests and both RFC example structures with synthetic bodies.
- Top-level FETCH ENVELOPE now takes Subject and the original Date text from
  the raw headers. Missing Date/Subject/From/To no longer fabricate values from
  cached metadata or INTERNALDATE; empty scalar headers remain empty strings.
  Scalar header strings escape quoted-specials and use counted literals for
  8-bit octets. Adds partial RFC 3501 ENVELOPE checklist rows and public-Session
  wire tests, including the §8 example. Address groups, source routes, address
  literal framing and malformed nested BODYSTRUCTURE headers remain gaps.

- STARTTLS with arguments now returns BAD instead of starting a TLS negotiation
  (RFC 3501 §6.2.1). Adds partial RFC 3501/RFC 2595 STARTTLS checklist rows and
  public-Session wire tests over a real TLS handshake.
- MOVE and UID MOVE now stop at the first backend failure and return NO, still
  sending EXPUNGE (and COPYUID) for the messages already moved, instead of
  ignoring failures and reporting successful completion.
- Overlapping UID MOVE sets move each message once, preventing duplicate backend
  moves, duplicate COPYUID members and repeated EXPUNGEs that dropped an
  unrelated message from the session view. Adds partial RFC 6851 checklist rows
  and public-Session wire tests.
- COPY and UID COPY now stop at the first backend failure and return NO without
  COPYUID, instead of ignoring failures and reporting successful completion.
  Earlier destination copies cannot be rolled back through the current backend
  API; that RFC gap is documented explicitly.
- Overlapping UID COPY sets copy each source message once, preventing duplicate
  destination messages and duplicate source UIDs in COPYUID. Adds partial RFC
  3501/RFC 4315 checklist rows and public-Session wire tests with RFC examples.

- An overlong initial command line ending in a LITERAL+ `{n+}` marker now
  sends BAD then BYE and ends the session, instead of reading the in-flight
  literal octets as new commands. Only the line's last octets are kept, so
  the check stays constant-memory. Other overlong lines, including ones with a
  synchronizing marker, keep the connection usable. Adds checklist rows
  IMAP-LITERAL-08/09 and public-Session wire tests across read-buffer splits.

- Refused non-synchronizing command-argument literals now send BYE and end
  the session instead of dispatching unread payload as commands. This covers
  individual/aggregate literal bounds and expanded-command bounds. Overlong
  literal-continuation lines also close because their discarded suffix may
  hide another literal. Literal read errors end the session. Safe refusals
  before a synchronizing continuation or after a complete command keep the
  connection usable.
  Adds partial checklist rows and public-Session wire tests.

- Early APPEND refusal with a valid `{n+}` literal marker now sends BYE and
  ends the session instead of interpreting unread message bytes as commands.
  Synchronizing refusals and backend failures after complete message reads
  preserve the connection. Adds checked wire tests for pipelined payloads,
  header-only refusals and consumed-literal backend failures.

- APPEND validates its complete terminal message-literal marker instead of
  ignoring extra arguments or accepting signed lengths. Accepted `{n+}` message
  literals no longer receive an incorrect continuation.
- APPEND requires the command's CRLF before invoking the backend. Incomplete
  reads and malformed terminators close the connection without storing a
  message, instead of ignoring terminator read failures or trailing text.
- APPEND recognizes supported system flag names case-insensitively.

- EXPUNGE and UID EXPUNGE now return NO when any backend deletion fails,
  preserving failed messages and their deletion marks for retry. Successful
  removals still emit EXPUNGE responses; multi-message removal is not atomic.
- EXPUNGE and CLOSE reject extra arguments with BAD before changing state.
  EXPUNGE outside the selected state now returns BAD rather than NO.

- FETCH and UID FETCH now return NO when requested message data cannot be
  loaded, instead of OK with omitted data or a partial ENVELOPE fallback.
- Implicit Seen writes must succeed before FETCH emits the changed flags or
  body; backend refusal returns NO without phantom session flags. EXAMINE
  body responses no longer echo Seen when no flag change was made.

- SEARCH and UID SEARCH compare BEFORE/SINCE against the message's internal
  calendar date, ignoring its time and timezone as RFC 3501 requires, instead
  of comparing UTC instants. ON uses the same date-only comparison.
- SEARCH and UID SEARCH reject missing keys and empty parenthesized groups
  with BAD, including empty groups nested under NOT/OR, instead of treating
  them as valid criteria.

- `STORE` and `UID STORE` now answer `NO` when the backend refuses a flag
  write, and restore the message's session flags, instead of reporting `OK`
  with flags the mailbox never stored.
- SELECT/EXAMINE's `UNSEEN` response now identifies the first unseen message's
  sequence number, rather than the unread count. Empty or fully seen mailboxes
  omit the code instead of emitting the invalid sequence number zero.
- Non-peek `FETCH` and `UID FETCH` no longer set `\Seen` in a mailbox opened
  by `EXAMINE`, preserving both backend and session flags in read-only mode.

### Added

- Optional `UTF8Mailbox` interface for RFC 6855 `UTF8=ACCEPT`. When the
  authenticated `Mailbox` implements it, CAPABILITY lists `UTF8=ACCEPT` and
  `ENABLE UTF8=ACCEPT` (authenticated state only) calls `EnableUTF8` once
  and answers `* ENABLED UTF8=ACCEPT`. A backend error fails ENABLE with NO
  and leaves the mode off. Backends without it are unchanged. UTF-8 mailbox
  names and the APPEND `UTF8` data extension are not implemented yet.
- Checklist rows IMAP-LITERAL-10/11 with public-Session wire tests: an
  overlong AUTHENTICATE response is refused with tagged BAD before any
  credential check, and an overlong line while idling is ignored without
  ending IDLE. Both keep the session whatever the line ends in, since neither
  position admits a literal (RFC 3501 §9, RFC 2177 §3). No behavior change.

- Partial APPEND checklist and public-Session wire proofs for the RFC 3501
  example, message octets, flags/dates, selected-state notifications, backend
  refusal, literal grammar, non-synchronizing continuation behavior and
  incomplete commands. Storage, flag, date, grammar and extension gaps remain
  explicit in CHECKLIST.md.

- Partial EXPUNGE/CLOSE and RFC 4315 UID EXPUNGE checklist rows and external
  wire proofs for RFC examples, sequence renumbering, UID filtering, backend
  failure/retry, read-only operation, arguments and selection state. Persistent
  Deleted flags, CLOSE deletion failures and strict UID-set validation are
  explicit gaps.

- A partial FETCH/UID FETCH checklist and external wire proofs for literal
  octets, sections/aliases, header subsets, partial ranges, metadata/macros,
  sparse UID replies, backend errors and read-only flags, including the RFC
  3501 §§6.4.5/6.4.8 examples. Numeric MIME sections and strict argument
  validation are explicit unsupported gaps.

- A partial SEARCH/UID SEARCH checklist and public-session wire proofs for
  RFC 3501 examples, Boolean criteria, sparse UID/sequence sets, calendar-date
  boundaries, charsets, malformed criteria and selection state. MIME decoding,
  unsupported standard keys and independent Recent search are explicit gaps.

- Checked STORE/UID STORE rows and public-session wire tests, including the
  RFC 3501 §6.4.6 example, .SILENT, UID reporting and backend refusal. Keyword
  storage is recorded as unsupported.
- Checked SELECT/EXAMINE selection-state rows and public-session wire tests,
  including the RFC 3501 examples, failed/replacement selection and read-only
  write prevention. Recent, persistent UID metadata and permission gaps are
  explicitly recorded; the checklist remains partial.

- **A partial, checked RFC 3501 checklist** (`CHECKLIST.md`). Its first seven
  rows cover the greeting, CAPABILITY, NOOP, LOGOUT, LOGIN and tagged
  completion; each names a wire test in `internal/rfctest` that drives a public
  `Session`. CI runs `go run ./internal/checklist internal/rfctest`, which fails
  on a row with no test, a test naming an unknown row, or a duplicate ID.

## [0.4.0] - 2026-07-26

### Added

- **`SEARCH BODY`, `TEXT` and `RECENT` now match a real subset** (RFC 3501
  §6.4.4). `BODY <string>` matches a case-insensitive substring in the message
  body; `TEXT <string>` matches the header or the body; `RECENT` matches the
  `\Recent` messages, which this engine models as the unseen set (the same set it
  reports as RECENT in `SELECT`/`EXAMINE` and `STATUS`). The message body is
  loaded lazily, so a metadata-only search never fetches one.
- **`FETCH BODYSTRUCTURE` now emits Content-Disposition and MESSAGE/RFC822
  encapsulation** (RFC 3501 §7.4.2). The basic structure — type, subtype,
  content-type parameters, id, description, encoding, size, the TEXT line count,
  and the multipart recursion — already existed; this completes it. The
  disposition field (`body-fld-dsp`) now reflects the part's
  `Content-Disposition` header as `(disp-type (params))` — e.g.
  `("ATTACHMENT" ("FILENAME" "doc.pdf"))` — on both single parts and multiparts,
  instead of a blanket `NIL` (a disposition with no parameters renders
  `(disp-type NIL)`; an absent or unparseable header stays `NIL`). A
  `MESSAGE/RFC822` part now appends the envelope, body structure and line count
  of the encapsulated message, parsed from the part body and described
  recursively. The non-extensible `BODY` form is unchanged in shape but now also
  carries the `MESSAGE/RFC822` envelope/body/lines, which are part of the basic
  grammar rather than extension data.

### Fixed

- An overlong initial command line ending in a LITERAL+ `{n+}` marker now
  sends BAD then BYE and ends the session, instead of reading the in-flight
  literal octets as new commands. Only the line's last octets are kept, so
  the check stays constant-memory. Other overlong lines, including ones with a
  synchronizing marker, keep the connection usable. Adds checklist rows
  IMAP-LITERAL-08/09 and public-Session wire tests across read-buffer splits.

- **The `ENVELOPE` recipient and reference fields are populated from the message
  headers** (RFC 3501 §7.4.2). `To`, `Cc`, `Bcc`, `In-Reply-To` and `Message-ID`
  were previously always `NIL`; they are now parsed from the stored message (an
  absent header stays `NIL`, so a message with no `Bcc` reports `NIL` as before).
- **The `ENVELOPE` date now comes from the message's `Date:` header** rather than
  its arrival time (RFC 3501 §7.4.2). `INTERNALDATE` still reports the arrival
  time — the two are distinct — and a missing or unparseable `Date:` header falls
  back to the arrival time.

## [0.3.0] - 2026-07-25

A minor release that collects the IMAP4rev1 correctness pass across the server's
command set — `STORE`/`UID STORE` flag semantics (RFC 3501 §6.4.6), `APPEND`
argument parsing (§6.3.11), general synchronizing-literal handling (§4.3) so a
`{n}` literal works as any string argument (not only `APPEND`'s message), `FETCH`
sections and partial ranges (§6.4.5), `SEARCH` key validation (§6.4.4), `STATUS`
item selection (§6.3.10), `LIST`/`LSUB` case-sensitivity and response encoding
(§5.1, §4.3), `IDLE` change reporting (RFC 2177), the connection greeting's
advertised capabilities, and `AUTHENTICATE` continuation/abort handling — together
with denial-of-service hardening (bounded command lines and literals, a
linear-time wildcard matcher, and per-connection panic recovery). It adds two
exported fields (`Message.Answered`, `FlagUpdate.Answered`), one optional interface
(`DateAppender`), and two tunable package variables (`MaxLiteralSize`,
`MaxCommandLineLength`); these are backward-compatible additions. It makes one
breaking change to `Server.Shutdown` (see Breaking).

### Breaking

- **`Server.Shutdown` now takes a `context.Context` and returns an `error`, and
  actually drains in-flight sessions.** It previously waited only on the
  accept-loop goroutines — which return the moment their listener closes — so it
  returned while client sessions were still being served, contradicting its
  "waits for in-flight sessions to finish" documentation. Each accepted connection
  is now tracked in the server's `sync.WaitGroup`, and `Shutdown(ctx)` closes the
  listeners and then blocks until every session finishes, or until `ctx` is done
  (returning `ctx.Err()`), mirroring `net/http.Server.Shutdown`. Callers of the
  old no-argument `Shutdown()` must pass a context (e.g.
  `srv.Shutdown(context.Background())`).
- **New `Server.Close() error` for an immediate hard stop:** it force-closes live
  connections without waiting, for callers that want the old fire-and-forget
  behaviour rather than a graceful drain.

### Added

- `Message.Answered` and `FlagUpdate.Answered` so the `\Answered` system flag,
  already advertised in `FLAGS`/`PERMANENTFLAGS`, can actually be stored and
  reported.
- `DateAppender`, an optional `Mailbox` interface. When the concrete mailbox
  implements it, a client-supplied `APPEND` date-time is passed through so the
  store can set the message's `INTERNALDATE`; backends that do not implement it
  are unchanged (the date is parsed and validated but not stored).
- `MaxLiteralSize`, a tunable package variable bounding a synchronizing literal
  used as an ordinary string/astring command argument (see the `Fixed` note on
  general literal handling). Defaults to 8 KiB; `APPEND`'s message literal keeps
  its own separate, larger bound.
- `MaxCommandLineLength`, a tunable package variable bounding a single command
  line (defaults to 8 KiB), so an unauthenticated client cannot exhaust server
  memory with one unbounded line.

### Fixed

- An overlong initial command line ending in a LITERAL+ `{n+}` marker now
  sends BAD then BYE and ends the session, instead of reading the in-flight
  literal octets as new commands. Only the line's last octets are kept, so
  the check stays constant-memory. Other overlong lines, including ones with a
  synchronizing marker, keep the connection usable. Adds checklist rows
  IMAP-LITERAL-08/09 and public-Session wire tests across read-buffer splits.

- `LIST`/`LSUB` pattern matching is now case-sensitive for every mailbox name
  except `INBOX` (RFC 3501 §5.1). The matcher previously lower-cased both the
  pattern and the folder name, so a pattern such as `sent` wrongly matched a
  folder named `Sent`. `INBOX` — and an `INBOX`-spelled pattern — is still folded
  to the canonical spelling, so any-case `inbox` continues to list the one real
  `INBOX`.
- `SUBSCRIBE` and `UNSUBSCRIBE` (RFC 3501 §6.3.6/§6.3.7), mandatory IMAP4rev1
  commands, are now accepted instead of drawing a `BAD "Unknown command"`. This
  engine keeps no separate subscription list — `LSUB` reports every existing
  folder as subscribed — so the commands validate their mailbox argument and
  acknowledge with a tagged `OK`.
- `APPEND` into the mailbox the session currently has selected now emits an
  untagged `* n EXISTS` (RFC 3501 §6.3.11) and refreshes the cached message set,
  so the client is told a message arrived and later sequence-number references
  stay correct. Appends to any other mailbox remain invisible to the current
  selection.
- `IDLE` now reports external mailbox changes, not only new-message growth
  (RFC 2177). While a client idles, the poll diffs the mailbox against its cached
  message set and pushes an untagged `* n EXPUNGE` for each message another
  session removed — highest sequence number first, per the RFC 3501 §7.4.1
  renumbering rule — an `* n EXISTS` for new arrivals, and an `* n FETCH (FLAGS …)`
  when a surviving message's flags change, then updates the cache. Previously the
  poll only announced a larger count, so an external expunge was never reported
  and the cached set (which backs sequence-number resolution for every later
  command) went stale.
- A synchronizing literal (`{n}`, RFC 3501 §4.3) is now accepted as any
  string/astring argument of any command — `LOGIN`, `SELECT`, `CREATE`, `STATUS`,
  `SEARCH`, and so on — not only as `APPEND`'s message. The command reader detects
  a trailing `{n}`, sends the required `+` continuation, reads exactly `n` octets
  as the argument value (8-bit clean; an embedded space or quote is preserved),
  then continues parsing the rest of the command. Previously only `APPEND` handled
  a literal, so a literal anywhere else drew no continuation and its octets were
  misparsed as a new command, desyncing the connection. The non-synchronizing
  `LITERAL+` form (`{n+}`, RFC 2088) is accepted without a continuation. A literal
  used as an ordinary argument is bounded by `MaxLiteralSize` (checked before the
  continuation, so an over-large one is refused without its octets being read);
  `APPEND`'s large-message literal path is unchanged.
- `STORE FLAGS (...)` (replace mode) now sets the message's flags to exactly the
  given set, per RFC 3501 §6.4.6. Previously bare `FLAGS` was mishandled as a
  removal, so `STORE 1 FLAGS (\Flagged)` cleared `\Flagged` instead of setting
  it. `+FLAGS`/`-FLAGS` continue to add/remove.
- The `.SILENT` suffix now suppresses the untagged `FETCH` response; it was
  previously ignored so the untagged `FETCH` was always emitted.
- `STORE` now handles `\Answered` and `\Draft` instead of silently dropping them
  (only `\Seen`, `\Flagged` and `\Deleted` were handled before).
- A pending `\Deleted` mark now appears in `FETCH`/`STORE` flag lists; `buildFlags`
  previously omitted the session-local `\Deleted` state.
- `UID STORE` shares the same corrected flag logic as `STORE`.
- `APPEND` now accepts an unquoted (atom) mailbox name, per the `astring` grammar
  of RFC 3501 §6.3.11. Previously only a quoted name was recognised, so `APPEND
  Archive {n}` was ignored and the message misdelivered to `INBOX`. Quoted and
  literal names continue to work.
- `APPEND`'s optional flag-list and date-time are now parsed positionally after
  the mailbox rather than by scanning for the first `(`/`"`, so a parenthesis or
  quote inside a mailbox name is no longer mistaken for them. `\Answered` is now
  applied (it was silently dropped), and a flag the store cannot honour is
  rejected with a tagged `NO` instead of being discarded.
- `APPEND`'s optional date-time is now parsed and validated; with a `DateAppender`
  backend it sets the stored message's `INTERNALDATE`. It was previously ignored.
- The reserved mailbox name `INBOX` is now matched case-insensitively, per RFC
  3501 §5.1. Previously only the exact spelling `INBOX` was recognised, so
  `DELETE inbox` bypassed the standard-folder guard, `CREATE INBOX` (any case)
  was wrongly accepted, and `SELECT`/`EXAMINE`/`STATUS` of `inbox` depended on
  the backend to normalise. Any-case `INBOX` now resolves to the one real INBOX
  across `SELECT`/`EXAMINE`/`STATUS`/`CREATE`/`DELETE`/`RENAME`/`APPEND`/`COPY`/
  `MOVE`. All other mailbox names remain case-sensitive.
- Quoted-string arguments now have their backslash escapes decoded on input, per
  RFC 3501 §4.3: `\"` becomes a literal `"` and `\\` a literal `\`, and an escaped
  `\"` no longer terminates the string early. A `LOGIN` password (or mailbox name)
  containing a quote or backslash previously reached the backend mangled, so
  authentication failed silently.
- `SEARCH`/`UID SEARCH` no longer treat an unrecognised search key as `ALL`
  (RFC 3501 §6.4.4). An unknown key, a missing key argument, unbalanced
  parentheses, or a malformed `BEFORE`/`SINCE`/`ON` date (e.g. `SINCE 99-Foo-2020`)
  are now answered with a tagged `BAD` instead of silently matching every message.
  An unsupported `CHARSET` is answered `NO [BADCHARSET (US-ASCII UTF-8)]`;
  `US-ASCII` and `UTF-8` are accepted. Parenthesized key groups such as
  `(FROM "a" SUBJECT "b")` are parsed as the AND of the enclosed keys rather than
  mis-tokenised, and a bare message sequence set (e.g. `1,3:5`) is honoured as a
  valid search key.
- `STATUS` now honours the requested status-item list and returns exactly those
  items (RFC 3501 §6.3.10). Previously the list was ignored and a fixed
  `MESSAGES RECENT UNSEEN` set was always returned; `UIDNEXT` and `UIDVALIDITY`
  were unsupported. All five items — `MESSAGES`, `RECENT`, `UNSEEN`, `UIDNEXT`
  (highest UID + 1), `UIDVALIDITY` (the mailbox's real value on a UIDPLUS backend,
  else 1, matching `SELECT`/`EXAMINE`) — are now reported, an unknown item is
  rejected with `BAD`, and `STATUS` neither selects the mailbox nor sets `\Seen`.
- `AUTHENTICATE`'s continuation request is now the grammar-required `"+" SP`
  form (RFC 3501 §7.5 / §6.2.2, RFC 4959): `+ ` followed by the (empty, for
  PLAIN) base64 server challenge, rather than a bare `+`. A client that aborts
  the SASL exchange by sending a lone `*` is now answered with a tagged `BAD`
  ("authentication aborted"), per RFC 4959; previously the `*` was base64-decoded,
  the decode failed, and the abort was mis-reported as `NO "Invalid base64"`,
  conflating a deliberate cancel with a bad-credential decode error. A genuine
  wrong-credential response is still a tagged `NO [AUTHENTICATIONFAILED]`.
- `CHECK` (RFC 3501 §6.4.1) and `CLOSE` (§6.4.2) are now rejected with a tagged
  `BAD` when no mailbox is selected, rather than answering `OK`. Both are
  Selected-State commands: `CHECK` in the non-authenticated or authenticated
  (no mailbox selected) state, and `CLOSE` with nothing selected, previously
  completed `OK`. When a mailbox is selected `CHECK` still completes `OK` and
  `CLOSE` still expunges `\Deleted` messages (unless read-only) and returns to
  the authenticated state.
- The connection greeting's `[CAPABILITY ...]` code is now built from the live
  session state instead of a hardcoded `IMAP4rev1 STARTTLS AUTH=PLAIN`, and
  `LOGINDISABLED` is advertised whenever plaintext `LOGIN`/`AUTHENTICATE` is
  refused until TLS (RFC 3501 §7.1.1, RFC 2595). Previously the greeting offered
  `STARTTLS` even with no TLS configured (where it is refused) and offered
  `AUTH=PLAIN` while omitting `LOGINDISABLED` on a cleartext link to a
  TLS-requiring server, so the advertised capabilities disagreed with what the
  server would accept. The greeting, the untagged `CAPABILITY` response, and the
  post-login `[CAPABILITY ...]` code now share one dynamic list; after `STARTTLS`
  the `STARTTLS`/`LOGINDISABLED` tokens drop and `AUTH=PLAIN` appears.
- Mailbox names in `LIST`, `LSUB`, `STATUS`, and `QUOTAROOT` responses are now
  encoded per the `astring` grammar (RFC 3501 §4.3): a name containing a space,
  quote, backslash, or other special character is quoted or sent as a literal
  rather than emitted bare, where a client would otherwise mis-parse the response.
- `FETCH BODY[...]<start.count>` now returns only the requested partial range of
  the section and labels the response with the origin octet as
  `BODY[...]<start>` (RFC 3501 §6.4.5). Previously the partial specifier was
  parsed but ignored and the whole section was returned.
- `FETCH BODY[HEADER.FIELDS.NOT (...)]` now returns every header except those
  listed, and both `HEADER.FIELDS` and `HEADER.FIELDS.NOT` preserve multi-line
  (folded) header values instead of dropping the continuation lines.
- A `FETCH` of a body section that implicitly sets `\Seen` now reflects the new
  flag in the same response's `FLAGS` (RFC 3501 §6.4.5), so the client learns the
  message became read without issuing a separate fetch.
- `SELECT`/`EXAMINE` now report `UIDNEXT` as the highest existing UID plus one
  (RFC 3501 §2.3.1.1) instead of a placeholder, so a client can predict the UID
  the next appended message will receive.
- A failed `SELECT` or `EXAMINE` now leaves the session in the authenticated,
  no-mailbox-selected state rather than half-selecting the target, so a later
  command cannot operate on a mailbox that was never successfully opened.
- The server now sends an untagged `* BYE` before closing a connection that has
  passed the inactivity autologout timer (RFC 3501 §5.4), so the client is told
  the session was terminated rather than seeing an abrupt disconnect.
- A single command line is now bounded by `MaxCommandLineLength` (8 KiB by
  default), so an unauthenticated client can no longer exhaust server memory by
  sending one unbounded line.
- The `LIST`/`LSUB` wildcard matcher now runs in linear time, removing a
  super-linear matching path an authenticated client could exploit with a crafted
  pattern to pin CPU.
- A panic while serving one connection is now recovered and confined to that
  connection, so a single malformed client can no longer crash the whole server.

## [0.2.3] - 2026-07-25

Correctness fixes for FETCH, EXAMINE and sequence-set handling. No exported API
changes — this is a drop-in patch release.

### Fixed

- An overlong initial command line ending in a LITERAL+ `{n+}` marker now
  sends BAD then BYE and ends the session, instead of reading the in-flight
  literal octets as new commands. Only the line's last octets are kept, so
  the check stays constant-memory. Other overlong lines, including ones with a
  synchronizing marker, keep the connection usable. Adds checklist rows
  IMAP-LITERAL-08/09 and public-Session wire tests across read-buffer splits.

- `FETCH RFC822.SIZE` now returns only the message size and never sets `\Seen`.
  Previously it could fetch the body and mark the message read as a side effect.
- Bounded sequence-set and UID range expansion so an authenticated client can no
  longer force the server to materialize a huge range (e.g. `1:*` against a
  crafted set), which could pin CPU and memory. Ranges are now resolved against
  the mailbox's actual UID bounds.
- `SELECT` now resets per-session `\Deleted` state, so `\Deleted` marks from a
  previously selected folder can no longer leak into and expunge messages in a
  different folder.
- `EXAMINE` now enforces read-only selection: state-mutating commands (`STORE`,
  `EXPUNGE`, `UID EXPUNGE`, `MOVE`/`UID MOVE`, and `CLOSE`-driven expunge) are
  refused while a mailbox is selected read-only (RFC 3501 §6.3.2). `SELECT` after
  `EXAMINE` restores read-write access.
- `FETCH` now expands the `ALL`, `FAST` and `FULL` macros, implements `BODY` and
  `BODYSTRUCTURE` (single-part, non-text and multipart), and populates the `To`
  address in `ENVELOPE`. `BODYSTRUCTURE`/`BODY` structure fetches do not set
  `\Seen`.

## [0.2.2] - 2026-07-24

### Fixed

- An overlong initial command line ending in a LITERAL+ `{n+}` marker now
  sends BAD then BYE and ends the session, instead of reading the in-flight
  literal octets as new commands. Only the line's last octets are kept, so
  the check stays constant-memory. Other overlong lines, including ones with a
  synchronizing marker, keep the connection usable. Adds checklist rows
  IMAP-LITERAL-08/09 and public-Session wire tests across read-buffer splits.

- Refuse `AUTHENTICATE` before `STARTTLS` when a TLS configuration is present, so
  credentials are never accepted over an unprotected connection.

### Documentation

- Polished README and godoc for the public release.

## [0.2.1] - 2026-07-23

### Changed

- Renamed the module to `github.com/rest-mail/go-imap`.

## [0.2.0] - 2026-07-23

### Added

- `UIDPLUS` (RFC 4315), atomic server-side `MOVE` (RFC 6851), `UNSELECT`
  (RFC 3691) and `ENABLE` (RFC 5161), lit up through optional `Mailbox`
  interfaces (`UIDPlusMailbox`, `Mover`) so capabilities are advertised only when
  the backend implements them.

## [0.1.0] - 2026-07-23

### Added

- Initial IMAP4rev1 server engine (RFC 3501) built on a neutral `Backend` seam,
  standard library only, with no external dependencies.
