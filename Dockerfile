FROM scratch

COPY commit-headless /commit-headless

ENTRYPOINT ["/commit-headless"]
