# Core security and operational source crosswalk

This review covers every numbered section within sections8 and10–12 of RFC3501
and RFC9051. The complete requirement inventory remains **PARTIAL**. This
crosswalk does not replace the pending core command/response, grammar-alternative
or non-keyword prose reviews, nor prove every sentence solely by naming a test.

The independent CI pins record the canonical whole-source hash, selected section
order and the exact section-text hashes (heading inclusive; ending at the next
numbered heading, so child sections are pinned separately). The machine index
records narrow checklist references and dispositions. CI rejects omissions,
duplicate sections, stale source/section pins, invented references and document
drift. These are structural/provenance safeguards; semantic completeness still
requires source review. Section8 examples are explicitly recorded gaps.

| Source | Canonical text SHA256 |
|---|---|
| [RFC3501](https://www.rfc-editor.org/rfc/rfc3501.txt) | `4437974dde85859f2391f32215295e7b20ce157c3bd2adbe7e5b443e990f15cf` |
| [RFC9051](https://www.rfc-editor.org/rfc/rfc9051.txt) | `acf52a50eb748ea21b5ffdcb31c761d3fbba6242d53d4f53c2555de039c5c3cf` |

## Section dispositions

| Section | Checklist references | Scope and evidence |
|---|---|---|
| RFC3501 §8 | `IMAP-SECURITY-12` | Full sample transcript is an explicit outstanding vector; component proofs do not establish the combined example. |
| RFC3501 §10 | — | Historical specification succession; no independent runtime requirement. |
| RFC3501 §11 | `IMAP-SECURITY-02`, `IMAP-STARTTLS-01`, `IMAP-PLAIN-06` | Unprotected transactions expose credentials and mail; protected configuration is proved, while the documented development mode is outside that claim. |
| RFC3501 §11.1 | `IMAP-SECURITY-04`, `IMAP-SECURITY-05`, `IMAP-STARTTLS-07`, `IMAP-TLS-AUDIT-04`, `IMAP-SECURITY-17` | Historical cipher deviation, every client identity rule, handshake result and host privacy/authentication policy are separate dispositions. |
| RFC3501 §11.2 | `IMAP2-SEC-243`, `IMAP2-SEC-244`, `IMAP2-SEC-245`, `IMAP2-SEC-246`, `IMAP-AUTHENTICATE-10` | LOGIN/PLAIN privacy alternatives, generic wire errors, optional non-plaintext mechanisms and host-supplied failure limiting; no default per-account policy is implied. |
| RFC3501 §12 | `IMAP-SECURITY-14` | Capability-registration process and STARTTLS/LOGINDISABLED registry updates, not a new wire requirement. |
| RFC9051 §8 | `IMAP-SECURITY-13` | SCRAM, rev2 enablement and OLDNAME make this full transcript unsupported; rev1 components are not a substitute. |
| RFC9051 §10 | — | Historical specification succession; no independent runtime requirement. |
| RFC9051 §11 | `IMAP-SECURITY-02`, `IMAP-PLAIN-06` | Transport/confidentiality warning covers both implicit TLS and STARTTLS; SASL confidentiality layers remain unsupported. |
| RFC9051 §11.1 | `IMAP2-SEC-233`, `IMAP2-SEC-234`, `IMAP2-SEC-236`, `IMAP-SECURITY-02`, `IMAP-SECURITY-03`, `IMAP-SECURITY-05` | TLS recommendations and exact cipher/profile matrix remain host obligations; concrete TLS1.2/TLS1.3 fixtures do not prove the complete profile. Client identity checking stays client-side. |
| RFC9051 §11.2 | `IMAP-SECURITY-01`, `IMAP-SECURITY-02`, `IMAP-SECURITY-06`, `IMAP-SECURITY-07`, `IMAP2-SEC-238`, `IMAP-SECURITY-15` | Missing implicit-TLS configuration now fails before either bind. Server cleartext availability, client dual-mode support and concurrent port/address recommendations have distinct scopes and levels. |
| RFC9051 §11.3 | `IMAP-SECURITY-08`, `IMAP-SECURITY-16` | All three response-filtering rules and PREAUTH cross-reference are client injection defenses, not server parser proofs. |
| RFC9051 §11.4 | `IMAP2-SEC-241`, `IMAP-APPENDUID-05`, `IMAP-COPY-15` | Destination metadata privacy requires backend readability state; the engine does not infer that state from write access. |
| RFC9051 §11.5 | `IMAP2-SEC-242`, `IMAP-NAMESPACE-01` | No other-users namespace is offered; host mailbox authorization remains necessary. NIL namespace output does not prove arbitrary backend ACLs. |
| RFC9051 §11.6 | `IMAP-BS-17`, `IMAP-BS-19` | Content-MD5 is preserved metadata, never a generated/verified cryptographic digest. Absent/multipart forms and string framing have narrow existing wire proofs. |
| RFC9051 §11.7 | `IMAP2-SEC-243`, `IMAP2-SEC-244`, `IMAP2-SEC-245`, `IMAP2-SEC-246`, `IMAP2-SEC-247`, `IMAP-SECURITY-09`, `IMAP-SECURITY-10`, `IMAP-SECURITY-11` | Reporting, per-IP limiting, cross-account analysis, mandatory weak-account blocking/change request and recommended password strength are distinct. Generic errors and protected configuration have wire proofs; host policy remains unimplemented here. |
| RFC9051 §12 | `IMAP-SECURITY-14` | TCP143/993 service references and reserved UDP port registry actions; deployment does not claim registry administration. |
| RFC9051 §12.1 | `IMAP-SECURITY-14`, `IMAP2-SEC-224` | Capability registration process and AUTH=/STARTTLS/LOGINDISABLED reference updates. |
| RFC9051 §12.2 | `IMAP-SECURITY-14`, `IMAP-AUTHENTICATE-12` | The imap SASL service-name registry; PLAIN does not consume a GSSAPI service identity. |
| RFC9051 §12.3 | `IMAP-SECURITY-14` | RFC5258 registration procedures and OLDNAME registry reference; OLDNAME implementation is not inferred. |
| RFC9051 §12.4 | `IMAP-SECURITY-14` | Mailbox-name attribute and response-code registry references, not new runtime behavior. |

## Concrete correction and limits

Requesting an implicit-TLS port with no TLS configuration formerly opened a
plaintext socket but marked its sessions protected. Startup now refuses that
configuration before opening either requested listener. Explicit development
cleartext listeners retain their documented behavior.

Three focused public-listener tests cover missing configuration, verified
TLS1.2/TLS1.3 authentication and rejected hostname/trust/version negotiations.
A fourth wire test checks structured failure reporting for LOGIN and both PLAIN
forms without leaking passwords, encoded responses or backend diagnostics.
Existing privacy, limiter, STARTTLS, namespace and Content-MD5 proofs stay intact.
These fixtures do not prove the entire rev2 cipher matrix, production listener
configuration, arbitrary backend ACLs, account policy or password-spraying
detection. The full RFC sample transcripts remain outstanding. IMAP4rev2 is
still not advertised; no weak legacy suite, deployment or owner policy is added.
