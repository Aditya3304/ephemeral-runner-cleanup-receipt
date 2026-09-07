# Ephemeral Runner Cleanup Receipt — local build proposal

Status: approved by the user on September 7, 2026. Milestones a through e are implemented and validated; confirmation is required before f. Wording describing proposals below records the approved design; current implementation status is maintained in milestones.md and api-validation.md.

## Confirmed direction

- One developer: Aditya. All development and application compute run on this laptop, using Ubuntu under WSL 2.
- Hackathon scope, without a 60-hour deadline. Completion is determined by working behavior and acceptance tests.
- No paid services, AWS provisioning, credit-card-dependent trials, or spending on cloud resources.
- The user selected `Aditya3304` and public visibility. Repository created: https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt.
- The user requires fully local/offline execution. GitHub is a source-code publishing destination, not a runtime dependency. Dependency downloads and source publishing are setup/development activities; the running demonstration must work with external network access blocked.
- Preserve PostgreSQL, Go/Cobra/client-go, the TypeScript guard action, a separate trusted finalizer, real Cosign signature verification, React/Vite/Tailwind/TanStack, incidents, and the watchdog.
- Obtain plan approval before coding and demonstrate each implementation milestone before requesting confirmation to move to the next.

## Verified environment

Inspected September 7, 2026, without changing configuration:

| Component | Finding | Planned action after approval |
|---|---|---|
| WSL | Ubuntu 26.04, WSL 2; Docker Desktop distribution also running | Use Ubuntu for development |
| Docker | Client/server 29.1.3; no running containers | Use existing installation |
| Go | 1.27.1 at `/usr/local/go/bin/go`, absent from the inspected shell PATH | Correct project development PATH |
| Node | 22.22.1; npm currently resolves through a Windows path | Provide a Linux Node 24 toolchain consistent with the PDF |
| kind | 0.30.0; no clusters | Create a project-specific cluster |
| kubectl | 1.36.1; no current context | Pin a compatible kind node version; do not change unrelated contexts |
| Cosign | Installed, but reports `devel` and unknown build provenance | Validate or replace with a pinned, verified upstream release |
| Terraform | 1.16.1 installed | No AWS apply; cloud deployment excluded |
| GitHub CLI | Authenticated as `Aditya3304` | Use only the confirmed account |
| Resources | WSL sees 7.5 GiB RAM and 2 GiB swap; Windows C: has about 56 GiB free | One active demo runner at a time; bounded logs and container resources |

The WSL filesystem's large virtual capacity is not additional physical disk capacity.

## Offline changes requiring plan approval

1. Run real local CI jobs through a Go coordinator and Kubernetes Jobs instead of GitHub's hosted workflow control plane. Implement the GitHub Action adapter and workflow definitions as requested, but leave hosted workflows disabled and label their live GitHub integration as unvalidated. Local lifecycle tests exercise the guard main/post implementation; they do not prove GitHub's own cancellation or post-step semantics.
2. Preserve actual Sigstore keyless signing by running a private local Sigstore stack: a local OIDC identity issuer, Fulcio, certificate transparency, Rekor, and pinned trust material. Certificates identify the local finalizer; they do not assert a GitHub workflow identity. Supporting components needed by the selected upstream Sigstore release are included. No public identity provider, transparency log, TUF refresh, or timestamp endpoint may be required at runtime.
3. Use community MinIO, with S3-compatible versioning, encryption, object retention, and digest-addressed evidence. This replaces AWS S3 for this local scope and does not constitute an AWS S3 deployment test.
4. Use the explicit contract proposals below. The seven-page PDF summarizes the data model but does not supply exact SQL, JSON Schema, OpenAPI, retention settings, or retry parameters. No original `PLAN.md` has been supplied in this task. These proposals must not be mistaken for missing source material.

These changes require approval because offline execution is incompatible with the PDF's real GitHub run and public GitHub-OIDC signing acceptance gate. Local jobs, signatures and database/storage operations will be real; the GitHub and AWS acceptance gates will remain explicitly deferred.

## Proposed local architecture

The application comprises a dedicated kind cluster for disposable CI runners and test resources; a trusted local coordinator and separate finalizer execution environment; a private Sigstore deployment; PostgreSQL 17.11; local S3-compatible evidence storage; a Go API; and the React dashboard. Infrastructure services use project-specific containers and volumes. Local services are reachable only where necessary; PostgreSQL is available to the API and the one-shot migration process, not to CI jobs or the browser.

The coordinator records a durable run identity and launches real test commands in disposable Kubernetes Jobs. The finalizer runs approved, pinned application code after job termination. It never checks out PR code, executes uploaded evidence, or reuses an untrusted job's workspace. Only the finalizer can obtain its signing identity and archive credentials. Labels alone are not treated as a security boundary: credential isolation, disposable execution, storage boundaries, RBAC and network-access tests must enforce that separation.

The guard uploads preliminary evidence into a local staging area using run-scoped authorization. Staging is untrusted and separate from the immutable archive. A trusted collector persists job stdout/stderr outside the runner before disposal; premature deletion or a collection gap is reported as incomplete coverage. The finalizer validates evidence against the coordinator's job identity and trusted observations. This local transport replaces the PDF's GitHub artifact/log retrieval path in the offline demo.

The local signer generates an ephemeral signing key for each receipt. A short-lived identity token authorizes a local Fulcio certificate; the signing event receives actual local transparency evidence. API and CLI verification use an independently installed trust root and enforce the exact local issuer and finalizer identity. They never trust a root merely because it accompanied an uploaded bundle. Trust and identity services are inaccessible to untrusted jobs except for any narrowly necessary, tested public discovery endpoints.

Pin and preload all container images, language dependencies, tools, UI assets and trust configuration during setup. Offline startup must not pull images or call external identity, package, registry, log or telemetry endpoints. GitHub workflows remain disabled; no paid infrastructure is provisioned. A dedicated acceptance test blocks outbound internet and runs cleanup, signing, ingestion and dashboard viewing from start to finish.

The local application will use TLS for PostgreSQL and object-store connections. Secrets are generated locally, excluded from Git, and mounted only into their intended services. Development key material is kept outside job workspaces. No secrets are copied from unrelated projects.

MinIO will use encrypted, versioned objects and an object-retention policy. Propose seven days of retention with no automatic destructive cleanup during implementation. Verify that service credentials cannot overwrite or delete the exact archived object versions. Record object version IDs as well as digests. On one laptop, the administrator can still destroy the underlying disk or encryption keys; this demonstration establishes storage-service enforcement, not protection against the laptop owner.

## Nine PDF phases, adapted without hourly deadlines

The phase labels preserve a one-to-one mapping to the PDF. The implementation sequence below follows the user's requested milestones; consequently some dashboard/storage work finishes after finalizer work.

| PDF phase | Local deliverable | Exit evidence |
|---|---|---|
| 1. Lock idea and scope | Approved offline substitutions, trust boundaries, receipt/data/API contracts, and acceptance checklist | User approval of this revised proposal |
| 2. Parallel foundations | Solo repository scaffold, pinned tools, Goose migrations, local PostgreSQL, repeatable checks | Fresh database migrates successfully and constraints work |
| 3. Core cleanup path | `proofctl`, local job coordinator, guard main/post implementation and disabled GitHub adapter; safe deletion, workspace and credential checks | Real kind success, test failure, and cleanup-timeout cases |
| 4. Dashboard and storage | Transactional API ingestion, searchable metadata, immutable references, React list/detail/filter/incident views | Verified data is queryable and rendered accurately |
| 5. Finalizer and signing | Local identity/Sigstore stack, trusted finalization, full local log collection, archival, canonical JSON, Cosign bundle, verification policy | Signed archived receipt; modification and wrong identity are rejected |
| 6. Full integration | Real offline CI run through cleanup, finalization, API verification, and dashboard | One reproducible run with internet blocked; GitHub-hosted gate explicitly deferred |
| 7. Hardening and faults | Watchdog plus cancellation, missing evidence, resource deletion, duplicate, tampering, DB and object-store failure tests | Fault matrix passes with honest verdicts and deduplicated incidents |
| 8. Deployment and polish | Repeatable local start/stop, pinned images, migrations, local persistence, setup and troubleshooting documentation | A fresh local setup works using documented steps; no AWS provisioning |
| 9. Freeze and rehearsal | Stable contracts/migrations, release tag, backup, sample signed receipts, two rehearsals and backup recording | Repeatable success and failure demonstrations with verification evidence |

## Implementation milestones and review gates

| Milestone | Implementation | Demonstration before confirmation |
|---|---|---|
| a | New repo; Go/Action/dashboard scaffolds; PostgreSQL and Goose migrations for receipts, incidents, finalization attempts | Start PostgreSQL, apply migrations from empty state, exercise uniqueness and receipt/incident rollback |
| b | Go `proofctl begin`, `inventory`, `cleanup --timeout 120s`, `receipt`, `verify`; client-go | Create project resources, inventory, remove, independently confirm absence; protect an unrelated sentinel resource |
| c | TypeScript/Node 24 guard main/post state and local transport; real local CI coordinator; GitHub adapter/workflow definitions left disabled | Passing and failing local jobs invoke cleanup; cancellation and missing post are explicitly observable; hosted behavior remains unvalidated |
| d | Private Sigstore services and identity, separate finalizer, schema/identity limits, complete local logs, S3 archive, canonicalization and keyless signing | Show a real archived receipt and bundle; verify them with the CLI against pinned local trust |
| e | Go Chi + pgx/v5 API, signature re-verification, sanitization, atomic receipt/incident writes, queries and deduplication | Valid ingestion, rejected tampering, concurrent duplicate delivery, database failure and later recovery |
| f | React/Vite/Tailwind/TanStack dashboard | Receipt list/detail/search; repository/verdict/date/incident filters; incident visibility and artifact verification results |
| g | Scheduled watchdog on the laptop, independent from disposable job runners | Detect a missing finalization, retry it, and expose unresolved failure without inventing success |
| h | Fault injection, integration tests and operational hardening | Run the complete acceptance matrix and save actual results |
| i | Local deployment and rehearsal replacing the optional AWS milestone | Start the whole system, repeat the demo, restore a known-good metadata backup, retain sample evidence |

Early milestones demonstrate their own completed behavior. The first complete signed receipt appears at finalizer milestone d; the dashboard path is completed at f. Scaffolds and test fixtures are never presented as verified end-to-end operation.

At every gate, report what works, what was tested, remaining PDF requirements, and every approved deviation. Obtain confirmation before the next implementation milestone.

## Proposed contracts to freeze before scaffold implementation

- Identity: a provider discriminator, repository identity, run ID, run attempt and job ID form the deduplication key. The local coordinator allocates real local run/job IDs and records a source commit. A future GitHub adapter uses GitHub IDs; it cannot label a local run as a GitHub run. Names remain searchable display metadata. A finalizer retry is distinct from a CI run attempt.
- Verdict: `pass`, `fail`, or `partial`. Required observable failures result in `fail`; missing, unsupported, or unobservable required coverage prevents `pass`. A failed test command by itself does not mean cleanup failed.
- Coverage: independent workspace, credentials, logs, resources, and runner-disposal sections. Each records status, observer, reason, and evidence references. Untrusted job claims are distinguishable from trusted observations. A signature authenticates the finalizer's receipt; it does not turn an unobserved job claim into an independently proven fact.
- Deletion boundary: resources are restricted to the run namespace and an explicitly supported set of tracked resources. PVC/PV handling verifies ownership and actual absence. No broad cluster deletion, host-directory wiping, force-removal of unrelated finalizers, or cleanup of user credentials.
- Workspace meaning: unlinking and checking every path in the disposable job workspace, including hidden files and symlink boundaries. This is filesystem absence verification, not forensic erasure of SSD blocks. Runner destruction is checked by an observer outside the runner; unsupported storage-erasure claims remain explicit.
- Credentials: verify deletion of run-scoped files and Kubernetes service-account/token resources; distinguish this from revocation of an external cloud credential. No AWS credential lifecycle is claimed in the local demo.
- Canonicalization: propose RFC 8785 JSON Canonicalization Scheme, a versioned JSON Schema, strict decoding, and SHA-256 digests. The canonical receipt does not include its own digest; its external reference and bundle provide that binding. Digests cover the exact archived byte sequences.
- Storage references: known endpoint/bucket/prefix, object key, object version, digest, size, and content type. The API never fetches arbitrary caller-controlled URLs. Archive failure blocks a passing receipt.
- Signature policy: exact expected local OIDC issuer and finalizer identity, certificate and bundle verification, transparency evidence, and permitted finalizer revision. The offline application has no skip-verification mode. Signatures must come from the real local Sigstore services; fake bundles and static-key-only signing do not satisfy acceptance. Public Sigstore/GitHub identity verification remains a separate, unvalidated integration profile.
- Metadata: `receipts` stores sanitized searchable metadata, signature result and references; `incidents` stores a unique issue key, receipt relationship, reason/state and timestamps; `finalization_attempts` stores identity, stage, result, error code, retry time and lease information. Full logs and raw evidence stay out of PostgreSQL.
- Missing evidence: the trusted finalizer can issue a signed partial/failure receipt that states exactly what is absent, allowing a linked incident. Invalid submitted signatures do not create a receipt. If signing/ingestion is unavailable, operational failure lives in attempt state or durable local retry state until a valid receipt can be produced. The API remains the runtime database writer.
- Idempotency: exact repeated receipts return the existing record; a different receipt for the same job identity is an explicit conflict, not a silent overwrite. Retry transient finalization failures before emitting a terminal incomplete receipt. Once a terminal receipt is committed, preserve it and its incident; a new CI attempt receives a new identity. Automatic replacement of a terminal partial/failure receipt is excluded from v1.
- API proposal: ingestion, paginated receipt list/detail, artifact verification, incident queries, and authenticated finalization-attempt endpoints. Query filters include repository, verdict, completion date, and incident state; text search includes workflow/job/run identity.
- Incidents: default to local tracked incidents with a nullable GitHub issue URL. Publishing GitHub issues is optional and requires an explicit destination and authorization.
- Limits: propose 1 MiB preliminary JSON, 1 MiB canonical receipt, 2 MiB signature bundle, bounded decompression, and 100 MiB archived logs per job. An exceeded limit results in explicit incomplete coverage and an incident; truncation never masquerades as complete logs.
- Retry policy: propose a five-minute watchdog interval, retry delays of one, five, fifteen and sixty minutes, and a six-attempt limit. Persistent unresolved runs remain visible after exhaustion. Retry scheduling survives a database outage using durable local state plus enumeration of the coordinator's run ledger. On laptop restart, overdue work is reconciled. The ledger stores operational recovery information; PostgreSQL remains the metadata index.
- No separate accounts, multi-tenancy, comments, incident editing, analytics database, charts, or live-refresh features are added to v1. The dashboard starts as a local interface.

## Acceptance checklist derived from the PDF

All boxes are currently unverified. Completion requires actual tests and saved evidence. Checks below use the proposed local CI and storage equivalents. Original GitHub/AWS-specific gates are separately listed as deferred; they will not be marked complete based on local tests.

- [ ] Successful test job invokes cleanup and produces an honest cleanup verdict.
- [ ] Failed test job also invokes cleanup.
- [ ] Namespace, service account, and supported tracked volumes are confirmed absent.
- [ ] Workspace verification detects no residual paths; deliberately left paths prevent success.
- [ ] Downloaded archived logs match the signed receipt's SHA-256 digest and cover the complete locally collected job log; gaps prevent success.
- [ ] CLI verification and dashboard/API verification both succeed for the same genuine receipt.
- [ ] Duplicate and concurrent finalizations produce one receipt and no duplicate incident.
- [ ] Tampered receipt, modified evidence, invalid bundle, wrong issuer and wrong signer are rejected before receipt insertion.
- [ ] Database failure preserves archived objects and schedules recovery; recovery succeeds without duplicates.
- [ ] Object-store failure prevents a passing receipt and leaves a visible retry/failure path.
- [ ] Each cleanup failure creates one actionable tracked incident.
- [ ] Dashboard filters work for repository, verdict, date and incident state.

Additional tests required by the PDF's trust rules and hardening phase:

- [ ] Graceful cancellation attempts cleanup; abrupt runner deletion or skipped post is discovered by the watchdog.
- [ ] Deletion timeout, inaccessible Kubernetes API and unobservable runner disposal never become `pass`.
- [ ] An unrelated resource and all host workspaces survive cleanup tests unchanged.
- [ ] A resource ownership mismatch prevents unsafe deletion.
- [ ] The trusted finalizer never checks out or executes PR-controlled code or treats an evidence file as executable.
- [ ] Job evidence identity, schema, size, artifact name, run attempt and job association are validated.
- [ ] A job runner cannot obtain archive/admin credentials, write database metadata, invoke a trusted signer, or access host credentials.
- [ ] Receipt plus incident insertion is atomic, including a forced rollback test.
- [ ] Archived objects are encrypted and versioned; protected versions resist overwrite/deletion using service credentials.
- [ ] Missing evidence remains explicit; signing and log-download outages produce accurate attempt/incident state.
- [ ] Watchdog recovery survives laptop/service restart and does not duplicate unresolved incidents.
- [ ] A complete run starts, cleans, signs, archives and verifies with external network access blocked and no runtime downloads.
- [ ] No workflow depends on a paid service or enables metered usage that could violate the zero-spend requirement.
- [ ] A tagged local release supports two rehearsals, sample signed receipts, a known-good database backup, and a backup demo recording.

Definition of success for the proposed offline substitution: a complete real local CI run is visible in the dashboard, its keyless signature verifies against the expected local finalizer identity and pinned private Sigstore trust, logs/evidence are retained in the local S3-compatible archive with verified digests and storage protections, observable cleanup absence is confirmed, and failures generate one actionable incident. The entire demonstration works with internet access blocked.

Deferred original acceptance gates: actual GitHub workflow execution and full GitHub job-log retrieval; GitHub OIDC identity/public Sigstore trust; AWS S3 enforcement; RDS, ECR, App Runner and EKS deployment. Implemented adapters and documented future mappings do not count as live validation of those services.

## Deviations and exclusions to approve

| PDF requirement or organization | Local adaptation | Reason |
|---|---|---|
| Three parallel owners; fixed hour gates | One implementer; ordered milestone gates | User's solo, no-deadline instruction |
| GitHub Actions runtime and GitHub job logs | Real local coordinator, Kubernetes Jobs and durable local log collector; retain disabled GitHub integration code | Offline execution; this runtime substitution needs approval |
| GitHub OIDC/public Sigstore | Local OIDC, Fulcio, CT log, Rekor and pinned private trust; genuine ephemeral-key signing | Offline execution; local identity is not a GitHub attestation |
| AWS S3 | Local MinIO with tested encryption/versioning/retention | Local-only, zero-spend requirement; specific substitution pending approval |
| Private RDS | Local PostgreSQL 17.11 with TLS and equivalent schema | Local-only requirement; PostgreSQL itself is preserved |
| ECR and App Runner | Locally built, pinned container images and local services | No paid hosting |
| AWS Secrets Manager | Locally generated, excluded, restricted secret files | No AWS dependencies |
| Terraform cloud apply and optional EKS | Excluded from execution; retain an architecture note describing the future mapping | Cloud deployment is outside the user-approved scope |
| Job evidence upload | Local staging transport | Offline evidence transport; pending plan approval |
| Immutable S3 evidence | Storage-service protection on the laptop | Cannot prove independent off-machine durability or resistance to the laptop administrator |
| GitHub issue URL | Nullable; dashboard incidents implemented first | External issue publication not yet authorized |

No substitution of SQLite, unsigned receipts, simulated verification, or a finalizer that executes PR code is proposed.

## Sources checked

- User's goal objective and `ephemeral-runner-cleanup-receipt-plan.pdf` (seven pages), plus the user's solo/local/zero-spend and fully offline clarifications.
- [Sigstore custom components](https://docs.sigstore.dev/cosign/system_config/custom_components/): private service endpoints and explicit trust-root configuration.
- [Sigstore scaffolding](https://github.com/sigstore/scaffolding): upstream local-stack deployment building blocks; exact versions and resource needs must be validated during implementation.
- [Sigstore blob signing](https://docs.sigstore.dev/cosign/signing/signing_with_blobs/): keyless signing and bundle contents.
- [MinIO encryption design](https://github.com/minio/minio/blob/master/docs/security/README.md) and [local KMS configuration](https://github.com/minio/minio/blob/master/docs/kms/IAM.md): encryption mechanisms to validate against the selected pinned release.
