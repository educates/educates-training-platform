#syntax=docker/dockerfile:1.3-labs

FROM kubernetesui/dashboard:v2.7.0 AS k8s-console

# The Kubernetes dashboard 2.7.0 image ships a backend binary built with
# Go 1.19 and vulnerable module versions, and the 2.x series is no longer
# maintained upstream (3.x/7.x is a multi-container architecture which
# cannot run as a single process inside the workshop container). Rebuild
# the same backend from source with a current Go toolchain and patched
# module versions; the frontend assets are unchanged and still come from
# the upstream image.
FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26 AS k8s-console-build

ARG TARGETARCH

# Pin the exact Go release: the golang:1.26 image can lag a patch behind,
# and the current patch carries stdlib security fixes the binary must have.
ENV GOTOOLCHAIN=go1.26.9

RUN <<EOF
    set -eo pipefail
    DASHBOARD_VERSION=2.7.0
    DASHBOARD_COMMIT=42deb6b32a27296ac47d1f9839a68fab6053e5fc
    git clone --depth 1 --branch v${DASHBOARD_VERSION} https://github.com/kubernetes/dashboard /dashboard
    cd /dashboard
    test "$(git rev-parse HEAD)" = "${DASHBOARD_COMMIT}"
    go get \
        github.com/docker/distribution@v2.8.2+incompatible \
        github.com/golang/glog@v1.2.4 \
        github.com/gorilla/websocket@v1.5.3 \
        github.com/moby/spdystream@v0.5.1 \
        golang.org/x/crypto@v0.58.0 \
        golang.org/x/net@v0.61.0 \
        golang.org/x/oauth2@v0.37.0 \
        golang.org/x/text@v0.43.0
    go mod tidy
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
        go build -trimpath -ldflags "-s -w" -o /dashboard-backend ./src/app/backend/
EOF

FROM fedora:44 AS system-base

RUN HOME=/root && \
    INSTALL_PKGS=" \
        bash-completion \
        buildah \
        cadaver \
        containerd.io \
        docker-ce \
        docker-ce-cli \
        docker-compose-plugin \
        findutils \
        fuse3 \
        gcc \
        gcc-c++ \
        gettext \
        git \
        glibc-langpack-en \
        httpd \
        httpd-devel \
        httpie \
        jq \
        less \
        make \
        nano \
        ncat \
        netcat \
        nodejs \
        openssh-server \
        perl-Digest-SHA \
        procps \
        python3 \
        python3-devel \
        python3-pip \
        python3-virtualenv \
        python3-wheel \
        redhat-rpm-config \
        siege \
        slirp4netns \
        skopeo \
        supervisor \
        sudo \
        tree \
        tmux \
        vim-enhanced \
        which \
        yarn \
        zlib-devel \
    " && \
    dnf install -y dnf-plugins-core && \
    dnf-3 config-manager --add-repo https://download.docker.com/linux/fedora/docker-ce.repo && \
    PUPPETEER_SKIP_CHROMIUM_DOWNLOAD=1 && \
    curl -fsSL https://rpm.nodesource.com/setup_24.x | bash - && \
    curl -sL https://dl.yarnpkg.com/rpm/yarn.repo | tee /etc/yum.repos.d/yarn.repo && \
    dnf install -y --setopt=tsflags=nodocs --setopt=nodesource-nodejs.module_hotfixes=1 $INSTALL_PKGS && \
    dnf clean -y --enablerepo='*' all && \
    sed -i.bak -e '1i auth requisite pam_deny.so' /etc/pam.d/su && \
    sed -i.bak -e 's/^%wheel/# %wheel/' /etc/sudoers && \
    groupadd -g 2375 docker-sock && \
    useradd -u 1001 -g 0 -M -G docker-sock -d /home/eduk8s eduk8s && \
    mkdir -p /home/eduk8s && \
    chown -R 1001:0 /home/eduk8s && \
    chmod -R g=u /home/eduk8s && \
    chmod g+w /etc/passwd && \
    chown 1001:0 /opt && \
    ln -s /var/run/docker/docker.sock /var/run/docker.sock

# Node 24 bundles an npm whose vendored tar lags the security fixes;
# newer Node lines already bundle npm 11.19+, which carries the fixed
# tar. Take the newest npm 11.x so rebuilds keep absorbing 11.x fixes.
RUN npm install -g npm@11 && \
    npm --version && \
    rm -rf /root/.npm

FROM system-base AS vscode-helper

COPY opt/helper /opt/helper

WORKDIR /opt/helper

RUN npm install && \
    npm run vsce-package

FROM golang:1.26 as builder-image

# Pin the exact Go release: the golang:1.26 image can lag a patch behind,
# and the current patch carries stdlib security fixes the binary must have.
ENV GOTOOLCHAIN=go1.26.9

WORKDIR /app

# git-serve 0.0.5 is the last release its author published, in 2021, so its
# dependencies are pinned at versions with known vulnerabilities and no newer
# release will pick up fixes. Bump the flagged modules before building, the
# same approach the Kubernetes dashboard backend above uses. The release
# tarball is still checksum pinned, so only the dependency graph moves.
RUN curl --silent --fail -L -o /tmp/git-serve.tar.gz https://github.com/cirocosta/git-serve/archive/refs/tags/v0.0.5.tar.gz && \
echo "09cd14a34f17d88cd4f0d2b73e0bbd0bf56984be21bc947f416a7824a709011e /tmp/git-serve.tar.gz" | sha256sum --check --status && \
    tar xvf /tmp/git-serve.tar.gz && \
    cd git-serve-0.0.5 && \
    go get \
        github.com/prometheus/client_golang@v1.23.2 \
        github.com/sirupsen/logrus@v1.9.4 \
        golang.org/x/crypto@v0.58.0 \
        golang.org/x/net@v0.61.0 && \
    go mod tidy && \
    go build -o git-serve cmd/git-serve/main.go

# The bundled vendir, k9s and kustomize releases each carry critical
# vulnerabilities in their dependency graphs or Go toolchains, and each is
# already the newest release upstream has published, so there is nothing to
# upgrade to. Rebuild them from their pinned release sources with a fixed Go
# toolchain and patched dependencies instead, the same approach used for the
# dashboard backend and git-serve above. GOTOOLCHAIN pins the exact Go
# release, so the build does not depend on how fresh the golang base image
# happens to be.
FROM --platform=$BUILDPLATFORM golang:1.26 AS vendir-build

ARG TARGETOS
ARG TARGETARCH

ENV GOTOOLCHAIN=go1.26.9 CGO_ENABLED=0

RUN <<EOF
    set -eo pipefail
    VENDIR_VERSION=0.46.2
    VENDIR_COMMIT=58acd4c5cad5ea379c27e5b6011bdff2feb43ab3
    git clone --depth 1 --branch v${VENDIR_VERSION} https://github.com/carvel-dev/vendir /build
    cd /build
    test "$(git rev-parse HEAD)" = "${VENDIR_COMMIT}"
    go get golang.org/x/crypto@v0.58.0 golang.org/x/net@v0.61.0
    go mod tidy
    # vendir commits a vendor/ tree, which must be regenerated after the
    # dependency bumps or the build refuses to run with -mod=vendor.
    go mod vendor
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
        -ldflags "-s -w -X carvel.dev/vendir/pkg/vendir/version.Version=${VENDIR_VERSION}" \
        -o /vendir ./cmd/vendir/...
EOF

FROM --platform=$BUILDPLATFORM golang:1.26 AS k9s-build

ARG TARGETOS
ARG TARGETARCH

ENV GOTOOLCHAIN=go1.26.9 CGO_ENABLED=0

RUN <<EOF
    set -eo pipefail
    K9S_VERSION=0.51.0
    K9S_COMMIT=558caafe7ba067467de46b320cc22ef11fef9c34
    git clone --depth 1 --branch v${K9S_VERSION} https://github.com/derailed/k9s /build
    cd /build
    test "$(git rev-parse HEAD)" = "${K9S_COMMIT}"
    go get \
        github.com/aws/aws-sdk-go-v2/service/s3@v1.97.3 \
        github.com/containerd/containerd@v1.7.36 \
        github.com/containerd/containerd/v2@v2.2.9 \
        github.com/go-git/go-git/v5@v5.19.3 \
        go.opentelemetry.io/otel/sdk@v1.45.0 \
        golang.org/x/crypto@v0.58.0 \
        golang.org/x/net@v0.61.0 \
        google.golang.org/grpc@v1.83.2 \
        oras.land/oras-go/v2@v2.6.2
    go mod tidy
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
        -ldflags "-s -w -X github.com/derailed/k9s/cmd.version=v${K9S_VERSION}" \
        -o /k9s main.go
EOF

FROM --platform=$BUILDPLATFORM golang:1.27 AS kustomize-build

ARG TARGETOS
ARG TARGETARCH

# kustomize 5.8.3 requires Go 1.27, so this stage uses a newer toolchain
# than the other source rebuilds.
ENV GOTOOLCHAIN=go1.27.2 CGO_ENABLED=0

RUN <<EOF
    set -eo pipefail
    KUSTOMIZE_VERSION=5.8.3
    KUSTOMIZE_COMMIT=6e4452f7b136fb9a261afe60830867a875183a71
    git clone --depth 1 --branch kustomize/v${KUSTOMIZE_VERSION} https://github.com/kubernetes-sigs/kustomize /build
    cd /build
    test "$(git rev-parse HEAD)" = "${KUSTOMIZE_COMMIT}"
    cd kustomize
    go get golang.org/x/text@v0.43.0
    go mod tidy
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
        -ldflags "-s -w -X sigs.k8s.io/kustomize/api/provenance.version=v${KUSTOMIZE_VERSION}" \
        -o /kustomize .
EOF

FROM system-base AS scratch-image

ARG TARGETARCH

# Kubernetes web console. Frontend assets come from the upstream image;
# the backend binary is the patched rebuild from the k8s-console-build
# stage.

COPY --from=k8s-console / /opt/console/
COPY --from=k8s-console-build /dashboard-backend /opt/console/dashboard

# Miscellaneous tools.

RUN <<EOF
    YQ_VERSION=4.54.1
    CHECKSUM_amd64="8e34fc298390875de416e6a4afcb8cabeceb25d9aa8506c1a2f9353cf702ea5f"
    CHECKSUM_arm64="189088da0c6429ec5178dfaab1a114805f6cab0b61b165ab236efedf1d57a71b"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /usr/local/bin/yq https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_${TARGETARCH}
    echo "${!CHECKSUM} /usr/local/bin/yq" | sha256sum --check --status
    chmod +x /usr/local/bin/yq
EOF

RUN <<EOF
    HUGO_VERSION=0.167.0
    CHECKSUM_amd64="4d84519b9f619e6d4c3fb45a50157abeabeb724f859c60605f44c23def6e1169"
    CHECKSUM_arm64="d0dd0d1ed249842525a93413e10ca1ef6f3b63311ee1695b16a0ce488142cec1"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/hugo.tar.gz https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_${HUGO_VERSION}_linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/hugo.tar.gz" | sha256sum --check --status
    cd /usr/local/bin
    tar -zxf /tmp/hugo.tar.gz hugo
    rm /tmp/hugo.tar.gz
EOF

RUN <<EOF
    UV_VERSION=0.12.24
    ARCHNAME_amd64=x86_64
    ARCHNAME_arm64=aarch64
    ARCHNAME=ARCHNAME_${TARGETARCH}
    CHECKSUM_amd64="b4dfaef47d491a7296981f8374a4595f55dbf84e8937c8ecd2983574d8bb3da6"
    CHECKSUM_arm64="5231be65f496304623895dacdbf1de8504fec90303684bdf05805aa34414dd21"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/uv.tar.gz https://github.com/astral-sh/uv/releases/download/${UV_VERSION}/uv-${!ARCHNAME}-unknown-linux-gnu.tar.gz
    echo "${!CHECKSUM} /tmp/uv.tar.gz" | sha256sum --check --status
    tar -C /usr/local/bin --strip-components 1 -xf /tmp/uv.tar.gz uv-${!ARCHNAME}-unknown-linux-gnu/uv uv-${!ARCHNAME}-unknown-linux-gnu/uvx
    rm -f /tmp/uv.tar.gz
EOF

# Kubernetes tools.

RUN mkdir -p /opt/kubernetes/bin

RUN <<EOF
    KUBECTL_VERSION=1.33.13
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="7365f43aa89d666d66840935756a32978320eb9a1c92e8c07d6f8bbd76fccfa7e5f18a2393065dd242057f93750823255bed5be8f4eab3729af57cda578096e7"
    CHECKSUM_arm64="634f8243fc33dac418eebba24399b89e697f4762cf6c3fce2bb4700c50032e65d4ed64c70d9ad47f39dd49bab99ae5b7a7972026821a2b4a93b24af929581bc1"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 3 -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz kubernetes/client/bin/kubectl
    mv /opt/kubernetes/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
EOF

RUN <<EOF
    KUBECTL_VERSION=1.34.12
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="46596080fe335de00bf3eab6fb02ec737a75133bfdabd9191381fc3a077c6e4eb3eeff8c434f6f8cc0ec3690e22a19410a1c875a32f83f5091e96b860c5f6f40"
    CHECKSUM_arm64="c14f35d82bd5606da121dc4fe76693763941eb2b011738d05663d436ed96de4b04f4d7f51383c77e513b67623aae2ae23e2717eb95d1460e264b72f0ef87c7f9"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 3 -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz kubernetes/client/bin/kubectl
    mv /opt/kubernetes/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
EOF

RUN <<EOF
    KUBECTL_VERSION=1.35.9
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="4c2fca25cd725d25ff48ad06c8ebc167d1cca6cfe166d7e1fc5c6b4480c50c7a3afc1145c8acb5b8aa79177f7afb9b446e977aa3f921198a957cebe2ef02cc43"
    CHECKSUM_arm64="6671c3caf7ac053f764282eea3733d6378fc18f9877cb95f8866aa38532462f0db33c173990dbac64a39881bcf12dda5fa9c9af5fbcd6d47b1169887e1b0b91a"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 3 -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz kubernetes/client/bin/kubectl
    mv /opt/kubernetes/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
EOF

RUN <<EOF
    KUBECTL_VERSION=1.36.5
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="952e177737c5de7942c17478ad57694f3dd8d606f66e6198ec8e671effc93d1e5b2b0149d43a32e4eb241c15ea939182718442f533a783c0c915854e87587101"
    CHECKSUM_arm64="1227e62604f1349c8324956b28d3a86f0b7a74cb3870ba07625c4891a06449b539970b486e9069979fe93d17f1d4ea002d22ed23788e4eb9912906ce460ba87c"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 3 -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz kubernetes/client/bin/kubectl
    mv /opt/kubernetes/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
EOF

# k9s is the patched source rebuild from the k9s-build stage.
COPY --from=k9s-build /k9s /opt/kubernetes/bin/k9s

RUN <<EOF
    YTT_VERSION=0.55.3
    CHECKSUM_amd64="15751b45a819edbf22b3d3eadb5fa9a5a2599128d921660a874bd39c47bb41e1"
    CHECKSUM_arm64="fed073d52b780a88ce506e68c44f33cedede2dad3d5f4fbe07a2833e45d996ed"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/ytt https://github.com/carvel-dev/ytt/releases/download/v${YTT_VERSION}/ytt-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/ytt" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/ytt
EOF

RUN <<EOF
    IMGPKG_VERSION=0.48.3
    CHECKSUM_amd64="d177073901136b1e07741c9c7e6afdbdf8b4bcf32017c1e5bc8d184c8383ec69"
    CHECKSUM_arm64="655609325d9497096546342a4683688fc2a87a2952542b5da8105838285ec079"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/imgpkg https://github.com/carvel-dev/imgpkg/releases/download/v${IMGPKG_VERSION}/imgpkg-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/imgpkg" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/imgpkg
EOF

RUN <<EOF
    KBLD_VERSION=0.49.2
    CHECKSUM_amd64="8715db7932129a279b25060a9b90cc0eb9221d111d9924af996d6ed82de5b391"
    CHECKSUM_arm64="89c19adefa4c15799fb4d036dc747c7737091190df28ef277398037fbe8947f0"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/kbld https://github.com/carvel-dev/kbld/releases/download/v${KBLD_VERSION}/kbld-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/kbld" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/kbld
EOF

RUN <<EOF
    KAPP_VERSION=0.65.4
    CHECKSUM_amd64="bf7df0647d7645572d45185bb8bf6a40dd1e631ff4ee1522bc8ce56db53bd243"
    CHECKSUM_arm64="e41693ac3d69e1143c7e5fe870712ca05ed810e1e0bc5b00f77ca22ba0f18ae1"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/kapp https://github.com/carvel-dev/kapp/releases/download/v${KAPP_VERSION}/kapp-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/kapp" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/kapp
EOF

# vendir is the patched source rebuild from the vendir-build stage.
COPY --from=vendir-build /vendir /opt/kubernetes/bin/vendir

RUN <<EOF
    HELM_VERSION=4.3.0
    CHECKSUM_amd64="86584a54def73570558f66f5111cc53dfed56689637ae32c1201205d494f54fb"
    CHECKSUM_arm64="31c5794dd55c66a51e6b7d2e2ac7a114ae8b1de41ff1d9ba51748ac973b06a08"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/helm.tar.gz https://get.helm.sh/helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/helm.tar.gz" | sha256sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 1 -zxvf /tmp/helm.tar.gz linux-${TARGETARCH}/helm
    rm /tmp/helm.tar.gz
EOF

RUN <<EOF
    SKAFFOLD_VERSION=2.25.0
    CHECKSUM_amd64="42b9e2e3246c19b78fcc53dd60cc7ded1da8704293886b909a2a02b9bef34d20"
    CHECKSUM_arm64="416f5fed2009c2b4adc62ae887ee5fc19ff08cc8a0250b81d8682d55626a75fd"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/skaffold https://github.com/GoogleContainerTools/skaffold/releases/download/v${SKAFFOLD_VERSION}/skaffold-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/skaffold" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/skaffold
EOF

# kustomize is the patched source rebuild from the kustomize-build stage.
COPY --from=kustomize-build /kustomize /opt/kubernetes/bin/kustomize

# VS Code editor and dashboard extension.
# Make sure when updating that AI features are disabled and working https://github.com/coder/code-server/issues/7540
RUN <<EOF
    CODE_VERSION=4.141.0
    CHECKSUM_amd64="65bcd20b524d03eb1b5adb7f360244599ef0e5b74089a46a982d32c074b97a29"
    CHECKSUM_arm64="e5cc78da1d631c6cf6162f9b18eeaa8ee9d3239e1a88bb99d0602cdc5fa1852d"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    mkdir /opt/editor
    curl --silent --fail -L -o /tmp/code-server.tar.gz https://github.com/cdr/code-server/releases/download/v${CODE_VERSION}/code-server-${CODE_VERSION}-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/code-server.tar.gz" | sha256sum --check --status
    cd /opt/editor
    tar -zxf /tmp/code-server.tar.gz --strip-components=1
    rm /tmp/code-server.tar.gz
    # Delete server.key from editor test fixtures
    find /opt/editor/node_modules -type d  \( -name test -o -name tests -o -name __tests__ \) -prune -exec rm -rf {} +
EOF

COPY --from=vscode-helper --chown=1001:0 /opt/helper/educates-0.0.1.vsix /opt/eduk8s/educates-0.0.1.vsix

# Git server.

RUN mkdir /opt/git /opt/git/bin /opt/git/repositories

COPY --from=builder-image /app/git-serve-0.0.5/git-serve /opt/git/bin/git-serve

# Dashboard applications.

COPY opt/. /opt/

RUN mkdir -p /opt/slides/reveal.js/4.6.1 && \
    cd /opt/slides/reveal.js/4.6.1 && \
    curl -sL -o src.tar.gz https://github.com/hakimel/reveal.js/archive/4.6.1.tar.gz && \
    tar --strip-components 1 -xf src.tar.gz && \
    rm src.tar.gz && \
    mkdir -p /opt/slides/reveal.js/5.2.1 && \
    cd /opt/slides/reveal.js/5.2.1 && \
    curl -sL -o src.tar.gz https://github.com/hakimel/reveal.js/archive/5.2.1.tar.gz && \
    tar --strip-components 1 -xf src.tar.gz && \
    rm src.tar.gz && \
    mkdir -p /opt/slides/reveal.js/6.0.1 && \
    cd /opt/slides/reveal.js/6.0.1 && \
    curl -sL -o src.tar.gz https://github.com/hakimel/reveal.js/archive/6.0.1.tar.gz && \
    tar --strip-components 1 -xf src.tar.gz && \
    rm src.tar.gz && \
    mkdir -p /opt/slides/impress.js/1.1.0 && \
    cd /opt/slides/impress.js/1.1.0 && \
    curl -sL -o src.tar.gz https://github.com/impress/impress.js/archive/refs/tags/1.1.0.tar.gz && \
    tar --strip-components 1 -xf src.tar.gz && \
    rm src.tar.gz && \
    mkdir -p /opt/slides/impress.js/2.0.0 && \
    cd /opt/slides/impress.js/2.0.0 && \
    curl -sL -o src.tar.gz https://github.com/impress/impress.js/archive/refs/tags/v2.0.0.tar.gz && \
    tar --strip-components 1 -xf src.tar.gz && \
    rm src.tar.gz

RUN cd /opt/httpd && \
    virtualenv /opt/httpd && \
    source /opt/httpd/bin/activate && \
    pip install --no-cache-dir -r requirements.txt

RUN cd /opt/gateway && \
    npm install && \
    npm run compile && \
    npm prune --production && \
    npm cache clean --force && \
    find node_modules -type d  \( -name test -o -name tests -o -name __tests__ \) -prune -exec rm -rf {} +

RUN cd /opt/renderer && \
    npm install && \
    npm run compile && \
    npm prune --production && \
    npm cache clean --force && \
    find node_modules -type d  \( -name test -o -name tests -o -name __tests__ \) -prune -exec rm -rf {} +

FROM system-base

ARG TARGETARCH

COPY --from=scratch-image --chown=1001:0 /opt/. /opt/
COPY --from=scratch-image --chown=1001:0 /home/. /home/

COPY --from=scratch-image /usr/local/. /usr/local/

COPY usr/. /usr/
COPY etc/. /etc/

COPY --chown=1001:0 home/. /home/

RUN rm /etc/supervisord.conf && \
    ln -s /opt/eduk8s/etc/supervisord.conf /etc/supervisord.conf

RUN fix-permissions /home/eduk8s

ENV HOME=/home/eduk8s \
    PATH=/home/eduk8s/.local/bin:/home/eduk8s/bin:/opt/eduk8s/bin:/opt/kubernetes/bin:/opt/editor/bin:$PATH \
    PLATFORM_ARCH=${TARGETARCH}

ENV BASH_ENV=/opt/eduk8s/etc/profile \
    ENV=/opt/eduk8s/etc/profile \
    PROMPT_COMMAND=". /opt/eduk8s/etc/profile"

WORKDIR /home/eduk8s

USER 1001

ENTRYPOINT [ "container-entrypoint" ]

EXPOSE 10081

CMD [ "start-container" ]
