# Signature fixtures (test trust only)

`artifact.txt` and `bundle.json` are unmodified test data from
[sigstore-conformance](https://github.com/sigstore/sigstore-conformance/tree/9a5d9e9c5171eb56df7821e342eda7122f764b43/test/assets/bundle-verify),
specifically `a.txt` and `happy-path-v0.3/bundle.sigstore.json`.

`trusted-root.json` is the public-good test trust material from
[Cosign v3.1.3](https://github.com/sigstore/cosign/blob/v3.1.3/pkg/cosign/testdata/trusted_root_pgi.json).

These fixtures verify the real Cosign integration offline. They are not cleanup
receipts and do not authenticate this project's finalizer. Their publicly issued
test signing identity must never be added to the application's allowed identities.
Milestone d will provision separate private Sigstore services and operator trust.

The source URLs are pinned in `scripts/fetch-verification-fixtures.sh`; downloading
is a setup operation, not part of any test or runtime invocation.
