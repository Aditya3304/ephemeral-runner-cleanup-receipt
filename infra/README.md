# Local infrastructure

Milestone a implements PostgreSQL 17.11, private networking, generated local TLS,
separate administrative and API credentials, and durable named volumes.

Milestone b adds a dedicated kind 0.32.0 / Kubernetes 1.36.1 test cluster with its
own kubeconfig.

Milestone c adds restricted CI Jobs, a network policy controller, protected staging
metadata, and a startup gate that installs IPv4/IPv6 filtering inside each runner's
pinned network namespace. The helper is trusted node infrastructure and is never
mounted into a job. See docs/local-ci.md for the boundary and tested limitations.

MinIO, private Sigstore, API and dashboard services follow in
their approved milestones. No AWS resources, hosted workflows or paid services
are provisioned. Bootstrap downloads are permitted; runtime uses preloaded assets.
