#syntax=docker/dockerfile:1.3-labs

FROM fedora:44

ARG TARGETARCH

RUN useradd -u 1001 -g 0 -M -d /opt/app-root/src default && \
    mkdir -p /opt/app-root/src && \
    chown -R 1001:0 /opt/app-root

WORKDIR /opt/app-root/src

RUN <<EOF
    set -eo pipefail
    VERSION=2.1.18
    CHECKSUM_amd64="a359a0af159db6758b24f8e26d6a244aae95caf3ff382c462875f42d9e082898"
    CHECKSUM_arm64="0504b0bd926a7ca9dab71055ba0e0bca8ae4bd67159ef4b8ee3855dcc38818c9"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    curl --silent --fail -L -o /usr/bin/zot https://github.com/project-zot/zot/releases/download/v${VERSION}/zot-linux-${TARGETARCH}
    echo "${!CHECKSUM}  /usr/bin/zot" | sha256sum --check --status
    chmod +x /usr/bin/zot
EOF

USER 1001

ENTRYPOINT ["/usr/bin/zot"]

EXPOSE 5000

VOLUME ["/var/lib/registry"]

CMD ["serve", "/etc/zot/config.yaml"]
