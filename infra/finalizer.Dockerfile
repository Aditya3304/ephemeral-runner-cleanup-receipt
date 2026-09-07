# The build context is a curated directory of approved static binaries.
# No repository checkout, runner filesystem, Docker socket or package tools enter.
FROM scratch
COPY --chmod=0555 finalizer proofctl archivectl cosign /usr/local/bin/
USER 65532:65532
WORKDIR /tmp
ENTRYPOINT ["/usr/local/bin/finalizer"]
