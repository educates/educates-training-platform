#syntax=docker/dockerfile:1.3-labs

FROM fedora:42

ARG TARGETARCH

RUN useradd -u 1001 -g 0 -M -d /opt/app-root/src default && \
    mkdir -p /opt/app-root/src && \
    chown -R 1001:0 /opt/app-root

WORKDIR /opt/app-root/src

RUN <<EOF
    set -eo pipefail
    VERSION=1.4.3
    CHECKSUM_amd64="f43de78c3b072a18a1e5e8be1bcfe46d8cdf14a4dcf2f0ca5857b440df3e72d8"
    CHECKSUM_arm64="61371d5605a403326108d5384887dfbcdc27e5907ec264c801b48a63a7811dbb"
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
