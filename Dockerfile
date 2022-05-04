FROM scratch
COPY lock-exec /
ENTRYPOINT ["/lock-exec"]
