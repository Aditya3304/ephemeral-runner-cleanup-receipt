# Trusted finalizer (milestone d)

`cmd/finalizer` consumes a committed coordinator publication, archives bounded
snapshots, creates `cleanup-receipt/v1`, signs it using real keyless Cosign, verifies
the signature against operator-pinned trust and identity, and archives the exact
receipt and bundle. It publishes a local result marker only after verification.
It makes no API or PostgreSQL writes. API ingestion and incidents are milestone e.

## Frozen integration contract

Run the approved binary as:

```sh
/usr/local/bin/finalizer --config /config/finalizer.json --run <32-hex-local-run-id>
```

The image build must set `-ldflags '-X main.buildRevision=<full-source-commit>'`.
An empty, abbreviated or mismatching embedded revision is rejected by the CLI.
The operator pins the built image by its content digest at launch. Receipt
`finalizer.revision` must equal that embedded revision. Job `source_revision` is
a separate field copied from the trusted coordinator ledger.

Operator configuration (replace each descriptive placeholder with a real path or
digest; paths below illustrate the mount contract):

```json
{
  "input_dir": "/input",
  "state_dir": "/state",
  "output_dir": "/output",
  "archive_config": "/secrets/archive/config.json",
  "cosign_binary": "/usr/local/bin/cosign",
  "signing_config": "/trust/signing-config.json",
  "trust_root": "/trust/trusted-root.json",
  "tls_ca_file": "/trust/ca.pem",
  "policy": {
    "revision": "<embedded full source commit>",
    "issuer": "https://issuer:8443",
    "identity": "finalizer@cleanup-receipt.local",
    "trust_root_sha256": "<64 lowercase hex>",
    "cosign_sha256": "<64 lowercase hex>",
    "signing_config_sha256": "<64 lowercase hex>"
  },
  "token": {
    "endpoint": "https://issuer:8443/token",
    "ca_file": "/trust/issuer-ca.pem",
    "cert_file": "/secrets/signing/finalizer.crt",
    "key_file": "/secrets/signing/finalizer.key"
  }
}
```

Configuration accepts readable JSON but rejects unknown fields, duplicate keys
and trailing JSON. Only an operator may supply this configuration. No option is
loaded from job evidence, a job URL, a checkout or environment variables. Fixed
local issuer, signer email and token endpoint are enforced in this CLI profile.

The separate scratch container runs as UID/GID 65532, with a read-only root and a
private bounded `/tmp`. Its mounts are the read-only coordinator root `/input`,
read-only operator configuration, public trust and named secret volumes, and its
own writable state/output volumes. It has no checkout, job workspace, Docker
socket, Kubernetes credentials, shell or host signing credentials. Approved
binaries are built into the image. Main owns image construction and launch scripts;
this package does not start containers or execute job commands. Mount and network
isolation require the integration tests; receipt validation cannot prove them.

### Coordinator boundary

The coordinator publishes `/input/<run>/run.json`, `observations.json` and finally
`finalization-ready.json`. The readiness marker has exactly:

```json
{
  "kind": "local-ci-ready/v1",
  "identity": {"provider":"local","repository":"owner/repository","run_id":"<run>","run_attempt":1,"job_id":"<job>","source_revision":"<commit>"},
  "ledger_sha256": "<exact run.json digest>",
  "observations_sha256": "<exact observations.json digest>"
}
```

The actual marker and input JSON are canonical. The marker is an atomic last
write after its inputs are durable. Missing/invalid readiness defers finalization
before freezing state or obtaining a token. Old runs need coordinator collection
to publish the marker. A mere `phase: complete` is insufficient: the coordinator
can crash between ledger completion and observation publication.

`coordinator.ValidateFinalizationReady` verifies the publication binding.
`coordinator.ValidateObservations(data, ledger, ledgerBytes)` verifies the exact
ledger digest, full identity and independently derived observations.
`coordinator.ValidateEvidence(data, ledger)` validates preliminary job evidence,
including revision, cluster and namespace UIDs, canonical form and coverage.
The finalizer also checks the preliminary evidence and logs against the ledger's
exact digests and byte counts. The raw log object contains the coordinator's exact
bytes, including CRI framing when that is what the trusted collector produced.

Job claims remain `observer: job`; a signed receipt never upgrades them. Trusted
coverage comes from the independently validated coordinator observations. Observed
failures are sticky, including valid job-reported failures. Failed job commands
alone do not imply failed cleanup. Missing, invalid or oversized job evidence can
produce an honest signed partial/fail receipt after coordinator publication is
committed. Readable invalid evidence is archived as raw bytes, never executed.

### Archive boundary

`Archiver` requires `Put(ctx, name, bytes) (archive.Ref, error)` and
`Verify(ctx, ref) error` and `Protect(ctx, ref) error`. Production uses
`internal/archive.Client`.
It accepts only `run.json`, `observations.json`, `cleanup-evidence.json`, `job.log`,
`receipt.json` and `receipt.bundle.json`. Every returned reference is shape checked,
compared to the local byte digest/size, and verified through the configured archive
client before being committed to state. Resumed references are verified again.
Before first publication, including recovery after a long offline interval, every
saved reference receives `Protect`: its exact existing version is verified and
COMPLIANCE retention renewed to at least seven days from admission. This includes
receipt and bundle references and sixth-attempt recovery. Recovery never calls
`Put` to select a newer version. Historical completed replays use read-only `Verify`
and do not renew retention.
The archive package enforces its exact configured store/bucket/key prefix,
immutable version, digest, size, encryption and retention. Receipt fields never
select a remote endpoint. Names do not select object keys; archive keys derive
from SHA-256, and retrying a write must recover the protected identical version.

References have **exactly** `store`, `bucket`, `key`, `version_id`, `sha256` and
`size_bytes`, matching `docs/database.md`. `store` is an alias, never a URL. There
is no `content_type`, `size`, endpoint or credential field in a receipt reference.

### Sigstore boundary

`TokenProvider.Token(ctx)` returns token bytes. The production `MTLSProvider` sends
an empty-body POST to the fixed HTTPS token endpoint, with no query or redirects,
using the mounted CA and a client certificate whose URI is exactly
`spiffe://cleanup-receipt.local/finalizer`. It accepts a bounded response containing
`id_token`, `expires_in: 120` and `token_type: Bearer`. Fulcio and Cosign validate the
token and resulting certificate. The issuer integration uses audience `sigstore`.

`Cosign` hashes the approved executable, signing configuration and independent
trusted root against the policy before use. The executable is immutable in the
approved read-only image. Configuration, root, receipt and token are copied into
a private temporary directory. The token is a mode-0600 file; process arguments
contain its path only. No shell is invoked. Ambient credentials, proxy settings
and public Sigstore configuration are not inherited. Subprocess output is discarded
so credentials cannot leak into errors or retry state. Requests have deadlines.

Signing uses `sign-blob --signing-config ... --trusted-root ... --identity-token
<private-file> --oidc-disable-ambient-providers --bundle ...`. Verification uses
`verify-blob --offline --trusted-root ... --bundle ... --certificate-identity
finalizer@cleanup-receipt.local --certificate-oidc-issuer https://issuer:8443`.
There is no skip-verification or static-key fallback. Operator signing configuration
must point only to the private Sigstore services; launch/network integration owns
that configuration. Never install trust material supplied by a job or bundle.

## Receipt schema and validation

The versioned JSON Schema is `internal/finalizer/receipt.schema.json`.
`ParseReceipt` is the authoritative validator: it additionally checks RFC 8785
canonical bytes, exact required fields, UTF-8, byte bounds, chronology, assignment
relationships and derived verdict. JSON Schema alone cannot verify these external
facts or canonical serialization. Duplicate/unknown keys, case aliases, null
collections, missing fields and trailing data are rejected. `VerifyReceipt` also
requires independently expected identity, UID binding and signer policy before
signature verification. A valid signature by itself does not establish them.

The top-level fields are:

| Field | Meaning |
|---|---|
| kind | `cleanup-receipt/v1` |
| identity | Provider, repository, run, CI attempt, job and full source revision |
| binding | Cluster UID; resource/runner namespaces and UIDs; job/pod UIDs; ledger digest |
| finalizer | Embedded source revision, exact issuer/email and pinned tool/config/root digests |
| started_at, completed_at, finalized_at | Ordered coordinator and frozen finalization times |
| evidence_status | `accepted-untrusted`, `missing` or `invalid` |
| coverage | Exactly workspace, credentials, resources, logs and runner_disposal |
| objects | Required ledger and optional observation/preliminary-evidence six-field references |
| log_objects | Zero or one exact archived log reference |
| verdict | Derived `pass`, `fail` or `partial` |

Each coverage entry contains exactly `status`, `observer`, `reason`. Reasons are
sanitized bounded metadata, at most 512 bytes, consistent with PostgreSQL's limit.
Original observations remain in the archive. A pass requires all five components
verified by trusted observers, valid preliminary evidence, complete UID binding,
an observation archive and a log reference. Any failed component yields fail.
Incomplete/untrusted coverage yields partial. Missing preliminary evidence keeps
workspace coverage explicitly unobservable even if disposal was independently
confirmed. Empty allocation UIDs can describe provisioning failure but cannot pass.

Receipt JSON is at most 1 MiB, the detached bundle 2 MiB and logs 100 MiB. JSON
snapshots are at most 1 MiB. No decompression is performed. Regular-file/no-follow
reads reject symlinks and special files; private directories reject symlink ancestry.
Untrusted files do not supply filenames, executable paths or remote URLs.

The receipt excludes its own digest and bundle. `cleanup-finalization-result/v1`
contains the full identity, verified receipt and bundle references, and
`signature_state: verified`. The database/API must independently verify these
artifacts and derive sanitized metadata when that milestone is implemented.

## Durable state, retries and replay

State and output directories use the SHA-256 of the canonical deduplication
identity (source revision replaced with an empty string) as the directory name.
Thus a changed source revision for the same job identity conflicts instead of
creating a second receipt. Full source revision and every input digest are still
pinned inside state. The coordinator readiness marker is frozen as a fifth local
snapshot; the receipt binds its ledger/observation inputs through archive digests.

The finalizer takes a nonblocking process lock and writes through temporary files,
file fsync, atomic rename and parent-directory fsync. It freezes a finalization
timestamp once, then records every successful archive reference, receipt digest,
bundle digest and stage. Identical retries preserve receipt bytes. Changed input,
identity, revision or policy is a conflict; snapshots and signatures are not silently
replaced. Completed replays reverify signatures and every archived version.

There are at most six charged attempts, with delays of 1, 5, 15, 60 and 60 minutes.
Archive/sign/verify failures retain snapshots and completed work, a machine error
code and the next due time. Exhaustion retains state for inspection. Recovery of
an already archived receipt **and** bundle runs before the attempt cap: after a
crash on attempt six, verification and local publication can finish without another
signature/upload. Failed reconciliation retains `recovering` state and a one-minute
retry time; this is recovery of finished work, not a seventh signing attempt.

Output is `/output/<identity-digest>/receipt.json`, `receipt.bundle.json`, then
`result.json` as the last publication marker. Consumers require `result.json` and
must verify its referenced bytes. Output can be reconstructed from verified private
state. No successful publication is claimed while signing or archival is unavailable.

## Verification scope

Focused Go tests cover strict schema rejection, job/trusted distinction, identity,
UID and revision mismatches, missing/invalid evidence, snapshot/bundle/archive
tampering, replay conflicts, backoff/exhaustion, archive/sign/verify recovery,
coordinator publication readiness, and recovery at the sixth-attempt publication
boundary. Boundary doubles in unit tests are explicitly test-only and prove no
Sigstore or storage-service behavior. Main's real private Sigstore/MinIO/container
end-to-end tests are required to claim milestone-d integration.

Run focused tests with cached dependencies in the Linux development environment:

```sh
GOPROXY=off GOSUMDB=off go test -p 2 ./internal/finalizer ./cmd/finalizer
GOPROXY=off GOSUMDB=off go vet ./internal/finalizer ./cmd/finalizer
```
