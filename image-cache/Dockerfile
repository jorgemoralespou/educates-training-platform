# syntax=docker/dockerfile:1

# zot is a self-contained Go binary, so the image needs no distribution
# userland. It is built as a static PIE that still names the glibc dynamic
# loader as its interpreter, so it needs the distroless base with glibc
# rather than the fully static one. That base also supplies the public CA
# roots zot needs to pull from upstream registries over TLS, tzdata, and a
# passwd file. The pod spec sets runAsUser explicitly, so the image's default
# user does not matter.

ARG TARGETARCH
ARG ZOT_VERSION=2.1.22

# ADD --checksum verifies the download and fails the build on mismatch. One
# stage per architecture carries its own checksum; the FROM below selects
# the stage matching the target platform.

FROM scratch AS zot-amd64
ARG ZOT_VERSION
ADD --checksum=sha256:33b9b5bfe05d6788c076de951a01920477dbd6395c330146e23cc2340073e52e --chmod=755 \
    https://github.com/project-zot/zot/releases/download/v${ZOT_VERSION}/zot-linux-amd64 /zot

FROM scratch AS zot-arm64
ARG ZOT_VERSION
ADD --checksum=sha256:abc9bab13c144a4ee2f9067a6bbbe70ca1c018c49dbf521db28975514ea705da --chmod=755 \
    https://github.com/project-zot/zot/releases/download/v${ZOT_VERSION}/zot-linux-arm64 /zot

FROM zot-${TARGETARCH} AS zot

FROM gcr.io/distroless/base-debian13:nonroot

COPY --from=zot /zot /usr/bin/zot

ENTRYPOINT ["/usr/bin/zot"]

EXPOSE 5000

VOLUME ["/var/lib/registry"]

CMD ["serve", "/etc/zot/config.yaml"]
