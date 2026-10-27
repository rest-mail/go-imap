# Core response source crosswalk and transcript evidence

This review indexes all 43 numbered sections within section 7 of RFC 3501 and RFC 9051, plus
all 44 response-code names defined there (11 rev1, 33 rev2). It records existing
narrow proofs, client duties and unsupported/backend boundaries. It does not
establish every grammar alternative or complete semantic coverage. The full
core command/prose inventory and complete RFC sample transcripts remain PARTIAL.
IMAP4rev2 session switching, advertisement and keyword mapping remain owner decisions in #718.

Independent test pins protect source hashes, section order/text hashes and code
names; omission/reference/document mutations must fail. These are provenance and
structural safeguards, not a substitute for reviewing the protocol prose.

| Source | Canonical text SHA256 |
|---|---|
| [RFC3501](https://www.rfc-editor.org/rfc/rfc3501.txt) | `4437974dde85859f2391f32215295e7b20ce157c3bd2adbe7e5b443e990f15cf` |
| [RFC9051](https://www.rfc-editor.org/rfc/rfc9051.txt) | `acf52a50eb748ea21b5ffdcb31c761d3fbba6242d53d4f53c2555de039c5c3cf` |

## Section dispositions

| Section | Checklist references | Scope and evidence |
|---|---|---|
| RFC3501 §7 | `IMAP-RESP-01`, `IMAP-RESP-AUDIT-02`, `IMAP-RESP-CLIENT-01` | Tag identity, untagged data and continuation framing have component proofs; unsolicited update/client memory duties remain separate. |
| RFC3501 §7.1 | `IMAP-RESP-02`, `IMAP-RESP-04`, `IMAP-RESP-AUDIT-03`, `IMAP-RESP-CLIENT-01` | Each named response code is dispositioned below. Optional availability and client presentation/unknown-code duties do not imply server implementation. |
| RFC3501 §7.1.1 | `IMAP-GREET-01`, `IMAP-RESP-AUDIT-02` | OK greeting begins unauthenticated; tagged OK marks success. Optional informational alerts are not emitted. |
| RFC3501 §7.1.2 | `IMAP-RESP-10`, `IMAP-RESP-AUDIT-02` | Tagged NO reports operational failure while selection remains usable. Optional untagged warnings are not emitted. |
| RFC3501 §7.1.3 | `IMAP-RESP-03`, `IMAP-RESP-AUDIT-01`, `IMAP-RESP-AUDIT-02` | Undetermined commands, including blank lines, receive untagged BAD. Determined protocol failures carry their own tag; the next command remains usable. |
| RFC3501 §7.1.4 | `IMAP-RESP-11`, `IMAP2-RESP-178` | PREAUTH is not offered. Its protected-transport recommendation and client refusal duty do not establish a server feature. |
| RFC3501 §7.1.5 | `IMAP-RESP-13`, `IMAP-RESP-14`, `IMAP-GREET-02` | LOGOUT sends BYE then tagged OK; panic, deadline and refusal paths close after untagged BYE. Deadline injection proves handling, not elapsed production timing. |
| RFC3501 §7.2 | `IMAP-RESP-01` | Server and mailbox status data are untagged; completion is separate. |
| RFC3501 §7.2.1 | `IMAP-CAP-01`, `IMAP-CAP-02`, `IMAP-RESP-CLIENT-01` | Rev1 capability baseline and protected-authentication advertisement have proofs; unknown-capability tolerance is client-side. |
| RFC3501 §7.2.2 | `IMAP-LIST-01`, `IMAP-LIST-02`, `IMAP2-RESP-187`, `IMAP2-RESP-188`, `IMAP2-RESP-189` | LIST syntax, attributes, separator consistency and reusable mailbox names have narrow component proofs; arbitrary hidden-child ACLs remain host-side. |
| RFC3501 §7.2.3 | `IMAP-LSUB-01`, `IMAP-LSUB-02` | LSUB has its own response name; separate subscription persistence remains the recorded backend boundary. |
| RFC3501 §7.2.4 | `IMAP-STATUS-01`, `IMAP-STATUS-02` | STATUS names the requested mailbox and attributes; detailed item/backend identity boundaries remain in the STATUS family. |
| RFC3501 §7.2.5 | `IMAP-SEARCH-01`, `IMAP-SEARCH-02` | SEARCH sequence/UID results have existing component proofs. The complete response example is not a new transcript claim. |
| RFC3501 §7.2.6 | `IMAP-SELECT-01`, `IMAP-STORE-06`, `IMAP-RESP-CLIENT-01` | FLAGS and PERMANENTFLAGS have distinct scopes; keyword persistence and client memory duties remain separate. |
| RFC3501 §7.3 | `IMAP-UPDATE-01`, `IMAP-RESP-15` | Numeric mailbox counts are untagged; RECENT remains an explicit approximation. |
| RFC3501 §7.3.1 | `IMAP-SELECT-01`, `IMAP-UPDATE-01`, `IMAP-RESP-CLIENT-01` | EXISTS reports total messages on selection and updates. Remembering the new count is a client duty. |
| RFC3501 §7.3.2 | `IMAP-RESP-15`, `IMAP-RESP-CLIENT-01` | True Recent ownership is not implemented; unseen-count approximation must not be presented as conformance. |
| RFC3501 §7.4 | `IMAP-FETCH-01`, `IMAP-EXPUNGE-01` | Message data use sequence-number prefixes; full FETCH structure semantics remain in the component families. |
| RFC3501 §7.4.1 | `IMAP-EXPUNGE-01`, `IMAP-RESP-12`, `IMAP-RESP-CLIENT-01` | Sequence renumbering and EXPUNGE restrictions are separate. UID commands MAY expunge; current deferral is a narrower valid choice. |
| RFC3501 §7.4.2 | `IMAP-FETCH-01`, `IMAP-ENV-01`, `IMAP-BS-01`, `IMAP-RESP-16` | Metadata, body structures, sections and partials have narrower proofs. Host wire representation and all grammar alternatives remain explicit limits. |
| RFC3501 §7.5 | `IMAP-RESP-17`, `IMAP-AUTHENTICATE-02` | Continuation requests precede synchronizing payloads and SASL challenges. Client wait-before-send duty is distinct from server framing. |
| RFC9051 §7 | `IMAP-RESP-01`, `IMAP-RESP-AUDIT-02`, `IMAP-RESP-CLIENT-01` | Tag identity, untagged data and continuation framing have component proofs; unsolicited update/client memory duties remain separate. |
| RFC9051 §7.1 | `IMAP-RESP-02`, `IMAP-RESP-04`, `IMAP-RESP-AUDIT-03`, `IMAP-RESP-CLIENT-01` | Each named response code is dispositioned below. Optional availability and client presentation/unknown-code duties do not imply server implementation. |
| RFC9051 §7.1.1 | `IMAP-GREET-01`, `IMAP-RESP-AUDIT-02` | OK greeting begins unauthenticated; tagged OK marks success. Optional informational alerts are not emitted. |
| RFC9051 §7.1.2 | `IMAP-RESP-10`, `IMAP-RESP-AUDIT-02` | Tagged NO reports operational failure while selection remains usable. Optional untagged warnings are not emitted. |
| RFC9051 §7.1.3 | `IMAP-RESP-03`, `IMAP-RESP-AUDIT-01`, `IMAP-RESP-AUDIT-02` | Undetermined commands, including blank lines, receive untagged BAD. Determined protocol failures carry their own tag; the next command remains usable. |
| RFC9051 §7.1.4 | `IMAP-RESP-11`, `IMAP2-RESP-178` | PREAUTH is not offered. Its protected-transport recommendation and client refusal duty do not establish a server feature. |
| RFC9051 §7.1.5 | `IMAP-RESP-13`, `IMAP-RESP-14`, `IMAP-GREET-02` | LOGOUT sends BYE then tagged OK; panic, deadline and refusal paths close after untagged BYE. Deadline injection proves handling, not elapsed production timing. |
| RFC9051 §7.2 | `IMAP-RESP-01` | Server status data remain untagged. |
| RFC9051 §7.2.1 | `IMAP-ENABLE-01`, `IMAP-ENABLE-02`, `IMAP-ENABLE-03` | ENABLED lists successfully enabled features, including empty results. This does not imply ENABLE IMAP4rev2. |
| RFC9051 §7.2.2 | `IMAP2-RESP-180`, `IMAP2-RESP-181`, `IMAP2-RESP-182`, `IMAP2-RESP-183`, `IMAP2-RESP-184` | Shared protected-auth behavior has proofs; rev2 capability and dual-mode selection remain blocked under #718. |
| RFC9051 §7.3 | `IMAP-RESP-01` | Mailbox status data remain untagged. |
| RFC9051 §7.3.1 | `IMAP-LIST-01`, `IMAP-LIST-02`, `IMAP2-RESP-187`, `IMAP2-RESP-188`, `IMAP2-RESP-189` | LIST syntax, attributes, separator consistency and reusable mailbox names have narrow component proofs; arbitrary hidden-child ACLs remain host-side. |
| RFC9051 §7.3.2 | `IMAP-NAMESPACE-01`, `IMAP-NAMESPACE-02` | Personal namespace and NIL unavailable namespace classes are offered; shared/other-user namespaces and arbitrary ACLs are not inferred. |
| RFC9051 §7.3.3 | `IMAP-STATUS-01`, `IMAP-STATUS-02` | STATUS names the requested mailbox and attributes; detailed item/backend identity boundaries remain in the STATUS family. |
| RFC9051 §7.3.4 | `IMAP-ESEARCH-01`, `IMAP2-RESP-191`, `IMAP2-RESP-192`, `IMAP2-RESP-193`, `IMAP2-RESP-194`, `IMAP2-RESP-195`, `IMAP2-RESP-196` | ESEARCH item uniqueness, empty-result omissions, matching sets and COUNT have component proofs. Rev2 default SEARCH switching is not implemented. |
| RFC9051 §7.3.5 | `IMAP-SELECT-01`, `IMAP-STORE-06`, `IMAP-RESP-CLIENT-01` | FLAGS and PERMANENTFLAGS have distinct scopes; keyword persistence and client memory duties remain separate. |
| RFC9051 §7.4 | `IMAP-UPDATE-01` | Numeric mailbox totals remain untagged; rev2 removes RECENT rather than proving the rev1 approximation. |
| RFC9051 §7.4.1 | `IMAP-SELECT-01`, `IMAP-UPDATE-01`, `IMAP-RESP-CLIENT-01` | EXISTS reports total messages on selection and updates. Remembering the new count is a client duty. |
| RFC9051 §7.5 | `IMAP-FETCH-01`, `IMAP-EXPUNGE-01` | Message data use sequence-number prefixes; full FETCH structure semantics remain in the component families. |
| RFC9051 §7.5.1 | `IMAP-EXPUNGE-01`, `IMAP2-RESP-199`, `IMAP-RESP-CLIENT-01` | Sequence renumbering and incomplete-command restrictions are separate. UID commands MAY expunge; current deferral is stricter, not a normative UID prohibition. |
| RFC9051 §7.5.2 | `IMAP2-RESP-201`, `IMAP2-RESP-202`, `IMAP2-RESP-203`, `IMAP2-RESP-204`, `IMAP2-RESP-205`, `IMAP2-RESP-207`, `IMAP2-RESP-208`, `IMAP2-RESP-209`, `IMAP2-RESP-210`, `IMAP2-RESP-211`, `IMAP2-RESP-212`, `IMAP2-RESP-213`, `IMAP2-RESP-215` | BINARY representation, sizes and refusal paths, partial origins and ordered structures are separate. Text canonicalization, host BODY encoding and unsolicited UID formatting remain recorded rev2 boundaries. |
| RFC9051 §7.6 | `IMAP-RESP-17`, `IMAP-AUTHENTICATE-02` | Continuation requests precede synchronizing payloads and SASL challenges. Client wait-before-send duty is distinct from server framing. |

## Named core response-code dispositions

Extension codes defined elsewhere remain with their own inventories. An optional
code listed here is not a claim that the server emits it. Rev2-specific codes
do not advertise rev2, and client-only duties are not server conformance proofs.

| Code | Checklist references | Scope and evidence |
|---|---|---|
| RFC3501 ALERT | `IMAP-RESP-05`, `IMAP-RESP-CLIENT-01`, `IMAP-SECURITY-08` | Not emitted; client display and protected/unprotected trust rules are separate obligations. |
| RFC3501 BADCHARSET | `IMAP-RESP-06` | Unsupported SEARCH charsets return this code with the supported list. |
| RFC3501 CAPABILITY | `IMAP-RESP-02` | Greeting capability code matches the explicit capability result; authentication refresh has its own command proofs. |
| RFC3501 PARSE | `IMAP-RESP-07` | Optional parse warning is not emitted; malformed-message handling is not proof of this code. |
| RFC3501 PERMANENTFLAGS | `IMAP-STORE-06`, `IMAP2-RESP-175` | Permanent flags are distinguished from session-local flags; arbitrary keyword creation and its limit notification are not offered. |
| RFC3501 READ-ONLY | `IMAP-RESP-08` | EXAMINE selection code is covered; dynamic backend ACL changes are not inferred. |
| RFC3501 READ-WRITE | `IMAP-RESP-08` | SELECT selection code is covered; dynamic backend ACL changes are not inferred. |
| RFC3501 TRYCREATE | `IMAP-RESP-09` | Missing destination is distinguished for APPEND/COPY; MOVE shares the narrow backend sentinel mapping. |
| RFC3501 UIDNEXT | `IMAP-SELECT-01`, `IMAP-SELECT-09` | Selection exposes UIDNEXT; persistent allocation and fallback identity remain host boundaries. |
| RFC3501 UIDVALIDITY | `IMAP-SELECT-01`, `IMAP-SELECT-09` | Selection exposes validity; stable store lifetime cannot be proved from the fallback. |
| RFC3501 UNSEEN | `IMAP-RESP-08` | Rev1 first-unseen sequence response is covered; rev2 removes this code. |
| RFC9051 ALERT | `IMAP-RESP-05`, `IMAP-RESP-CLIENT-01`, `IMAP-SECURITY-08` | Not emitted; client display and protected/unprotected trust rules are separate obligations. |
| RFC9051 ALREADYEXISTS | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 APPENDUID | `IMAP-APPENDUID-01`, `IMAP2-RESP-174` | Single append uses one destination UID, never a UID set; MULTIAPPEND is not implemented. |
| RFC9051 AUTHENTICATIONFAILED | `IMAP2-SEC-243` | Credential failure is generic on the wire; backend diagnostics and the supplied secret are not returned. |
| RFC9051 AUTHORIZATIONFAILED | `IMAP-RESP-AUDIT-03`, `IMAP-AUTHENTICATE-06` | Differing authorization identity is refused, but this distinct optional code is not emitted. |
| RFC9051 BADCHARSET | `IMAP-RESP-06` | Unsupported SEARCH charsets return this code with the supported list. |
| RFC9051 CANNOT | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 CAPABILITY | `IMAP-RESP-02` | Greeting capability code matches the explicit capability result; authentication refresh has its own command proofs. |
| RFC9051 CLIENTBUG | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 CLOSED | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 CONTACTADMIN | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 COPYUID | `IMAP-COPY-01`, `IMAP-COPY-15` | UID mapping and destination privacy are separate; backend unreadable-destination metadata remains a boundary. |
| RFC9051 CORRUPTION | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 EXPIRED | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 EXPUNGEISSUED | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 HASCHILDREN | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 INUSE | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 LIMIT | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 NONEXISTENT | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 NOPERM | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 OVERQUOTA | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 PARSE | `IMAP-RESP-07` | Optional parse warning is not emitted; malformed-message handling is not proof of this code. |
| RFC9051 PERMANENTFLAGS | `IMAP-STORE-06`, `IMAP2-RESP-175` | Permanent flags are distinguished from session-local flags; arbitrary keyword creation and its limit notification are not offered. |
| RFC9051 PRIVACYREQUIRED | `IMAP-AUTHENTICATE-09`, `IMAP-LOGIN-02` | Configured cleartext authentication is refused before credentials are checked; explicit development cleartext mode is outside this claim. |
| RFC9051 READ-ONLY | `IMAP-RESP-08` | EXAMINE selection code is covered; dynamic backend ACL changes are not inferred. |
| RFC9051 READ-WRITE | `IMAP-RESP-08` | SELECT selection code is covered; dynamic backend ACL changes are not inferred. |
| RFC9051 SERVERBUG | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 TRYCREATE | `IMAP-RESP-09` | Missing destination is distinguished for APPEND/COPY; MOVE shares the narrow backend sentinel mapping. |
| RFC9051 UIDNEXT | `IMAP-SELECT-01`, `IMAP-SELECT-09` | Selection exposes UIDNEXT; persistent allocation and fallback identity remain host boundaries. |
| RFC9051 UIDNOTSTICKY | `IMAP2-RESP-176` | Not emitted; nonpersistent UID metadata is not offered and stable identities remain a host obligation. |
| RFC9051 UIDVALIDITY | `IMAP-SELECT-01`, `IMAP-SELECT-09` | Selection exposes validity; stable store lifetime cannot be proved from the fallback. |
| RFC9051 UNAVAILABLE | `IMAP-RESP-AUDIT-03` | Not emitted as a distinct code. A generic NO does not prove this optional condition; host error taxonomy is not added by this audit. |
| RFC9051 UNKNOWN-CTE | `IMAP2-RESP-203`, `IMAP2-RESP-205` | BINARY and BINARY.SIZE refuse unknown decoding; this does not implement all rev2 text canonicalization. |

## Concrete correction and limits

Blank command lines formerly received no answer. They now receive untagged BAD
before the next pipelined command completes, preserving unauthenticated,
authenticated and selected state. Three baseline state cases distinguish silent
ignore from an explicit refusal without waiting for a timeout.

Additional transcripts prove matching BAD/NO/OK completions and later usability,
plus BYE followed by EOF on panic and transport deadline failure. The timeout
fixture injects the same transport error as deadline expiry; it does not wait 30
minutes or prove an operator's production timeout configuration. The refusal
and normal LOGOUT proofs remain separate. Backend panic diagnostics are not
included in wire text. No rev2 feature or host error taxonomy is added.

The old blanket EXPUNGE prohibition incorrectly included UID commands. Both
core RFCs permit EXPUNGE during UID FETCH/STORE/SEARCH; the engine's decision to
defer these too is a valid narrower behavior, not an extra RFC MUST NOT.
Existing proof rows are retained; unsupported Recent, textual BINARY
canonicalization, host wire representation, unsolicited UID data and persistent
identity/ACL metadata remain explicit boundaries. No new coverage measurement,
full suite, race or timed fuzz result is claimed.
