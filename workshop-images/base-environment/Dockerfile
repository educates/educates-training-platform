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
ENV GOTOOLCHAIN=go1.26.6

RUN <<EOF
    set -eo pipefail
    DASHBOARD_VERSION=2.7.0
    DASHBOARD_COMMIT=42deb6b32a27296ac47d1f9839a68fab6053e5fc
    git clone --depth 1 --branch v${DASHBOARD_VERSION} https://github.com/kubernetes/dashboard /dashboard
    cd /dashboard
    test "$(git rev-parse HEAD)" = "${DASHBOARD_COMMIT}"
    go get \
        github.com/docker/distribution@v2.8.2+incompatible \
        github.com/moby/spdystream@v0.5.1 \
        golang.org/x/crypto@v0.54.0 \
        golang.org/x/net@v0.57.0 \
        golang.org/x/oauth2@v0.36.0 \
        golang.org/x/text@v0.40.0
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

FROM system-base AS vscode-helper

COPY opt/helper /opt/helper

WORKDIR /opt/helper

RUN npm install && \
    npm run vsce-package

FROM golang:1.26 as builder-image

# Pin the exact Go release: the golang:1.26 image can lag a patch behind,
# and the current patch carries stdlib security fixes the binary must have.
ENV GOTOOLCHAIN=go1.26.6

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
        github.com/sirupsen/logrus@v1.9.4 \
        golang.org/x/crypto@v0.54.0 \
        golang.org/x/net@v0.57.0 && \
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

ENV GOTOOLCHAIN=go1.26.6 CGO_ENABLED=0

RUN <<EOF
    set -eo pipefail
    VENDIR_VERSION=0.46.0
    VENDIR_COMMIT=a7fb189b2a1d1be30ccdf0049ae62b17d83240bc
    git clone --depth 1 --branch v${VENDIR_VERSION} https://github.com/carvel-dev/vendir /build
    cd /build
    test "$(git rev-parse HEAD)" = "${VENDIR_COMMIT}"
    go get golang.org/x/crypto@v0.54.0 golang.org/x/net@v0.57.0
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

ENV GOTOOLCHAIN=go1.26.6 CGO_ENABLED=0

RUN <<EOF
    set -eo pipefail
    K9S_VERSION=0.51.0
    K9S_COMMIT=558caafe7ba067467de46b320cc22ef11fef9c34
    git clone --depth 1 --branch v${K9S_VERSION} https://github.com/derailed/k9s /build
    cd /build
    test "$(git rev-parse HEAD)" = "${K9S_COMMIT}"
    go get \
        github.com/containerd/containerd@v1.7.33 \
        github.com/containerd/containerd/v2@v2.2.5 \
        github.com/go-git/go-git/v5@v5.19.2 \
        golang.org/x/crypto@v0.54.0 \
        golang.org/x/net@v0.57.0 \
        google.golang.org/grpc@v1.82.1 \
        oras.land/oras-go/v2@v2.6.2
    go mod tidy
    GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
        -ldflags "-s -w -X github.com/derailed/k9s/cmd.version=v${K9S_VERSION}" \
        -o /k9s main.go
EOF

FROM --platform=$BUILDPLATFORM golang:1.26 AS kustomize-build

ARG TARGETOS
ARG TARGETARCH

ENV GOTOOLCHAIN=go1.26.6 CGO_ENABLED=0

RUN <<EOF
    set -eo pipefail
    KUSTOMIZE_VERSION=5.8.1
    KUSTOMIZE_COMMIT=9790a1c3efd2fd35f1b768d495112834176581c1
    git clone --depth 1 --branch kustomize/v${KUSTOMIZE_VERSION} https://github.com/kubernetes-sigs/kustomize /build
    cd /build
    test "$(git rev-parse HEAD)" = "${KUSTOMIZE_COMMIT}"
    cd kustomize
    go get golang.org/x/text@v0.40.0
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
    YQ_VERSION=4.53.3
    CHECKSUM_amd64="fa52a4e758c63d38299163fbdd1edfb4c4963247918bf9c1c5d31d84789eded4"
    CHECKSUM_arm64="578648e463a11c1b6db6010cbf41eafed6bee79466fcffa1bb446672cf7945ea"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /usr/local/bin/yq https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_${TARGETARCH}
    echo "${!CHECKSUM} /usr/local/bin/yq" | sha256sum --check --status
    chmod +x /usr/local/bin/yq
EOF

RUN <<EOF
    HUGO_VERSION=0.165.0
    CHECKSUM_amd64="5c3a37a5450b3e386e5b75a87a790fea2d04a796d75e171216c80ef48a32b432"
    CHECKSUM_arm64="65c9fdd75e82d5f1eaf565f6e9fede6c0ceecaa267798e10c73068986996b77d"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/hugo.tar.gz https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_${HUGO_VERSION}_linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/hugo.tar.gz" | sha256sum --check --status
    cd /usr/local/bin
    tar -zxf /tmp/hugo.tar.gz hugo
    rm /tmp/hugo.tar.gz
EOF

RUN <<EOF
    UV_VERSION=0.12.5
    ARCHNAME_amd64=x86_64
    ARCHNAME_arm64=aarch64
    ARCHNAME=ARCHNAME_${TARGETARCH}
    CHECKSUM_amd64="68a509da24b06b4223a1c0175fb5eb5bc79342b76cbeff0cfe51ac3f5b17b6b2"
    CHECKSUM_arm64="9bf43b4d1a07665bf64d4c4e710930b382321a785e0eb10aac07f46471f86a31"
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
    KUBECTL_VERSION=1.34.11
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="245d980bec82376dbc1cc1dee6ed80333fa82f4e7a4a1beef7acb4381fd63416afb59b158fca5672ea5c87d3335268bc1537cab8339004bcfc78c7124c33ea75"
    CHECKSUM_arm64="d632ea6f349e786e0cc8afe36c293555d5cd2d3e90cbb79ba19cb100f7f9985eace8291f15e3b6f3683c39a8e5fd6b1ef470ecc4edf1a92cfe07d096f721ef9a"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 3 -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz kubernetes/client/bin/kubectl
    mv /opt/kubernetes/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
EOF

RUN <<EOF
    KUBECTL_VERSION=1.35.8
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="5e138260db123d000dfabf2a0535c6958b938112f499f698a91097402c9dcf2a0eb4ddcf57707f63e889c1e40e3e22f15fcb2c46a846d069a3a66f91b4912158"
    CHECKSUM_arm64="f21c47e1bc01fc39f3043980f9fbb91d8f6579dda33b57c2bd16a558fb0c78dce37a928ec111dd3cec11a7fb93fc1f05e25705b7ac2818b1a970c6d2ce1175ea"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 3 -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz kubernetes/client/bin/kubectl
    mv /opt/kubernetes/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
EOF

RUN <<EOF
    KUBECTL_VERSION=1.36.4
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="611ab5bb43640f4eb37302faddde2dbb900e0b79d3e96286acd86a6935b55ad766b7083aa1dcf0e7806da43de5055b1f7056454aaff33d6fabf11cf5c7b41f7d"
    CHECKSUM_arm64="583f43c85ab8fb884dad26c1650085b073148c390f618dbe89f9d0780a0cf503374c42a8281d377fb13c1b9c9ad677ede6e4e924b09cea2b5482ecf3af300467"
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
    YTT_VERSION=0.55.1
    CHECKSUM_amd64="3a2c925ed222f8db4956946d40279688edd6ceb3e919f03f919a8fc8b8532eda"
    CHECKSUM_arm64="ce61f7aee3f66f9b78d5781ef8528b7c8e199a2747796ef17a954118d3e65724"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/ytt https://github.com/carvel-dev/ytt/releases/download/v${YTT_VERSION}/ytt-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/ytt" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/ytt
EOF

RUN <<EOF
    IMGPKG_VERSION=0.48.1
    CHECKSUM_amd64="6c7a1a2e6555e7827c3c97580be87c84d06778d0650d308f7dc67613aec7c7c8"
    CHECKSUM_arm64="ce7243af30411581474ab86051774626b34c5b95ff1d030b3ee979ebddaf795d"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/imgpkg https://github.com/carvel-dev/imgpkg/releases/download/v${IMGPKG_VERSION}/imgpkg-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/imgpkg" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/imgpkg
EOF

RUN <<EOF
    KBLD_VERSION=0.49.1
    CHECKSUM_amd64="437d38d3e59d01dd0d1ad75b4eb67fbd04fe51ed3de1ed55c3f7b3b7d5ec7546"
    CHECKSUM_arm64="b3f5277ff4819de189d1ebd6e455b3390308ca43b122b5bb3c40a7bb9e6f30c4"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/kbld https://github.com/carvel-dev/kbld/releases/download/v${KBLD_VERSION}/kbld-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/kbld" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/kbld
EOF

RUN <<EOF
    KAPP_VERSION=0.65.3
    CHECKSUM_amd64="1724da4b62982285b1da696fb0354738e33913b33e59f3787b5c2b5ac7030327"
    CHECKSUM_arm64="485eb76508c33365780a91831ebbf030b1be93b02d6011935abdaa751328d719"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/kapp https://github.com/carvel-dev/kapp/releases/download/v${KAPP_VERSION}/kapp-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/kapp" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/kapp
EOF

# vendir is the patched source rebuild from the vendir-build stage.
COPY --from=vendir-build /vendir /opt/kubernetes/bin/vendir

RUN <<EOF
    HELM_VERSION=4.2.4
    CHECKSUM_amd64="c306b46f719b0a4da32d0f78ee21bf90ce8d602f15b22ab753f0674d1670a7f3"
    CHECKSUM_arm64="564de2191b881e9f71b5606b25345821ea1682f06ab90499d3ab22b530176da1"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/helm.tar.gz https://get.helm.sh/helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/helm.tar.gz" | sha256sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 1 -zxvf /tmp/helm.tar.gz linux-${TARGETARCH}/helm
    rm /tmp/helm.tar.gz
EOF

RUN <<EOF
    SKAFFOLD_VERSION=2.23.0
    CHECKSUM_amd64="c44fb83ec88a9ed2945f3326a6d6b78bb957354535f0efcf653413bcd0e9510d"
    CHECKSUM_arm64="d7905e255319c250302cf52285abe68e59699a91534a5e2be06613186d1916dd"
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
    CODE_VERSION=4.133.0
    CHECKSUM_amd64="a4e0f8f8c76e7de8e7424289f74e507af4c97bfe104c3e8ee272b8cc7b46c6f1"
    CHECKSUM_arm64="d999d8b0256e5537f3b62e6c09f624220026e19107a04a876c0cef62d1c71147"
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
