FROM scratch
COPY api deliver gateway cosign /usr/local/bin/
USER 65532:65532
ENTRYPOINT []
CMD ["/usr/local/bin/api"]
