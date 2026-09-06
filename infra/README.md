# Local infrastructure

Milestone a implements PostgreSQL 17.11, private networking, generated local TLS,
separate administrative and API credentials, and durable named volumes.

Milestone b adds a dedicated kind 0.32.0 / Kubernetes 1.36.1 test cluster with its
own kubeconfig. This developer cluster is not yet the hardened job boundary.

MinIO, CI runner workloads, private Sigstore, API and dashboard services follow in
their approved milestones. No AWS resources, hosted workflows or paid services
are provisioned. Bootstrap downloads are permitted; runtime uses preloaded assets.
