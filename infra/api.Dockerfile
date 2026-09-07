FROM scratch
COPY api deliver gateway recoverybridge cosign /usr/local/bin/
COPY ui /ui
USER 65532:65532
ENTRYPOINT []
CMD ["/usr/local/bin/api"]
