# Completed live walkthrough

Executed against AWS and GitHub on 2026-09-08T11:13:33.622224+00:00.

| Scenario | GitHub result | Signed cleanup | Dashboard receipt | GitHub run |
| --- | --- | --- | --- | --- |
| normal | success | pass | [Open receipt](http://localhost:18080/?receipt=2f2c5918-a7ab-449d-8df9-405d83cadcd9) | [34215449903](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34215449903) |
| test-failure | failure | pass | [Open receipt](http://localhost:18080/?receipt=75e18991-52ae-4a9e-9d20-957d5102d7dd) | [34215613350](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34215613350) |
| cleanup-blocked | failure | fail | [Open receipt](http://localhost:18080/?receipt=5c0df43d-749e-4ca8-8b3a-9b4646de1c54) | [34215767448](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34215767448) |
| normal | success | pass | [Open receipt](http://localhost:18080/?receipt=f389b6b2-8080-4dc9-965f-b72b73a8d1cf) | [34216421814](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34216421814) |
| test-failure | failure | pass | [Open receipt](http://localhost:18080/?receipt=2cb0d908-a566-45f5-932b-025bd49c9e8b) | [34218897916](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34218897916) |
| cleanup-blocked | failure | fail | [Open receipt](http://localhost:18080/?receipt=719e2051-699f-460d-9918-ead78c301b82) | [34219047670](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34219047670) |
| normal | success | pass | [Open receipt](http://localhost:18080/?receipt=08e6009c-142e-4d91-bd2f-fd4cfc91a250) | [34219196086](https://github.com/Aditya3304/ephemeral-runner-cleanup-receipt/actions/runs/34219196086) |

Every listed case was newly dispatched and its synthetic files inspected inside the running pod. The API reverified each receipt and its archived artifacts. Exact S3 versions and observation hashes were exported. All signatures verified offline; altered receipt bytes were rejected. S3 confirmed SSE-KMS and Compliance Object Lock.

The blocked case retained a job-reported workspace failure; its signed archive separately binds the trusted collector’s independent residue finding.

See `runs.json` for exact IDs and receipt metadata, `verification.json` for verification results, each run folder for original bytes/S3 metadata, and the `*-live.json` files for live pod observations.

The final normal run follows a tested EC2 stop/start when `host-resume.txt` is present. The host and private tunnels are left available for rehearsal; stopping/teardown is an operator action, and locked evidence is retained.

No repository source, trust roots or historical receipts were rewritten for these runs. No credentials or raw job logs are exported in this packet.
