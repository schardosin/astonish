# Security remediation analysis — revised after reviewer feedback

## 1. Purpose and status

This is a security handoff for SAP/astonish, incorporating the supplied Oct 15–19 reviews, subsequent read-only source investigation, and the security reviewer's correction identifying omitted Oct 14 agent-tool findings and unresolved work from PRs #593 and #594.

**Local urgent-fix implementation is in progress under the separate approved execution plan.** The baseline inventory and proposed acceptance below remain open unless superseded by recorded execution evidence. No production deployment, migration, key rotation or live acceptance is claimed. Ship independently reviewable urgent fixes without waiting for sandbox or SQL redesigns; the current working-tree edits are not yet release evidence. See `SECURITY_REMEDIATION_PROGRESS.md` for the latest bounded implementation/test record.

Latest evidence supplement: the consolidated memory/session/masked-secret batch in that progress record preserves the earlier 380-entry/408-parser-test MCP baseline and expands strict coverage to 594 entries/633 normal parser tests. MEM-01 planning now stops on transient destination lookup failure before mutation; scoped cleanup remains nontransactional. MCP GET nil-row panics are corrected with failing-before/passing-after handler regressions. CFG-03/04 and MCP-01/02 gain registered-route temporary-SQLite/JWT masked-update coverage, not PostgreSQL or successful sandbox discovery proof. These remain **FixedPendingVerification** at the program boundary. Four team missing-target MCP PUT cases still produce bounded storage errors rather than validation errors; their no-effects checks are not authorization-denial success. PR593-03 older cross-org denial assertions are tightened but await infrastructure execution. No existing ID, policy gate, migration duty or incident-response obligation is closed by these supplemental local results.

### Evidence and disposition states

Use these exact states in the handoff; a state is not a severity:

- **Reported:** reviewer-supplied finding, not independently reproduced here.
- **Corroborated:** source or recorded observation supports the report; this alone does not establish live closure.
- **Contained:** a restriction demonstrably blocks exposure, but the underlying fix or full verification remains outstanding. No containment is claimed here.
- **FixedPendingVerification:** a code correction has been observed, but the required regression/deployment evidence is incomplete.
- **Verified:** the finding-specific regression and applicable live deployment acceptance passed at recorded revisions. No finding is assigned this state here.
- **PolicyDependent:** the final authorization verdict or acceptance semantics depend on an explicit team-owned decision; technical observations remain recorded.
- **NotReproduced:** a documented reproduction attempt did not reproduce the issue in its stated environment; absence of a test is not this state.

“Source corroborated” in the retained inventory means **Corroborated** from the earlier local investigation. **Additional candidate** and **Open question** distinguish unconfirmed investigations from the reviewer's findings; neither implies verification or dismissal.

The reviewer confirmed that the original document matched their Oct 15–19 issues, severities, and references, but rejected its claim to be a complete inventory. The omitted items are tracked below. The follow-up specifies **High for TOOL-01**; the other six Oct 14 tool severities remain **unspecified**. A reviewer recheck of this corrected handoff has been requested by the main thread and is **pending**, not an approval.

## 2. Revision provenance

Per the reviewer, reviewed main `6bd4861` is 18 commits after `af8140f`; those commits affect chat streaming, LLM providers and session compaction, not the finding locations, so the supplied `af8140f` links remain applicable. This comparison is attributed to the reviewer, not an independent comparison performed here. The later `pkg/api/knowledge_debug_handler.go` change in PR #596 remains unreviewed follow-up work, not a presumed vulnerability.

Implementation began at local HEAD `6bd486153cdb8386c59f74105f21e203a7d3a350`, reconfirmed during execution. Local remediation edits are uncommitted; no fix PR or deployment is claimed. The user's unrelated slides files remain untouched. No checkout or reset was performed.

## 3. Executive assessment

The central design conclusions still hold:

- Authentication alone does not authorize a requested scope, resource, or operation.
- Request-scoped handlers must not fall through to process-global stores or authority in multi-user deployments.
- Effective merged configuration should be read-only; writes require an explicit authorized scope.
- Management responses must mask copies of secret-bearing data, not modify runtime configuration or stored secrets.
- PostgreSQL `search_path`, chroot, and collision-prone runtime names are not security isolation boundaries.
- Untrusted shells and build recipes must not share storage publication authority or registry push credentials.

Two conclusions are strengthened by the feedback:

1. **Runtime credential exposure is distinct from template credential capture.** A shell-readable team credential vault is a direct authorization bypass even if snapshots exclude it perfectly.
2. **Artifact file-read exposure requires incident response, not just a patch.** A reachable vulnerable instance must assume its JWT signing secret and master key have been exposed and follow a rotation/re-encryption procedure.

## 4. Consolidated inventory

Identifiers are handoff tracking labels, not CVEs. Existing IDs are retained. Additional items have new IDs to avoid hiding separate security boundaries inside one finding.

### 4.1 Oct 14 agent-tool findings — previously omitted

Reported against `cacdeee`. The reviewer specifies **High** for TOOL-01; exact severities for TOOL-02 through TOOL-07 and remaining pinned source references must be recovered from the original Oct 14 review. None of these additions was independently reproduced in this follow-up.

| ID | Severity | Reported finding | Required remediation/verification direction |
|---|---|---|---|
| TOOL-01 | High | `/tmp/astonish/session-credentials.json` is readable with shell and `read_file`, allowing a non-admin member to retrieve team secrets directly. | Stop materializing broad team-secret plaintext into a user-controlled execution environment. Apply credential-use authorization at a broker or equivalent inaccessible boundary. Prove shell, file tools, and arbitrary code cannot read a team-secret canary while approved credential-backed operations still work. Separate from SB-10 capture leakage. |
| TOOL-02 | Unspecified | `browser_file_upload` can upload the master key. | Resolve upload inputs through the authorized execution/artifact filesystem, not unrestricted server paths; enforce confinement before browser/CDP upload. Test an allowed file and denied server secret/symlink inputs. |
| TOOL-03 | Unspecified | `http_request` has SSRF gaps permitting agent-triggered server access to internal addresses. | Trace the actual tool transport and enforce destination policy through resolution, dialing, redirects, and proxies; cover loopback, private, link-local, metadata, IPv6 and DNS rebinding. Keep explicit trusted local-mode exceptions separate. |
| TOOL-04 | Unspecified | `web_fetch` has SSRF gaps permitting agent-triggered server access to internal addresses. | Apply an equivalent boundary to the fetch tool and its redirect/extraction paths; do not assume the HTTP tool's fix automatically covers it. |
| TOOL-05 | Unspecified | `browser_pdf` writes files on the host. | Route PDF output to the authorized session/artifact destination and prevent arbitrary host path writes. Verify download/render consumers and local-mode compatibility. |
| TOOL-06 | Unspecified | `SetScopeRoot` is never called, so intended folder scoping is inactive. | Trace scope initialization and every consumer; wire a fail-closed execution root. A file-tool root alone does not constrain arbitrary shell code, so do not claim this also fixes TOOL-01 without an execution boundary. |
| TOOL-07 | Unspecified | Credential headers survive request redirects. | Strip secret-bearing headers or reject redirects across unauthorized origins, including custom credential headers. Reauthorize each hop and test host, port and scheme changes. Identify all affected clients instead of assuming one transport. |

### 4.2 Residual findings from PR #593 — previously omitted

Reported by the reviewer; PR changes and current call paths have not been independently inspected in this follow-up.

| ID | Severity | Residual issue | Required direction |
|---|---|---|---|
| PR593-01 | Unspecified | Team session store reads by session ID without a user filter. | Define owner/authorized shared-session lookup semantics and enforce them consistently at store/service boundaries and callers. Do not accidentally remove legitimate fleet/shared access; authorize it explicitly. |
| PR593-02 | Unspecified | `MemoryListBySessionHandler` has no session owner check; the security verdict is conditional on intended team-memory visibility. | **PolicyDependent:** if session-linked memory is private, require session-owner/shared-session authorization; if deliberately team-visible, authorize team membership and document that intent, including cross-team denial. Do not silently redefine shared memory as private or let memory visibility grant access to private sessions/artifacts. Team decision D-01 is pending. |
| PR593-03 | Test gap | No end-to-end test proves one user cannot reach another user's session. | Add real authenticated HTTP tests for two users in the same team and users across teams/orgs. Exercise session read/restore, session memory, artifacts, and relevant execution/proxy paths; verify denied requests have no side effects. |

### 4.3 Residual findings from PR #594 — previously omitted

Reviewer corrections distinguish a code/regression item, a deployment obligation, and low-urgency defense in depth. Original severity scores and exact PR revisions remain unrecorded; urgency is not a newly assigned severity.

| ID | Severity | Residual issue | Required direction |
|---|---|---|---|
| PR594-01 | Unspecified | Historical `POST /api/user/channels/{id}/verify` could mark an existing unverified row verified. The reviewer reports the current route returns **410 Gone**. | **FixedPendingVerification:** retain the observed code correction and require a regression proving 410 with no row/state mutation, plus the supported proof-based channel flow. The reported current 410 is not live deployment closure; attach the exact code revision, regression result and deployed-route evidence before Verified. |
| PR594-02 | Unspecified | Deployment mail trust requires stripping sender-spoofed `Authentication-Results` claiming the trusted authserv identity. The trusted authserv ID is **already pinned** in code per the reviewer. | **Reported deployment prerequisite**, not a missing-ID-pinning code defect: prove the receiving mail infrastructure strips spoofed trusted-ID headers before adding its own results. Test injected and duplicate headers through that ingress. Do not reopen pinning as if absent or accept sender-supplied trusted-ID results. |
| PR594-03 | Unspecified | Rate limits are per process; the reviewer describes this as **low urgency** given **128-bit** verification tokens. | Track distributed rate limiting as defense in depth, not an independently urgent release gate or a demonstrated practical token-guessing bypass. If adopted, define aggregate limits and test requests across replicas and concurrent attempts; no shared limiter or deployment test is claimed here. |

### 4.4 Sandbox, templates, browser, image builds and network pre-seed

The following severities retain the supplied reviews. “Source corroborated” refers to the earlier local investigation, not pinned-commit or live verification.

| ID | Severity | Finding | Evidence/remediation direction |
|---|---|---|---|
| SB-01 | Critical | Kubernetes editors mount all platform layers writable; read-only sessions can still read unrelated layers. | Source corroborated in local `pkg/sandbox/k8s/session.go:303–316`. Authorized immutable chain only; separate execution, capture and publication; real runtime isolation. |
| SB-02 | High | Docker template tar extraction follows archive-created symlinks during later host writes. | Source corroborated in local `pkg/sandbox/docker/capture.go:106–170`. Root-confined safe extraction, bounded resources and link handling across every capture/import path. |
| SB-03 | High | `AliasLayer` permits traversal/destructive overwrite without ownership enforcement. | Source corroborated in local `pkg/sandbox/docker/template.go:351–368`. Central identifier validation, scoped ownership, immutable digest directories, atomic aliases. |
| SB-04 | High | Docker sessions run privileged with the complete layers directory mounted. | Source corroborated in local `pkg/sandbox/docker/session.go:435–441`. Replace unsafe multi-user execution architecture; deleting a required flag alone is not a working fix. |
| SB-05 | High | Truncated OpenShell names collide; existing sandboxes are adopted on a clash. | Source corroborated in local `pkg/sandbox/openshell/session.go:147–181,496–510`. Tenant-qualified collision-resistant names plus verified object ownership and safe cleanup. |
| SB-06 | High | Bootstrap template name lookup permits cross-org overwrite. | Source corroborated in local `pkg/sandbox/bootstrap_files.go:174–222`. Exact authorized template IDs and request-bound stores, not global name scans. |
| SB-07 | High | Team editor sandbox identity uses only team slug. | Source corroborated in local `pkg/api/sandbox_backend.go:320` and `pkg/api/team_template_handlers.go:167–176`. Carry org/team identity through editors, persistence and builds. |
| SB-08 | High | Unauthenticated CDP/VNC listeners permit lateral browser takeover; required network provisioning is absent from production paths. | Source corroborated in backend browser/network wiring. Loopback management ports, authorized tunnels, deny-by-default network policy, and no generic port-exposure bypass. |
| SB-09 | High | Untrusted Kaniko Dockerfiles can access platform registry push credentials. | Source corroborated in local `pkg/sandbox/imagebuilder/job.go:104–147`. Separate untrusted no-push execution from trusted publisher authority. |
| SB-10 | Not separately specified | Template captures include decrypted runtime credentials. | Source corroborated: canonical vault and capture exclusion gap. Keep secrets outside snapshot storage and inspect/quarantine historical templates. This does not resolve direct runtime vault access in TOOL-01. |
| NP-01 | Medium | Pre-seeding network allows ignores higher-tier denies. | Source corroborated in local `pkg/sandbox/netpolicy/policy.go:58–75`. One deny-aware policy for startup, refresh and later grants; safe handling of wildcard overlap. |

### 4.5 SQL, settings, MCP secrets and fleets

| ID | Severity | Finding | Evidence/remediation direction |
|---|---|---|---|
| SQL-01 | Critical | App SQL lacks app authorization; search path permits cross-schema access; SQLite exec permits filesystem-capable SQL. | Relevant current source inspected, full investigation unfinished. Authorize the resolved app/operation and use genuinely restricted execution connections, separate from provisioning authority. |
| CFG-01 | Critical | `GET /api/settings/config` returns provider fields unmasked. | Source corroborated in local `pkg/api/settings_handlers.go:157–207`. Mask response copies; ensure masked ordinary and bulk updates preserve the correct stored secret. |
| MCP-01 | High | MCP list/get/settings reveal literal environment secrets to members. | Source corroborated. Separate redacted management DTOs from runtime values and implement safe scoped update merging. |
| FL-01 | High | Global fleet activator/default-org general-team authority leaks across lifecycle operations. | Source corroborated in daemon/API wiring. Tenant-bound plan, scheduler, monitor, run-state, session and credential bundle for every start/retry/recovery. |
| FL-02 | Medium | Fleet mutations lack consistent role/owner checks and can alter autonomous execution identity. | Source corroborated; current creator behavior varies by endpoint. One mutation policy and authoritative creator/execution identity. |
| CFG-02 | Low | Platform/org provider GET handlers omit promised admin checks. | Source corroborated. Enforce documented scope roles in addition to masking. |

### 4.6 Apps, artifacts and slides

| ID | Severity | Finding | Evidence/remediation direction |
|---|---|---|---|
| ART-01 | Critical | Artifact endpoints accept arbitrary host paths and make session ownership optional. | Reviewer reports `/etc/passwd` reproduction and `/proc/self/environ` exposure. Local source partially inspected; not reproduced here. Require authorized session plus root-confined artifact access; eliminate unrestricted platform host fallback. |
| ART-02 | Not separately scored | Download serves HTML/JS inline on the Studio origin. | Reported. Attachments, `nosniff`, sandbox CSP and isolated safe preview handling; check all consumers. |
| APP-01 | High | App HTTP proxy sends selected personal/team credentials to arbitrary caller URLs and echoes upstream errors. | Source corroborated. Destination-bound credential-use authority, redirect policy, safe errors; network permission is not secret-use permission. |
| APP-02 | Medium | Members/viewers can overwrite/delete shared team apps. | Relevant source inspected. Apply shared-definition mutation authorization, including related publishing/model edits. |
| APP-03 | Medium | App fork/promote reads another team's app using an unchecked team slug. | Relevant source inspected. Authorize source visibility and destination mutation independently. |
| APP-04 | Medium | App AI lacks rate limits and request-size routing uses wrong paths. | Reported; AI source partly corroborates. Tenant/user budget and concurrency controls, actual-route body caps. |
| APP-05 | Medium | Unauthenticated app requests share one anonymous sandbox/allowlist. | Reported; platform already requires login. Separate explicit trusted local behavior from network-facing anonymous execution. |
| SL-01 | Low | Team slide deletion lacks admin authorization. | Reported; detailed investigation incomplete. Check personal/team delete policy and authorized success. |
| SL-02 | Low | PPTX ZIP import trusts declared decompressed sizes. | Reported; existing total cap/time limit reduce impact. Bound actual decompression, entries, memory and cancellation. |
| APP-06 | Low | App AI uses the global rather than team-effective provider. | Local `pkg/api/app_ai_handler.go:57–67` corroborates. Resolve provider/model and billing/limits from authorized request context. |

### 4.7 Setup, deployment config and memory

| ID | Severity | Finding | Evidence/remediation direction |
|---|---|---|---|
| INIT-01 | Critical | Unauthenticated PostgreSQL init can replace an existing SQLite configuration and probe arbitrary destinations. | Source corroborated in local `pkg/api/platform_setup_handlers.go:50–64,120–140,179–187`. Immediate configured-backend guard; serialized one-time bootstrap authority and sanitized network errors. |
| CFG-03 | High | Org admins can change deployment-wide config; loopback bypasses authorization. | Source corroborated in local `pkg/api/fullconfig_handlers.go:412–424`. Platform-admin-only global writes; explicit local mode, not loopback-based privilege. |
| CFG-04 | Medium | Full config leaks browser proxy/CDP/VNC secrets. | Local handler returns `cfg.Browser` unchanged. Redact secret-bearing fields/URLs and preserve same-resource values safely on updates. |
| MEM-01 | Medium | Consolidation can delete shared sources/duplicates without owner/admin checks, including discarded cards. | Source corroborated. Preauthorize every affected source, duplicate and destination before mutation, using the direct-delete policy. |

### 4.8 MCP installation, taps, inspector and network approvals

| ID | Severity | Finding | Evidence/remediation direction |
|---|---|---|---|
| MCP-02 | High, described as critical in practice | Internet MCP install lets members write org/platform executable config. | Source corroborated in local `pkg/api/tool_discovery.go:928–996`. Scope-correct writable resolver before save or discovery. |
| MCP-03 | High | Team-admin-gated MCP install/PATCH/settings can write org/platform scopes. | Source corroborated. Apply the authoritative role-gated resolver to every write; reject effective/unknown scopes and missing stores. |
| TAP-01 | High | Global taps mutable by team admins; official alias spoofing/cache overwrite; tokens sent to arbitrary hosts. | Source corroborated. Platform authority for global changes, reserved aliases, canonical origin cache keys, exact token host/redirect restrictions. |
| MCP-04 | Medium | Inspector/discovery caches permit cross-tenant tool-definition poisoning. | Source corroborated. Tenant/scope/config identity in cache keys or proven config-file-only global writes. |
| NP-02 | Medium | Session approvals can grant broad persistent team access and act on body-supplied sandbox names. | Source corroborated. Server-derived owned sandbox identity, privileged persistent writes and deny-preserving grant validation. |

### 4.9 Open questions retained

- Remote MCP SSE/streamable HTTP SSRF, including inspector/discovery and chat startup transport differences. This is separate from reported HTTP/fetch tool SSRF.
- Full live fleet retry credential exploitation: mixed authority was observed, but the exact exploitation sequence remains untested.
- Remaining unspecified Oct 14 severities and pinned references; exact correction/regression/deployment evidence for PR residuals, while preserving the already supplied 410, pinned authserv-ID and 128-bit-token corrections.

## 5. Additional current-source candidates — independent review requested

The security reviewer considers these plausible but has **not verified them**. Keep them tracked without presenting them as confirmed additions to their daily reviews.

| ID | Candidate | Earlier local inspection reference |
|---|---|---|
| CAND-01 | Builder pods mount complete layers and uppers writable; fleet pods mount complete uppers. Fixing ordinary sessions alone may leave equivalent exposure. | `pkg/sandbox/k8s/template.go:451–471`; `pkg/sandbox/k8s/fleet.go:224–226`. |
| CAND-02 | Separate base replacement pipes untrusted archive output to host `tar`, outside the primary extractor fix. | `pkg/sandbox/docker/template.go:373–412`. |
| CAND-03 | Standard MCP server install/uninstall can affect global platform secrets from narrower-scope operations. | `pkg/api/standard_servers_handler.go`, especially install/uninstall paths. |
| CAND-04 | Fleet creator identity differs between JSON save, YAML save, duplicate, agent PATCH, and persisted creator column. | Fleet API handlers and `pkg/store/entstore/team_fleet.go`. |
| CAND-05 | Docker/Kubernetes adoption and failure-cleanup paths may act on an existing object without sufficient ownership proof. | Backend session creation/adoption paths. |
| CAND-06 | Capture hashes and extracts mutable data in separate traversals and trusts emitted metadata. | Shared overlay capture and Kubernetes template capture paths. |

Please include these in a future pinned-commit review. They remain part of the remediation impact investigation but are not substitutes for confirming the original reported defects.

## 6. Delivery correction: urgent fixes merge separately

**Supersedes the original single-branch delivery recommendation.** Keep one tracking inventory, not one release gate. Use small PRs against the agreed current base for urgent, independently verifiable fixes. Longer architectural work can use dedicated branches and later PRs without delaying containment.

### Immediate PR candidates

| Priority | Change | Minimum regression coverage |
|---|---|---|
| Immediate | INIT-01: refuse PostgreSQL init when either `IsPostgres()` or `IsSQLite()` is true. | Both configured backends reject before outbound dial or config/JWT mutation; first-run setup still follows authorized bootstrap rules. |
| Immediate | CFG-01: mask `/api/settings/config`. | Provider canaries absent; configured state correct; masked ordinary and replacement updates do not erase or persist placeholders. |
| Immediate | CFG-03: require `IsPlatformAdmin` for global full-config writes in multi-user mode, removing loopback bypass. | Member/org-admin denial, platform-admin success, loopback not privileged, explicit local-mode compatibility. |
| Immediate | MCP-02/MCP-03: route every affected write through `resolveMCPStoreForWrite`. | Role × scope matrix; effective and unknown scope rejection; denied calls have no save/discovery/start side effects. |
| Immediate | ART-01/ART-02: require and check session, and confine artifact paths/remove unrestricted host fallback. | Missing/wrong session, arbitrary absolute path, owned session plus host secret path, symlink escape, content/download headers and safe preview behavior. |
| Immediate | MEM-01: use `canManageMemory` or equivalent shared policy for every affected record before mutation. | Unauthorized source/duplicate/discard/merge denied without partial writes or deletions; authorized consolidation succeeds. |

Although several entry-point fixes are small, do not equate a one-line guard with complete closure. In particular, **session ownership alone does not make an arbitrary host artifact path safe**, and adding a delete check after earlier writes can leave partial consolidation effects.

Other narrow permission/redaction fixes should follow as soon as their compatibility tests are ready. PR #593/#594 residuals and agent-tool boundaries must be triaged alongside these, not deferred because they were reported later in this conversation.

### Longer tracks

- True SQL execution privilege isolation and existing-schema migration.
- Supported multi-user sandbox runtime, scoped storage, safe capture and trusted publication.
- Tenant-bound fleet lifecycle/recovery and authoritative execution identity.
- Credential broker/use-policy redesign and elimination of broad plaintext vault delivery.
- Email ingress deployment evidence for the already-pinned trusted authserv ID; shared rate limiting is a separate low-urgency defense-in-depth track for 128-bit tokens, not a blocker for independent urgent fixes.

Containment for an unremediated high-risk feature may require disabling or restricting that feature. Document such restrictions explicitly and obtain operator approval; do not silently preserve unsafe behavior to maintain compatibility.

### Team-owned decisions and blocking scope

These are requested owner roles, **not confirmed assignments or approved policy**. The team must name an accountable person and record the chosen semantics and decision revision. Reviewer input informs security acceptance but does not substitute for product/operator decisions. Pending decisions block only their stated scope, never all urgent releases.

| Decision | Requested owner | Semantics to decide | Status | Blocking scope |
|---|---|---|---|---|
| D-01 Memory visibility | Team product owner and memory/session maintainer; named owner pending | Are session-linked memories private to the session owner or deliberately shared with the team? Define member visibility, cross-team denial and explicit shared-session access independently of session/artifact ownership. | PolicyDependent; team intent not yet recorded | PR593-02 verdict and its acceptance cases. Does not block private-session checks, ART-01 or MEM-01 mutation authorization. |
| D-02 Shared-app data | Team app product owner and SQL/store maintainer; named owner pending | Separate shared-definition mutation from read/write/DDL permissions for ordinary app users; define personal/team app data identity and allowed operations. | PolicyDependent; no chosen permission model | SQL-01 authorized success cases, app-data grants/migration and affected APP-02 semantics; not unrelated permission/redaction fixes or SQL escape containment. |
| D-03 Fleet identity and active edits | Team fleet product owner and fleet/security maintainers; named owner pending | Choose authoritative creator/execution principal across create, JSON/YAML save, PATCH, duplicate, retry and recovery; decide who can edit active autonomous work, when reapproval is required and how persisted plans migrate. Identity-preserving edits and inactive duplicates are proposals, not accepted policy. | PolicyDependent; execution/active-edit policy pending | FL-02/CAND-04 final semantics and affected lifecycle migration; does not block FL-01 tenant-bound authority containment or other urgent releases. |
| D-04 Credential-use consent | Team credential/app product owner and security maintainer; named owner pending | Define which principal may authorize which app/tool to use personal/team credentials for which destination, consent lifetime/revocation and active-work reapproval. A personal-only restriction is containment, not a complete consent model. | PolicyDependent; consent/use policy pending | TOOL-01/APP-01 broker and allowed-use acceptance, affected fleet credential flows; does not block vault isolation, secret redaction, header stripping or incident rotation. |
| D-05 Runtime profile | Team platform/runtime owner and deployment operator with security review; named owner pending | Select supported hostile multi-user runtime/storage/browser/build profile versus explicitly trusted local mode; choose feasible isolation/capture boundary and required backend/storage test matrix. | PolicyDependent; feasibility and operator approval pending | Sandbox architectural replacement and its compatibility/migration acceptance; does not block narrow capture/path fixes, network restrictions or other urgent PRs. |

## 7. Mandatory incident response for artifact exposure

**Treat a reachable ART-01-vulnerable instance as having exposed its JWT signing secret and master encryption key.** Do not make rotation conditional on finding a successful request in access logs. The reviewer identifies `/proc/self/environ` as exposing `ASTONISH_JWT_SECRET`, master key material and the database connection string.

Required operator response:

1. Contain access to the vulnerable routes/instance and deploy the access-control plus path-confinement fix. Avoid generating replacement secrets while the read path remains exposed.
2. Preserve relevant audit evidence securely; establish the vulnerable exposure window. Do not print environment contents or real keys into issue reports/tests.
3. Rotate JWT signing material and invalidate old authentication/session material as supported; ensure every API/worker replica stops accepting the old signing key. Audit privilege and account changes because token forgery permits actions beyond file reads.
4. Rotate the master encryption key through a tested decrypt/re-encrypt migration with backup and recovery safeguards. **Do not overwrite the key blindly**, which can make existing ciphertext unrecoverable.
5. Re-encryption does not make previously disclosed plaintext or copied ciphertext safe. Rotate underlying provider, team/personal, registry, GitHub and other credentials that the exposed key and accessible stores could reveal. Rotate the exposed database connection credential and review database access.
6. Review installed MCP commands, fleet plans, users/roles, configuration, templates and base layers for persistence or tampering. Rebuild or quarantine artifacts with untrusted provenance.
7. Verify that old JWTs and rotated downstream credentials fail, new credentials work, all replicas use the new configuration, and secret canaries cannot be read through any remaining tool/API path.

Rotation is required operational work, **not something performed by this document**. Exact key-storage, re-encryption, revocation and recovery procedures must be verified against the deployment before execution. The newly reported master-key upload and runtime-vault access paths must be contained too, or replacement secrets could be exposed again.

## 8. Architectural recommendations retained, with caveats

### Sandbox isolation

Require a supported user-namespace/rootless or stronger boundary for hostile multi-user execution. The current privileged overlay/chroot implementation cannot be secured merely by setting `runAsNonRoot` or deleting `--privileged`; doing so may break overlay setup without providing a usable replacement.

Evaluate root-in-private-user-namespace runtime/snapshotter support, OCI-root sessions with external capture/publication, or VM-backed workers according to the deployment threat model. No candidate runtime has been proven in this investigation. Rootless by itself is not a universal guarantee.

Expose only the authorized immutable session chain and private writable state. Per-org volumes alone do not establish same-org cross-team confidentiality. Keep user execution, capture and publisher authority separate. Bind browser management to loopback through authenticated session tunnels and validate all generic proxy/exposure routes.

### Safe capture and secrets

Use rooted extraction into fresh private staging. Reject unsafe entry paths and conflicting entries, handle symlinks as inert data without following them during extraction, restrict hardlinks to validated internal regular files, and bound actual bytes/entries/time. Cover base replacement and every import/capture path, not just one helper.

Keep injected credentials out of capture structurally, but recognize that a user process can copy any readable secret elsewhere. Thus SB-10 requires a publication trust policy, and TOOL-01 requires removing or narrowing readable secret delivery. Filesystem permissions cannot protect a vault from a shell running as the same privileged identity.

### SQL execution

Use a constrained execution login/connection boundary that cannot regain provisioning authority. An admin connection followed by `SET ROLE` is not sufficient without addressing role reset/session authorization, public grants, extensions, security-definer functions and resource limits.

Decide personal/shared app identity and permitted data/DDL operations explicitly. Definition ownership does not automatically imply that ordinary shared-app users should be forbidden all data writes. Provision schemas/roles through separate trusted authority and migrate existing ownership/grants safely.

For SQLite, enforce restrictions on `ATTACH`, `VACUUM INTO`, unsafe PRAGMAs, extension loading and equivalent escape operations using supported engine/driver controls or a deliberately constrained interface. Regex/prefix filtering alone is not a general SQL sandbox.

### Scope, secrets and outbound requests

Make effective merged configuration read-only. Resolve each writable target through one role policy: owner for personal, appropriate administration for team/org, platform superadmin for deployment-global resources. Reject unknown scopes and absent stores rather than falling back to local/global state in platform mode.

Mask response copies, preserving same-resource/same-scope secrets on masked updates. Runtime use permission remains separate from metadata read permission and destination network permission.

A personal-only app credential restriction may contain team-secret exfiltration, but malicious app code could still exfiltrate personal secrets. Specify trusted app/destination binding and consent. Apply outbound SSRF and redirect credential policy consistently to tools, apps, catalog clients and remote MCP transports while retaining distinct evidence for each finding.

### Fleet and session identity

Bind all fleet stores, credentials and background execution to the same authenticated tenant identity. Remove default/general fallbacks from platform operations. Use durable scoped state for API/worker separation and multi-worker coordination.

Proposed under D-03, pending team approval: create sets creator server-side; edit preserves authoritative identity; duplicate creates a new inactive plan owned by the duplicator. The team must decide who may edit active autonomous work and whether it needs reapproval. Independently of that decision, do not infer authorization from runtime name or session-ID knowledge; verify recorded owner and backend object identity.

### Network and channel verification

One deny-aware policy must govern initial and incremental network access. If a backend cannot safely represent a broad allow with narrower denies, reject or conservatively restrict it rather than broaden access.

Channel verification requires explicit proof and trusted state transitions, not the presence of a row. Preserve the reviewer-observed 410 retirement of the historical verify route with code/regression and deployed-route evidence. The trusted authserv ID is already pinned; email trust still requires proof that receiving infrastructure strips sender-spoofed results claiming that ID. Any future shared rate-limit acceptance should cover multiple processes; this low-urgency defense-in-depth item for 128-bit tokens does not block urgent releases.

## 9. Verification and migration requirements

### Fixture

Use two orgs with identical team slugs and identical app/template/plan/credential names containing distinct synthetic canaries. Include two users in the same team, separate teams within one org, and member/viewer/team-admin/org-admin/platform-admin roles. This catches cross-user and same-org failures as well as cross-org ones.

For every denial, verify no side effects and include a successful authorized counterpart.

### Added acceptance for the omitted items

- A member cannot extract team-secret canaries using shell, file reads, alternate paths, or arbitrary code; approved credential-backed use remains possible.
- Browser upload/PDF tools cannot read/write host canaries outside the authorized root, including symlink paths.
- HTTP/fetch and relevant remote clients reject unauthorized internal/metadata destinations through DNS, IPv6, redirects and proxies; credential headers are absent at unauthorized redirect targets.
- Scope-root initialization is exercised through actual execution startup, not just a helper unit test.
- User B cannot read/restore User A's private session or artifacts through real authenticated HTTP calls, including within one team. Session-memory acceptance follows D-01: private memory denies nonowners unless explicitly shared; intentionally team-visible memory permits authorized teammates and denies other teams without exposing the underlying private session.
- The historical channel verify route returns 410 and does not mutate an existing unverified row; the supported proof-based flow rejects replay/expired/wrong-owner attempts. Deployed ingress strips spoofed trusted-ID `Authentication-Results`, including duplicates, before delivering trusted results; code pinning alone is not deployment proof.
- If shared rate limiting is adopted, requests across two server processes respect the chosen aggregate policy and concurrency protection. PR594-03 remains low-urgency defense in depth given 128-bit tokens, not a prerequisite to shipping unrelated urgent fixes.

### Existing acceptance retained

- Configured SQLite/PostgreSQL initialization fails before network/config effects.
- Global settings writes obey platform authority, including loopback requests; masked GET/update round trips retain secrets safely.
- App SQL cross-schema/privilege/filesystem escapes fail while authorized app use works.
- Artifacts remain root-confined even when the caller owns the supplied session; previews do not execute on the trusted origin.
- App sharing, slides deletion, memory consolidation and MCP mutations enforce source/destination scope and role.
- MCP caches/taps cannot cross tenants, spoof official identity, or send tokens to unauthorized origins.
- Fleet activation/status/delete/retry/restart and credentials remain tenant-bound; creator rules agree across JSON/YAML/PATCH/duplicate.
- User workloads cannot read unrelated layers/uppers or write publication storage; authorized template editing and package installation work under the chosen runtime.
- Archive attacks leave external canaries unchanged; captured templates contain no runtime-secret canaries.
- Runtime naming collisions cannot cause adoption, cleanup or browser/credential sharing.
- Direct lateral CDP/VNC access fails while authorized automation/handoff works.
- Untrusted build steps cannot obtain push authority; approved publication works.
- Network denies remain effective at startup and after grants/refresh; actual ZIP decompression is bounded.

### Finding-to-PR / test / deployment evidence matrix

This is an evidence ledger, not a list of completed fixes. “Proposed PR” means no PR number or merge evidence is recorded. Rows group related work only for handoff; each listed ID needs its own disposition, exact review/fix commit and test output before closure. No regression or deployment acceptance was run by this document correction. Existing source references above are historical evidence, not test results. All deployment evidence below is **pending** unless explicitly described as a reviewer observation.

| Finding IDs | Current state / evidence | PR or proposed workstream | Required regression evidence (not yet attached) | Required deployment evidence / closure dependency |
|---|---|---|---|---|
| TOOL-01 | Reported; reviewer severity High | Proposed runtime vault/broker PR, separate from capture | Non-admin shell/file/arbitrary-code canary denial and permitted credential-backed use | No plaintext broad team vault in execution; D-04 allowed-use semantics |
| TOOL-02, TOOL-05, TOOL-06 | Reported | Proposed agent filesystem/browser boundary PRs | Host upload/PDF escape denials, symlinks, startup scope wiring and authorized files | Actual browser/tool execution confined to owned root |
| TOOL-03, TOOL-04, TOOL-07 | Reported | Proposed tool transport/redirect PRs | Internal-address/DNS/proxy/redirect matrix and secret-header canaries | Real tool transports deny forbidden destinations and credential forwarding |
| PR593-01, PR593-03 | Reported; session test gap | PR #593 residual follow-up; exact revision pending | Two same-team users and cross-team/org authenticated HTTP session tests with no denial side effects | Private sessions inaccessible to unauthorized users; explicit shared access succeeds |
| PR593-02 | PolicyDependent; missing owner check reported | PR #593 residual follow-up; exact revision pending | D-01-selected private/shared-memory positive and negative cases | Team intent recorded; memory access does not grant private-session/artifact access |
| PR594-01 | FixedPendingVerification; reviewer-observed current 410 | PR #594 code/regression follow-up; exact correction revision pending | 410 on retired POST with unchanged row; supported proof flow and replay/expiry/owner cases | Observe retired route and supported flow on deployed revision; current code observation is not live closure |
| PR594-02 | Reported deployment dependency; reviewer says trusted ID already pinned | PR #594 deployment handoff; no missing-pinning code claim | Spoofed trusted-ID and duplicate Authentication-Results via actual receiving ingress | Operator evidence mail server strips spoofed trusted-ID results before trusted results are supplied |
| PR594-03 | Reported; low urgency, 128-bit tokens | PR #594 optional distributed-limit follow-up | If adopted, multi-process aggregate/concurrency limits | Record chosen aggregate policy and rollout; not an independent urgent blocker |
| SB-01, SB-04, SB-07 | Corroborated by earlier source investigation | Proposed scoped-storage/runtime PRs | Cross-tenant layers/uppers/edit identity denials and supported authorized workflows | D-05 runtime feasibility and backend acceptance; no unsafe fallback |
| SB-02, SB-03 | Corroborated by earlier source investigation | Proposed extraction/alias PRs | Malicious archives, traversal/overwrite, ownership and bounded extraction | External canaries unchanged; existing aliases/layers safely migrated |
| SB-05 | Corroborated by earlier source investigation | Proposed runtime identity PR | Colliding names, adoption and cleanup ownership tests | Collision cannot adopt/delete another tenant's sandbox |
| SB-06 | Corroborated by earlier source investigation | Proposed bootstrap authorization PR | Duplicate names across orgs and scoped template ID tests | Authorized bootstrap works with no cross-org mutation |
| SB-08 | Corroborated by earlier source investigation | Proposed browser/network isolation PR | Direct CDP/VNC denial, tunnel ownership and generic exposure bypass tests | Actual lateral access fails; authorized automation/handoff works |
| SB-09 | Corroborated by earlier source investigation | Proposed build/publisher separation PR | Hostile build cannot read push canary; authorized publish succeeds | Untrusted builders lack publisher authority in deployed jobs |
| SB-10 | Corroborated by earlier source investigation | Proposed capture-secret isolation PR | Capture/import canary exclusion across backends | Historical template quarantine/review and clean publication; TOOL-01 independently addressed |
| NP-01, NP-02 | Corroborated by earlier source investigation | Proposed deny-aware policy/approval PRs | Tiered denies, refresh/grants, owned sandbox and persistent-role cases | Startup and later grants preserve denies on supported backends |
| SQL-01 | Reported; source inspection incomplete | Proposed SQL authorization/isolation PRs | Cross-app/schema/privilege/filesystem denial and allowed DDL/data cases | D-02 policy, restricted runtime connection and existing-grant migration |
| CFG-01, CFG-02, CFG-03, CFG-04 | Corroborated by earlier source investigation | Independent settings masking/role/full-config PRs; urgent items in §6 | Secret canaries, same-resource masked updates and role/scope/loopback matrix | Correct runtime config retained; deployed responses/writes honor scope roles |
| MCP-01, MCP-02, MCP-03, MCP-04 | Corroborated by earlier source investigation | Independent MCP secret/write/cache PRs | Redaction/update preservation, role × scope, denied-call side effects and cache isolation | Runtime secrets preserved without narrower-scope disclosure/mutation/poisoning |
| FL-01, FL-02 | Corroborated; policy details pending D-03 | Proposed tenant-bound fleet lifecycle/identity PRs | Start/status/delete/retry/recovery and JSON/YAML/PATCH/duplicate identity cases | Worker/daemon tenant-bound stores and credentials; D-03 migration and active-edit rules |
| ART-01 | Reported; reviewer reproduction, local inspection partial | Independent urgent artifact confinement PR | Missing/wrong/owned session plus host path/symlink attacks | Deployed path denial plus §7 rotation/re-encryption/revocation evidence |
| ART-02 | Reported | Artifact response/preview follow-up, independently testable | Attachment/nosniff/CSP and safe preview behavior | HTML/JS does not execute on Studio origin |
| APP-01 | Corroborated by earlier source investigation | Proposed app credential destination/consent PR | Unauthorized target/redirect denial, secret-free errors and approved requests | D-04 consent/use policy and actual proxy transport enforcement |
| APP-02, APP-03 | Reported; relevant source inspected | Proposed app definition/source/destination authorization PRs | Shared-definition mutation and fork/promote role/scope matrix | D-02 data semantics kept separate; legitimate sharing succeeds |
| APP-04, APP-05 | Reported; APP-04 partly source corroborated | Proposed AI limits/route caps and anonymous-mode boundary PRs | Actual-route payload/budget/concurrency cases and anonymous state separation | Explicit local versus authenticated multi-user behavior and chosen limits |
| APP-06 | Corroborated by earlier source investigation | Proposed request-effective AI provider PR | Distinct team provider/model/billing canaries | Correct tenant provider and limits selected in app AI requests |
| SL-01, SL-02 | Reported; detailed investigation incomplete | Proposed slide delete/ZIP bound PRs | Role/owner delete cases; actual decompression/entry/memory/cancellation bounds | Authorized delete/import works and denied/resource-bound cases remain contained |
| INIT-01 | Corroborated by earlier source investigation | Independent urgent bootstrap guard PR | Both configured backends deny before dial/mutation; authorized first-run path | Existing deployments cannot reinitialize; bootstrap race/authority evidence |
| MEM-01 | Corroborated by earlier source investigation | Independent urgent memory mutation-authorization PR | Source/duplicate/discard/destination preauthorization; no partial side effects | Authorized consolidation succeeds without unauthorized shared deletion; not blocked by D-01 visibility choice |
| TAP-01 | Corroborated by earlier source investigation | Proposed tap authorization/cache/token PR | Global-role, reserved alias, canonical cache origin and token redirect cases | No alias spoofing, cross-tenant poisoning or unauthorized token destination |
| CAND-01 through CAND-06 | Additional candidates; independent verification pending | No fix PR assigned; pinned reviewer investigation requested | Safe reproductions at exact revision for each candidate before confirmed disposition | No closure or vulnerability confirmation claimed; assess candidate-specific runtime/migration impact |
| PR #596: `knowledge_debug_handler.go` | Later unreviewed follow-up scope; not a finding | PR #596 review scope only; exact revision pending | Review changed behavior and decide whether any regression/security follow-up is warranted | No vulnerability, severity or fix inferred from the filename or PR number |

A transition to **Contained** needs observed restriction evidence; to **FixedPendingVerification**, a recorded code correction; to **Verified**, matching regression and deployment evidence plus resolved applicable decisions. Keep **Reported** versus **Corroborated** distinct. Move a conditional verdict to **PolicyDependent** without deleting its underlying observation; record the approved decision before resolving it. Use **NotReproduced** only with attempt/revision/environment details and preserve the original report. New contradictory evidence reopens the disposition rather than retaining a stale closure. A merged PR or passing unit test alone cannot establish deployed security closure.

### Harness limitations

The earlier investigation found that `tests/e2eboot` starts a real Studio/auth/store stack but does not wire daemon fleet activation/scheduling. The inspected matrix supports PostgreSQL/Kubernetes, SQLite/Kubernetes and PostgreSQL/OpenShell; Docker helper coverage and SQLite/OpenShell wiring were incomplete. These observations require revision comparison before relying on them.

Do not count missing prerequisites, skipped tests or zero selected drills as successful security acceptance. Run destructive tests only against disposable databases and explicit dedicated namespaces. Live tests have not run in this analysis.

### Upgrade controls

Inventory existing layers, templates, aliases, session objects, app grants, fleet creator/schedule state, catalogs and caches. Quarantine ownership/provenance that cannot be established; do not infer org identity from a shared team slug. Require dry-run, backups, interrupted-upgrade tests, idempotency and a safe fail-closed state. Rollback must not silently re-enable the vulnerable boundary.

## 10. Remaining questions and requested feedback

1. Reviewer recheck **pending**: the main thread has already asked the reviewer to recheck this corrected handoff. Attach that response when received; do not treat the request as approval. Recover the remaining unspecified TOOL-02–TOOL-07 severities/pinned references and exact PR #593/#594 code/regression evidence without reopening corrections already supplied.
2. Confirm the urgent PR grouping and any feature restrictions needed until the deeper fixes ship. The main security tracking issue remains unified; releases need not be. PR594-03 is low-urgency defense in depth, not a blanket release gate.
3. Independently verify CAND-01 through CAND-06 at a pinned revision, especially full-volume builder/fleet mounts, host-tar base replacement, standard-server global secrets and creator inconsistencies. Review the later PR #596 `knowledge_debug_handler.go` change as unreviewed scope, not a presumed vulnerability.
4. Assign named team owners and record D-01 through D-05 decisions, including memory visibility, shared-app data, fleet identity/active edits, credential-use consent and the supported runtime profile. These decisions are pending; blocking scope is limited to the affected workstreams in §6.
5. Approve a concrete SQL execution privilege model and runtime profile only after feasibility testing; neither is settled by this document. Attach required per-finding code, regression and deployment evidence to §9, including deployed 410 behavior and trusted-ID spoofed-header stripping.
6. Establish the operator rotation/re-encryption/revocation runbook and the minimum live backend/storage matrix for closure.

For each response, include the tracking ID, disposition, exact commit/path/symbol, actor and trust boundary, safe synthetic reproduction, recommended fix/bypass considerations, migration impact and merge-blocking status.

**Next step:** verify the pinned findings against the selected implementation base and open narrowly scoped urgent fix PRs with regression evidence, while separately designing the SQL, sandbox, credential and fleet boundaries. No issue is considered fixed by this document update.


## 11. Approved revision: continuity ledger (2026-10-20)

This additive record supersedes current-status statements above without rewriting their historical evidence. The original `handoff-and-decisions` verification remains complete; the original `urgent-boundary-fixes` command remains failed at exit 2 before cleanup; its later phases were pending. No release, deployment, production rotation or receiving-MTA change is established. Current HEAD is `a66f59d86e92596828273ed12b6a3f9db8e28c78` (`fix(security): consolidate urgent boundary remediation`), not the earlier uncommitted baseline. `ArtifactDelivery.test.tsx` is now tracked. Earlier reports' working-tree/untracked descriptions are historical, not the current checkout. No new PR number or release is inferred from the commit.

The revision reused the same session PLAN.md sidecar. The earlier 346-line contents were recovered during planning, but an unchanged original on-disk copy cannot be claimed. Retained inventory and historical reports are the durable audit evidence; unsaved earlier conversation is not claimed recoverable. `SECURITY_REMEDIATION_LATEST_CHECK.md` and `SECURITY_CHANNEL_LINK_CHECK.md` remain untouched.

### Recorded policy and provenance

- **D-02 approved by the user:** apps remain simple external-system integrations with lookup/reference/team data. Personal maintenance remains supported with private personal state. Team-level apps and their data belong to the team: all current team members may maintain/read/write/refine/perform app-local DDL. Do not introduce creator/admin-only ordinary team collaboration. The isolated app boundary must protect other apps, databases and host resources.
- **APP-02 reassessment:** the historical Medium report remains attributed, but same-team collaboration is authorized under D-02, not itself a defect. APP-02 remains **PolicyDependent** for reconciliation of exact endpoints/roles and unresolved wider publication; cross-team/source/destination authorization must still be enforced and tested. This is not a verified fix or an unrecorded severity change.
- Publication/forking copies definitions, not personal SQL data, personal secrets or another user's credential-use authority. Org/platform publication remains separately authorized. No app-scope parameter may silently change credential scope.
- D-01 memory visibility, D-03 fleet execution principal/active edits, D-04 credential consent/destination/expiry/revocation and D-05 supported runtime remain unapproved. SQLite engine-authorizer/dependency feasibility and any fleet schema expansion remain separate gates. Proposed owners remain roles pending named assignment.
- Preserve reviewer attribution: reviewed main `6bd4861` was described by the reviewer as 18 commits after `af8140f`. TOOL-01 is **High**; TOOL-02..07 severity/pinned links remain unspecified, pending reviewer response. No scoring is guessed.
- PR #596 knowledge-debug and refresh missing/deleted login-session behavior are **separate unconfirmed review candidates**, not established vulnerabilities. Remote MCP SSE/streamable HTTP review remains unconfirmed too.

### Inventory-to-delivery ledger

Each row retains the individual IDs and original finding/boundary in §4–5. Exact changes/evidence below apply only where stated; all other implementation/test/migration evidence is pending. States use §1 definitions. No row is **Verified**. New contradictory evidence must reopen any later disposition.

| IDs | State / exact change and evidence | Owning approved phase | Migration/deployment obligation and residual risk |
|---|---|---|---|
| INIT-01; CFG-01, CFG-02, CFG-03, CFG-04 | FixedPendingVerification: earlier configured-backend/bootstrap authority, copied redaction, exact-target masked updates and global-write guards recorded at current HEAD; historical focused tests, no fresh live acceptance | urgent-independent-fixes | Deploy only after positive/negative both-storage acceptance; preserve runtime secrets/bootstrap usefulness; full role/no-effects matrix still outstanding |
| MCP-01, MCP-02, MCP-03; CAND-03 | FixedPendingVerification for recorded masking/scoped-write fixes; CAND-03 remains Reported candidate requiring narrow/global-secret side-effect reproduction | urgent-independent-fixes | All write/install/discovery callers and same-target preservation must pass; no global-secret effects; no live closure |
| PR593-01, PR593-03 | FixedPendingVerification: store/request owner checks and artifact/session regressions recorded; full real-auth matrix not run | urgent-independent-fixes | Same-team and duplicate-slug cross-org denial with authorized success; internal recovery authority separately maintained |
| PR593-02 | PolicyDependent: D-01 not approved | urgent-independent-fixes | Decide team-visible versus private session memories without exposing private sessions/artifacts |
| MEM-01 | FixedPendingVerification: recorded preauthorization correction, not live verified | urgent-independent-fixes | All source/duplicate/discard/destination/no-partial-mutation and authorized positive tests required |
| PR594-01 | FixedPendingVerification: retained 410/no-mutation and proof/persistence checks; bounded email-create error regression preserved in SECURITY_CHANNEL_LINK_CHECK.md | urgent-independent-fixes | Real authenticated owner/expiry/replay/concurrent-consume/persistence acceptance and deployed-route evidence pending |
| ART-01 | Contained: platform unconfined PullFile paths denied before backend effects; historical regression in PROGRESS; transcript owner delivery remains | sandbox-and-artifact-boundaries; closure-matrix-and-handoff | Restore trusted rooted live reader; no workload cat/tar, no host fallback; full live binary/shell-created/stopped/restored/browser/TUI positive/negative contract retained; mandatory incident response remains |
| ART-02 | FixedPendingVerification: passive MIME allowlist, attachments/nosniff/CSP and authenticated blob/PDF transport recorded in LATEST_CHECK | sandbox-and-artifact-boundaries; closure-matrix-and-handoff | Actual browser rendering/download/media and remote-terminal compatibility remain unverified; do not equate transcript containment with full delivery |
| SB-01, SB-02, SB-03, SB-04, SB-05, SB-06, SB-07, SB-08, SB-09, SB-10 | Corroborated by historical source evidence; no new fix | sandbox-and-artifact-boundaries (SB-10 also tools-apps-and-sql) | D-05 trusted execution/reader/capture/publisher architecture; scoped immutable chains; malicious extraction/adoption/cleanup/browser/publication tests; quarantine/backup/rollback and historical secret captures |
| NP-01, NP-02 | Corroborated; no new fix | sandbox-and-artifact-boundaries | Higher-tier deny precedence, owned-session grants, persistent-role checks and production policy/lateral-port acceptance |
| CAND-01, CAND-02, CAND-05, CAND-06 | Reported candidates; historical observations retained, independent reproduction pending | sandbox-and-artifact-boundaries | Include builder/fleet/whole stores, base replacement, ownership adoption/cleanup and immutable capture; not presumed closed by ordinary-session fixes |
| TOOL-01, TOOL-02, TOOL-03, TOOL-04, TOOL-05, TOOL-06, TOOL-07 | Reported; no new fix or live reproduction | tools-apps-and-sql | D-04 inaccessible credential broker with authorized-use positives; scoped browser/upload/PDF/direct-drill paths; immutable execution scope; SSRF/dial/redirect canaries; preserve trusted local access |
| SQL-01 | Corroborated current-source authorization/identity/provisioner issues; engine isolation not implemented | tools-apps-and-sql | D-02 approved positives; SQLite authorizer gate, separate PostgreSQL execution authority; ambiguous legacy personal/team state quarantine and interrupted ownership migration |
| APP-01, APP-06 | Corroborated; no new fix | tools-apps-and-sql | D-04 personal-use consent, destination binding and team-effective AI/budget tests; external integration success required |
| APP-02 | PolicyDependent; D-02 resolves ordinary team collaboration as authorized, endpoint reconciliation still pending | tools-apps-and-sql | Do not restrict ordinary team maintenance/read/write/DDL; verify personal privacy and wider publication authority |
| APP-03, APP-04, APP-05 | Reported (APP-04 partly source corroborated); no new fix | tools-apps-and-sql | Canonical scoped app/source/destination checks, actual body-cap routes/AI limits, anonymous network boundary; saved parent iframe identity; no secret/state copy on publication |
| FL-01, FL-02 | Corroborated; D-03-dependent semantics unresolved | tenant-fleet-lifecycle | Durable tenant-bound lifecycle/principal/revision and retry/restart/permission-loss tests; paused legacy ambiguity; schema gate before new persisted fields |
| CAND-04 | Reported candidate; creator inconsistencies retained | tenant-fleet-lifecycle | JSON/YAML/PATCH/duplicate/column reconciliation; do not assign latest editor as principal |
| TAP-01, MCP-04 | Corroborated; no new fix | hardening-and-incident-rehearsal | Authorized global catalog changes, reserved origins/cache identity, scoped tool cache and token redirect tests; remote MCP review separately unconfirmed |
| SL-01, SL-02 | Reported; no new fix | hardening-and-incident-rehearsal | Resolve personal/team deletion authority, actual decompression/resource bounds and allowed import/delete; unrelated slides work untouched |
| PR594-02 | Reported operational prerequisite; trusted authserv-ID already pinned | hardening-and-incident-rehearsal | Actual receiving-MTA spoofed/duplicate-header stripping evidence, separately authorized operator action |
| PR594-03 | Reported optional low-priority hardening, 128-bit tokens | hardening-and-incident-rehearsal | Distributed limiter only if approved; not a blocker for independent urgent releases; atomic proof single-use remains mandatory |
| PR #596 knowledge-debug; refresh-session candidate; remote-MCP transports | Reported review scope only, not verified vulnerabilities | hardening-and-incident-rehearsal (remote transports also tools boundary review) | Reproduce safely before claim/change; bounded follow-on approval if production edit blast radius expands |

### Supersession and verification ownership

The revised urgent-independent phase replaces the old serial release coupling, **not** its evidence or artifact requirements. Full ART reader/preview/browser/terminal acceptance moves from original urgent-boundary-fixes to sandbox-and-artifact-boundaries and final closure. No feature-disable assertion replaces a positive capability test. Every original longer-track obligation remains in the approved seven-phase plan; all unimplemented tracks (including `all`) fail selection rather than yielding empty success.

Phase continuity changes only the strict harness and durable record. New parser tests initially produced 13 failures (exit 1); after implementation, 46 parser/selection tests passed. The new no-flag TestE2E_Security prefix bootstrap probe failed before the guard change on both synthetic storage profiles, reaching disposable local setup instead of preflight; after the change the focused preflight command passes. These are harness unit/synthetic subprocess results, **not** live security acceptance or approval of infrastructure.
