# Local evidence archive

The archive runs real, free community MinIO and KES on this laptop. It uses a
private TLS CA, S3 versioning, default seven-day **COMPLIANCE** object retention,
and default SSE-KMS encryption with the persistent local `archive-key`. It does
not use AWS, public KMS, or runtime internet. No automatic object expiration or
volume deletion is configured.

## Setup and operation

Run from the repository in WSL; the Windows repository is available at the same
path under `/mnt/c`.

```sh
bash scripts/archive-prepare.sh  # setup downloads: pinned images and Go dependencies
bash scripts/archive-up.sh       # offline start and idempotent provisioning
bash scripts/archive-test.sh     # offline builds and real storage/permission checks
bash scripts/archive-stop.sh     # stops MinIO/KES; preserves every volume
```

The main `dev archive-check` command calls `archive-test.sh`. Root Go dependency:
`github.com/minio/minio-go/v7 v7.3.0`, with its dependencies recorded in the root
`go.mod`/`go.sum`. The archive has no nested module. Runtime startup uses
`pull_policy: never`; checks set `GOPROXY=off` and `GOSUMDB=off` after setup.

MinIO is pinned to `RELEASE.2025-04-22T22-12-26Z`, KES to
`2025-03-12T09-35-18Z`, and mc to `RELEASE.2025-04-16T18-13-26Z` by image digest.
The existing PostgreSQL image is also pinned by digest and is used only as a
utility image for OpenSSL and executable checks. The archive scripts never
start or configure a database.

## Isolation and integration mounts

Compose project: `cleanup-receipt-archive`. Docker network:
`cleanup-receipt-archive`, marked **internal**, with no published host ports.
The TLS endpoint is `https://minio:9000`; the KES endpoint is internal TLS with
mutual certificate authentication. A finalizer joins this network and the
signing network. It does not need the database network.

| Volume | Intended mount and access |
|---|---|
| `cleanup-receipt-archive_finalizer` | Finalizer only: `/run/archive:ro`; config `/run/archive/config.json` |
| `cleanup-receipt-archive_verifier` | Independent verifier only: `/run/archive-verifier:ro` |
| `cleanup-receipt-archive_public` | `/run/archive-public:ro`; public CA and `/run/archive-public/verifier.json`; no credentials |
| `cleanup-receipt-archive_admin` | Provisioning and MinIO administrator identity; never mounted into finalizer/verifier |
| `cleanup-receipt-archive_tls` | MinIO server TLS and MinIO-to-KES client identity |
| `cleanup-receipt-archive_kes_config` | KES TLS and its configuration |
| `cleanup-receipt-archive_kms_keys` | Persistent KES filesystem key store |
| `cleanup-receipt-archive_data` | Persistent encrypted MinIO data and metadata |

Finalizer/verifier private directories are mode `0700`; credential/config files
are `0600`, owned by **65532:65532**. Public files are `0644`. Configuration in
each private volume uses relative CA/credential paths, so the finalizer may
instead mount its volume at `/secrets/archive` and load
`/secrets/archive/config.json`. The public verifier config uses explicit
`/run/archive-verifier/access-key` and `/run/archive-verifier/secret-key` paths.
Mount it together with the verifier volume, or supply an operator configuration
with the intended file paths. An independent object verifier needs the archive
network; a signature-only offline verifier can run with no network.

Generated passwords, private keys and CA signing keys stay in Docker volumes.
The bootstrap preserves existing identities and refuses partial initialization.
Keep **both** archive data and KMS key volumes when moving/backing up this demo.
The scripts never remove volumes, regenerate an existing identity, or modify
the existing PostgreSQL service.

The finalizer may upload and read only `receipt-archive/evidence/sha256/*` and
extend COMPLIANCE retention for that prefix. It has no bucket listing,
administration, delete, governance-bypass, or bucket-configuration permission.
The verifier has only `GetObjectVersion` and `GetObjectRetention` permissions,
plus an explicit denial for reads with an empty version ID; the application
always supplies exact versions. The pinned server exposes a missing version ID
as an empty string in `s3:versionid`, so the policy uses `StringEquals: ""`
rather than `Null`. Forced unversioned byte reads, uploads and retention changes
are denied. Administrator
credentials are separate. Tests mount administrator credentials only into the
dedicated test container to prove COMPLIANCE deletion is rejected even for it.

Steady services are capped at 768 MiB/0.5 CPU for MinIO and 128 MiB/0.5 CPU for
KES. Utility services are capped at 128 MiB; the test container at 256 MiB.
Every service has bounded process counts and rotated logs. No image/package
pulls are performed by startup, application code, or tests.

## Go and evidence reference contract

```go
cfg, err := archive.LoadConfig("/run/archive/config.json")
client, err := archive.New(cfg)
ref, err := client.Put(ctx, "job.log", data)
err = client.Protect(ctx, ref) // exact saved version, new/resumed admission
err = client.Verify(ctx, ref)  // historical read-only verification
data, err = client.Read(ctx, ref)
```

`archive.Ref` has exactly the approved database fields:

```json
{"store":"local-minio","bucket":"receipt-archive","key":"evidence/sha256/<64 lowercase hex>","version_id":"<exact S3 version>","sha256":"<64 lowercase hex>","size_bytes":123}
```

`Ref.Validate()` checks bounded shape; every client operation also binds store,
bucket, prefix, digest key and size to operator configuration. A reference never
selects an endpoint, credentials, or a caller-controlled URL. This shape maps
directly to proof/finalizer evidence and the approved database contract. Content
type belongs only to object metadata: `.json` uploads use `application/json`,
`.log` uploads use `text/plain`, others use `application/octet-stream`.

Config fields are `store`, `endpoint`, `bucket`, `prefix`, `region`, `ca_file`,
`access_key_file`, `secret_key_file`, `kms_key`, and `max_object_bytes` (at most
104857600). HTTPS and a valid configured CA are mandatory. The client ignores
ambient proxy/credential discovery and uses a configured region and path-style
requests. Config and credential inputs must be bounded regular files; symlinks
and FIFOs are rejected on Linux. Application callers supply cancellation/deadline
contexts; `archivectl` uses a two-minute deadline.

Keys contain the SHA-256 of the exact bytes. `Put` uses `If-None-Match: *` and a
single-part upload, then reads the exact version back and checks bytes, size,
digest, SSE-KMS key ID and retention. An identical retry recovers the same
version. `Put` also calls `Protect`, which extends the exact version to at least
seven days from admission (a one-minute margin covers timestamp precision).
Retained COMPLIANCE protection can be extended but cannot be shortened.

Before first publication after an interrupted finalization, call `Protect` on
every saved reference, including receipt and bundle. This renews protection
without reselecting the latest version. Historical completed verification uses
`Verify`, which checks the original retention duration even after it naturally
expires and requires no write permission.

S3 versioning permits another version at the same key. It cannot change an
already referenced retained version. A deliberately corrupted latest version
causes `Put` to fail verification; it is never silently accepted as a successful
retry. Consumers always retain and request the signed version ID.

CLI: `archivectl put --config FILE --file FILE --name job.log` writes the reference
JSON to stdout. `archivectl verify --config FILE --reference FILE` checks it.
`archivectl read --config FILE --reference FILE` emits bytes only after complete
verification. Input bytes and readbacks are bounded to the configured limit.

## Validation and limits

On September 7, 2026, real Docker/MinIO tests passed for upload/readback, SHA-256,
size, exact version, SSE-KMS, seven-day COMPLIANCE, identical conditional retry,
tampered references, version-targeted overwrite rejection, exact-version deletion
rejection for both service roles **and admin**, deletion-marker rejection,
out-of-prefix uploads, forbidden bucket listings, and retention shortening.
The simulated-eight-days-later admission test renews a real stored version and
checks that its version ID and digest remain unchanged. This is a clock-driven
regression test, not a claim that the suite waited eight days.

Independent CLI probes upload as UID 65532 with only finalizer credentials and
verify as UID 65532 using only public config plus verifier credentials. An
archive/KES restart followed by identical-content upload and readback passed,
demonstrating that the original encryption key and stored version persisted.
The final complete test run passed, including a forced unversioned
`GetObject`/`io.ReadAll` returning `AccessDenied`. Transient raw test output is
saved under ignored `infra/archive/work/final-validation.log`. An
untrusted system CA cannot connect to the archive; external TCP connectivity is
blocked from the test container. The suite does not prove the separate runner
network policies or the full receipt/signature workflow; those have their own
integration checks. Test objects remain retained; no cleanup deletes them. The
future-clock renewal probe retains its tiny object for about fifteen days.

KES's filesystem backend is an upstream local-development option, not a hardware
security module. This proves local storage-service enforcement, not protection
against the laptop owner deleting disks/volumes/keys or independent off-machine
durability. Disk/volume quotas and automatic destructive cleanup are not enabled.

## Primary upstream references

- [MinIO object retention](https://github.com/minio/minio/blob/RELEASE.2025-04-22T22-12-26Z/docs/bucket/retention/README.md): COMPLIANCE and version semantics.
- [MinIO KMS configuration](https://github.com/minio/minio/blob/RELEASE.2025-04-22T22-12-26Z/internal/kms/config.go): local KES endpoint, certificate and CA settings.
- [KES release configuration](https://github.com/minio/kes/blob/2025-03-12T09-35-18Z/server-config.yaml): mTLS identities, scoped key operations and persistent development filesystem backend.
- [minio-go v7.3.0 API](https://github.com/minio/minio-go/blob/v7.3.0/docs/API.md): versioned reads, conditional upload and retention API.
- [MinIO condition values](https://github.com/minio/minio/blob/RELEASE.2025-04-22T22-12-26Z/cmd/bucket-policy.go): `versionid` condition population used by the verifier deny rule.

The pinned KES release did not create the configured key during the tested
startup. Provisioning therefore explicitly and idempotently creates
`archive-key` through the authenticated administrator API before enabling
bucket encryption. MinIO's KES identity can create/generate/decrypt that single
key, and cannot delete it; application roles never receive KES credentials.
