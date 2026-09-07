# Private Sigstore on this laptop

This is a real local keyless stack: authenticated local OIDC → Fulcio certificate
with CT SCT → ephemeral Cosign artifact signature → Rekor inclusion proof and
checkpoint plus a local RFC3161 timestamp. It does not impersonate GitHub identity.
The service keys are persistent infrastructure keys; they are not substitutes for
the ephemeral artifact-signing key.

## Start and verify

From the repository under Ubuntu WSL:

```sh
bash scripts/sigstore-prepare.sh # setup downloads; builds the local authenticated issuer
bash scripts/sigstore-up.sh      # offline bootstrap/start; preserves existing keys
bash scripts/sigstore-check.sh   # real signing, negative checks, network-none verify
python3 scripts/sigstore-isolation.py # actual guarded runner vs live service IPs
```

The independently pinned Cosign 3.1.3 binary must already exist at `.build/cosign`
(prepared by `scripts/prepare-cli.sh`). Bootstrap and startup verify its SHA-256.
No `latest` images or runtime downloads. No public TUF, identity, log, timestamp,
registry, or cloud services. No host ports. The signing network is Docker
`internal: true`; verification additionally runs in a fresh `--network none`
container with `--offline` and explicit operator trust.

## Integration contract

Compose is a separate project `cleanup-receipt-sigstore`. Attach the trusted
finalizer to the external Docker network **cleanup-receipt-sigstore_sigstore**, alongside
its archive network. Do not attach disposable runners or give the finalizer DB
access. The local issuer runs as UID 65532 with explicit private certificate and key paths.

| External volume | Finalizer mount | Contents |
| --- | --- | --- |
| `cleanup-receipt-sigstore_finalizer-identity` | `/run/finalizer:ro` | `client.crt`, `client.key`, `issuer-ca.crt` (also `ca.crt`) |
| `cleanup-receipt-sigstore_sigstore-trust` | `/run/trust:ro` | `trusted-root.json`, `signing-config.json`, public CA/key PEM files |

Services run as UID/GID 65532. Credential directories are mode 0700, private files
0600. Use this UID for the finalizer or deliberately provision a restricted group;
do not make credentials world-readable. Only bootstrap mounts all secret volumes.
Only the finalizer (and the explicit smoke-test client) receives its mTLS key.

Request a token using POST `https://issuer:8443/token` with the client certificate
and key; verify the server using `ca.crt`. Response is JSON: extract `id_token`.
The client certificate URI is `spiffe://cleanup-receipt.local/finalizer`; issuer
tokens have audience `sigstore` and verified email `finalizer@cleanup-receipt.local`.
Fulcio's configuration allows only that issuer and checks `email_verified`.

```sh
cosign sign-blob --yes --oidc-disable-ambient-providers \
  --identity-token /private/token-file \
  --signing-config /run/trust/signing-config.json \
  --trusted-root /run/trust/trusted-root.json \
  --bundle receipt.sigstore.json receipt.json

cosign verify-blob --offline \
  --trusted-root /run/trust/trusted-root.json --bundle receipt.sigstore.json \
  --certificate-identity finalizer@cleanup-receipt.local \
  --certificate-oidc-issuer https://issuer:8443 receipt.json
```

Do not add `--key`, ignore-SCT/ignore-tlog options, public defaults, or trust supplied
by an uploaded receipt. Signing config uses Rekor API **2** and TSA API **1**, with
`ALL` selectors. Trusted root uses **origin=rekor** matching server `--hostname`.
The CT entry deliberately omits `origin`: SCT verification uses the CT public-key
hash, distinct from Rekor v2 signed-note identity. TSA endpoint includes
`/api/v1/timestamp`. Fulcio trusts the issuer TLS CA through its `ca-cert` config.

Pinned-version details discovered and corrected during actual startup:

- TesseraCT's private key must be **SEC1** (`BEGIN EC PRIVATE KEY`), not PKCS8.
  Its origin is `ctlog`; `ctlog:6962` is rejected as containing a URL scheme.
- Fulcio requires `fileca-key-passwd` to be explicitly set. Its encrypted PKCS8
  key password is in the private `/run/fulcio/server.yaml` server config, never
  Compose, process arguments, or the checkout.
- TSA needs explicit `--host=0.0.0.0 --port=3000` and
  **`--disable-ntp-monitoring`**. Otherwise its default NTP monitor tries public
  time servers. This local deployment trusts the WSL/host clock.
- The issuer's `/token` response is JSON, not a raw token; extract `id_token`.

## Persistence and resource assumptions

Both logs use separate named Docker volumes on Linux storage; neither uses
`emptyDir`, the Windows checkout, or an application PostgreSQL database. Keep the
volume contents and keys together. Bootstrap refuses partial state and never
silently rotates keys. Preserve old public trust when intentionally rotating.
Admin TLS/client/TSA CA keys stay in the admin volume, which runtime services do
not mount. Leaf TLS/client/TSA certificates last 365 days; CA certificates 3650
days. Renewal is an explicit future operator action, not automatic regeneration.

Initial observed laptop baseline: WSL 7638 MiB total, 5141 MiB available, kind
1.107 GiB, PostgreSQL 36.86 MiB. Configured ceilings are issuer 128 MiB and four
upstream services 256 MiB each (1152 MiB total); transient client 512 MiB and
bootstrap 256 MiB. These ceilings are initial low-throughput demo budgets, not
upstream memory guarantees. Go services use two execution threads and a 192 MiB
soft GC limit. One active signer/runner; build sequentially with `-p 2`.

Measured after the first successful signing run: Fulcio 12.47 MiB, issuer
4.547 MiB, Rekor 7.387 MiB, CT 5.602 MiB, TSA 9.238 MiB, approximately **39.2 MiB**
combined in `docker stats`. This is an idle/one-receipt observation, not peak
memory or a load-test result. Disk cache and host memory are additional.

## Live validation, September 7, 2026 (local time)

All five services started on the private network. An unauthenticated `/token`
request was rejected. The authenticated local mTLS issuer authenticated the finalizer and
supplied a signed RS256 token. Cosign 3.1.3 generated an ephemeral key and emitted
a genuine `application/vnd.dev.sigstore.bundle.v0.3+json` bundle containing a
Fulcio certificate, one Rekor `hashedrekord` **0.0.2** entry with an inclusion
proof and signed `rekor` checkpoint, and one RFC3161 timestamp. Cosign verified
the bundle with the exact private identity and issuer. Artifact tampering was
rejected. A separate fresh container with `--network none` also printed
`Verified OK`; no ambient trust cache or online log was required.

A second genuine signing run after restart also passed, including rejection of
the wrong signer email and the wrong OIDC issuer, followed by another successful
verification in a fresh network-disabled container.

All five services were restarted; bootstrap preserved the existing keys and
trust, all readiness checks passed, and the original bundle still verified.
The Docker network's inspected `Internal` value was `true`.

Live service isolation also passed through the existing coordinator's verified
runner network gate, run **64dc4deeecc502dcc58dbd490927b8d6**. The runner could
access its authorized Kubernetes API, but TCP connections to the live issuer
(8443), Fulcio (5555/8081), CT (6962), Rekor (3000/3001), and TSA (3000) IPs were
all blocked. All seven listeners were separately confirmed reachable from an
authorized private-network client, so denial was not inferred from an inactive
service. The runner lacked signing-key and Docker-socket mounts, exited zero,
and both its runner and resource namespaces were removed. All five services
had empty host `PortBindings`; the kind node was not attached to the signing
network. The saved report is `.build/sigstore/service-isolation.json`.

First successful bundle SHA-256:
`dcdc64bdd5ccf6612bb20c4d28a7d646ce58f3231562ebb49cb1af229f9850f3`.
Independent operator trusted-root SHA-256:
`fbbd3e48e79bc80e3dad572fb9865289a0654a7de4a67edca09fdd79369afb70`.
Smoke-test artifacts live under ignored `.build/sigstore/` and are overwritten
by subsequent smoke tests. They are infrastructure test artifacts, not finalized
cleanup receipts or proof of archive/finalizer milestone completion.

## Primary upstream sources and chosen pins

Deployment baseline is immutable Sigstore scaffolding commit
`a8069d1650370b9d341931b695008b866376bc01`. The four complete image digest pins
are in `compose.sigstore.yaml`, copied from these upstream service manifests:

- [Fulcio 1.8.5](https://github.com/sigstore/scaffolding/blob/a8069d1650370b9d341931b695008b866376bc01/config/fulcio/fulcio/300-fulcio.yaml)
- [TesseraCT POSIX 0.1.1](https://github.com/sigstore/scaffolding/blob/a8069d1650370b9d341931b695008b866376bc01/config/ctlog/ctlog/300-ctlog.yaml)
- [Rekor POSIX 2.2.1](https://github.com/sigstore/scaffolding/blob/a8069d1650370b9d341931b695008b866376bc01/config/rekor-tiles/rekor-tiles/300-rekor.yaml)
- [Timestamp server 2.0.6](https://github.com/sigstore/scaffolding/blob/a8069d1650370b9d341931b695008b866376bc01/config/tsa/tsa/300-tsa.yaml)
- [Upstream local trust/bootstrap integration](https://github.com/sigstore/scaffolding/blob/a8069d1650370b9d341931b695008b866376bc01/actions/setup-sigstore-env/build-trusted-root.sh)
- [Rekor POSIX server implementation](https://github.com/sigstore/rekor-tiles/blob/v2.2.1/cmd/rekor-server/posix/app/serve.go)
- [CT POSIX flags and publication awaiter](https://github.com/transparency-dev/tesseract/blob/v0.1.1/cmd/tesseract/posix/main.go)
- [Fulcio issuer TLS trust and email configuration](https://github.com/sigstore/fulcio/blob/v1.8.5/pkg/config/config.go)
- [Cosign 3.1.3 signing config flags](https://github.com/sigstore/cosign/blob/v3.1.3/cmd/cosign/cli/options/signingconfig.go)
- [Cosign 3.1.3 trusted-root flags](https://github.com/sigstore/cosign/blob/v3.1.3/cmd/cosign/cli/options/trustedroot.go)
- [Cosign 3.1.3 bundle signing](https://github.com/sigstore/cosign/blob/v3.1.3/cmd/cosign/cli/sign/sign_blob.go)

We adapt the upstream service arguments to Compose without Knative, Kourier,
duplicate Fulcio replicas, or a TUF server. We replace upstream test identity
issuers with the authenticated local mTLS issuer, generate fresh private keys rather
than copying test keys, and persist CT data rather than using upstream emptyDir.
Rekor v2 avoids classic Trillian/MySQL; its POSIX driver needs local disk and no
cloud emulators. A separate Sunlight deployment is unnecessary here.
