FROM node:24.20.0-bookworm-slim@sha256:ba849c60be29959425b8734d57b8b4b7d56f98edd9504c9af091d5281095a71e
COPY --chmod=0555 .build/proofctl /usr/local/bin/proofctl
COPY action/dist/ /opt/guard/dist/
COPY examples/ /opt/examples/
USER 1000:1000
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/node", "/opt/guard/dist/local.js"]
