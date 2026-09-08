# Finals session handoff — 8 September 2026

This archive preserves the session's walkthroughs, operator scripts and selected
public verification evidence. The actual application and AWS infrastructure are
in the repository root. Start with [the AWS guide](../aws-finals.md).

- [Live walkthrough](outputs/LIVE-DEMO-WALKTHROUGH.md)
- [Technical panel preparation](outputs/TECHNICAL-PANEL-PREP.md)
- [Controlled experiments](outputs/CONTROLLED-EXPERIMENTS.md)
- [Team responsibilities and stack](TEAM-SPLIT.md)
- [Observed error rates](OBSERVED-RATES.md)

## Reproduction and historical context

The running demo and operator helpers pin application commit
`9605c536dd49faeb3acf6a80a60ec1136ad604c0` on `aws-github-finals`.
This handoff is published separately so that branch's exact-revision checks keep
working. Publishing this archive does not deploy changes or run workflows.

Files under `work/` are historical operator helpers, including repair and
deployment commands. They are not a portable installation package: they refer
to the prepared laptop, its authenticated tools, the original session directory,
and the original AWS instance. Do not run the repair scripts as a setup sequence.
Use the repository's `scripts/aws/` tooling and AWS guide for a new deployment.
The archived one-command launcher assumes the original session layout and its
existing authentication, tunnels, bridge and pinned verifier binary.

Guides retain original local paths and time-specific state. Links to the original
checkout or non-exported artifacts may only work on the prepared laptop.

Selected JSON evidence is copied without changing its bytes. Signature bundles
and the public trust root are included; possession of a public root does not
independently establish trust in its operator. Observation and verification files
record earlier checks, not a fresh check performed by opening this archive.

Credentials, login caches, private keys, raw job logs, console transcripts,
downloaded binaries, Git bundles and transient command payloads are excluded.
Their omission means this archive is not a complete offline evidence export:
full verification of every signed reference requires the original S3 versions
or the private local export, including logs. Resource identifiers in public
receipts are retained to preserve signature validity.
