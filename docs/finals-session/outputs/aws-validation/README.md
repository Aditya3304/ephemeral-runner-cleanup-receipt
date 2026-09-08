# Exported AWS validation evidence

Collected 8 September 2026. `verified-api-records.json` maps each receipt to its
exact S3 object version, SHA-256 digest, signature state and cleanup verdict.
Folders named `RUN-ATTEMPT` contain the original receipt and Sigstore bundle bytes.
The original four-receipt validation has now been extended with the controlled
order-export experiments. See `controlled-experiments.json` for the normal,
failed-application and residual-file cases. `offline-verification.json` lists
each actual signature and altered-byte check.

With the project's pinned Cosign v3.1.3 available as `cosign`, run from this folder:

```bash
cosign verify-blob --offline \
  --trusted-root trusted-root.json \
  --bundle 34200085381-2/receipt.bundle.json \
  --certificate-identity finalizer@cleanup-receipt.local \
  --certificate-oidc-issuer https://issuer:8443 \
  34200085381-2/receipt.json
```

Repeat with another folder to verify that execution. `offline-verification.json`
records the actual successful checks and rejection of altered bytes. The public
trust root SHA-256 is
`e8389b84f266cdc9d4e080dd398e1f8cf500bd81f485bcba3212e4dfb9c1bff3`.
Trust in that private signing infrastructure is an explicit deployment assumption;
a valid signature does not itself establish that a cleanup verdict is pass.

`aws-boundary-checks.json` contains real denied API calls and post-restart network
checks. `metadata-backup-restore.json` records a verified restore preceding the
fourth receipt. `host-restart.txt` records the successful host recovery. The
GitHub JSON snapshots independently identify the run, job, separate cleanup
status and empty runner registry. `s3-version-metadata.json` records the final
receipt/bundle's encryption and retention headers as observed after export.

These are saved observations, not a live audit of present AWS state. No credentials,
private signing keys or raw job logs are included. See
[the demo guide](../FINALS-DEMO.md) for execution, costs and teardown.
