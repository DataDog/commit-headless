FROM scratch

# Populated by buildx with the target platform and architecture
ARG TARGETOS
ARG TARGETARCH

# The context has two copies of commit-headless: commit-headless-linux-amd64 and
# commit-headless-linux-arm64
# We only need to copy the architecture appropriate one in-place.
COPY commit-headless-${TARGETOS}-${TARGETARCH} /commit-headless

ENTRYPOINT ["/commit-headless"]
