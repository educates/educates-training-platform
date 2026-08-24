#syntax=docker/dockerfile:1.3-labs

FROM fedora:44

ARG TARGETARCH

RUN useradd -u 1001 -g 0 -M -d /opt/app-root/src default && \
    mkdir -p /opt/app-root/src && \
    chown -R 1001:0 /opt/app-root

WORKDIR /opt/app-root/src

RUN <<EOF
    set -eo pipefail
    VERSION=2.1.20
    CHECKSUM_amd64="a32e42d042d1f17b5b1317e55cc1a415a744c873dcd05c25c56b665478258bcb"
    CHECKSUM_arm64="d6a39475587be18ec3d42e0d2bfa50f5c5064cbbcdc222dbd88e55ecf69dd8e9"
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
