#syntax=docker/dockerfile:1.3-labs

FROM fedora:44

ARG TARGETARCH

RUN useradd -u 1001 -g 0 -M -d /opt/app-root/src default && \
    mkdir -p /opt/app-root/src && \
    chown -R 1001:0 /opt/app-root

WORKDIR /opt/app-root/src

RUN <<EOF
    set -eo pipefail
    VERSION=2.1.22
    CHECKSUM_amd64="33b9b5bfe05d6788c076de951a01920477dbd6395c330146e23cc2340073e52e"
    CHECKSUM_arm64="abc9bab13c144a4ee2f9067a6bbbe70ca1c018c49dbf521db28975514ea705da"
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
