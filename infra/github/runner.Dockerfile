FROM node:24.20.0-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates libicu72 libssl3 libkrb5-3 zlib1g liblttng-ust1 libunwind8 && rm -rf /var/lib/apt/lists/*
COPY --chmod=0555 .build/proofctl /usr/local/bin/proofctl
COPY .build/actions-runner/ /opt/actions-runner/
COPY action/dist/ /opt/guard/dist/
COPY action/action.yml /opt/guard/action.yml
COPY infra/github/entrypoint.mjs /opt/github/entrypoint.mjs
COPY examples/ /opt/examples/
USER 1000:1000
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/node", "/opt/github/entrypoint.mjs"]
