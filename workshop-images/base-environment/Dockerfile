#syntax=docker/dockerfile:1.3-labs

FROM kubernetesui/dashboard:v2.7.0 AS k8s-console

FROM fedora:42 AS system-base

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
    curl -fsSL https://rpm.nodesource.com/setup_20.x | bash - && \
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

FROM golang:1.19-buster as builder-image

WORKDIR /app

RUN curl --silent --fail -L -o /tmp/git-serve.tar.gz https://github.com/cirocosta/git-serve/archive/refs/tags/v0.0.5.tar.gz && \
echo "09cd14a34f17d88cd4f0d2b73e0bbd0bf56984be21bc947f416a7824a709011e /tmp/git-serve.tar.gz" | sha256sum --check --status && \
    tar xvf /tmp/git-serve.tar.gz && \
    cd git-serve-0.0.5 && \
    go mod download && \
    go build -o git-serve cmd/git-serve/main.go

FROM system-base AS scratch-image

ARG TARGETARCH

# Kubernetes web console.

COPY --from=k8s-console / /opt/console/

# Miscellaneous tools.

RUN <<EOF
    BOMBARDIER_VERSION=2.0.2
    CHECKSUM_amd64="d3dc4a9ecec3f8bd43f8c196ad533cb4b79ca55b11fc74c3184231e00a95bc0b"
    CHECKSUM_arm64="258025ea4dc183bbe3f4ccc7772de1faf2c73700e9f81aa5b84e0c1799d7323c"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /usr/local/bin/bombardier https://github.com/codesenberg/bombardier/releases/download/v${BOMBARDIER_VERSION}/bombardier-linux-${TARGETARCH}
    echo "${!CHECKSUM}  /usr/local/bin/bombardier" | sha256sum --check --status
    chmod +x /usr/local/bin/bombardier
EOF

RUN <<EOF
    YQ_VERSION=4.48.1
    CHECKSUM_amd64="99df6047f5b577a9d25f969f7c3823ada3488de2e2115b30a0abb10d9324fd9f"
    CHECKSUM_arm64="0e46b5b926a9e57c526fa2bd8f8e38b7e17fbf6e2403ff1741f3b268e3363a9e"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /usr/local/bin/yq https://github.com/mikefarah/yq/releases/download/v${YQ_VERSION}/yq_linux_${TARGETARCH}
    echo "${!CHECKSUM} /usr/local/bin/yq" | sha256sum --check --status
    chmod +x /usr/local/bin/yq
EOF

RUN <<EOF
    HUGO_VERSION=0.152.2
    CHECKSUM_amd64="52b6eda6c00f4449d96f0cbfd7300e834c26179c4fe68e0510ef566db52dba04"
    CHECKSUM_arm64="76b6f2d229cba2e81555cfe08cf2a32294b6ce68f254cae6b981b062868c50ae"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/hugo.tar.gz https://github.com/gohugoio/hugo/releases/download/v${HUGO_VERSION}/hugo_${HUGO_VERSION}_linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/hugo.tar.gz" | sha256sum --check --status
    cd /usr/local/bin
    tar -zxf /tmp/hugo.tar.gz hugo
    rm /tmp/hugo.tar.gz
EOF

RUN <<EOF
    DIVE_VERSION=0.13.1
    CHECKSUM_amd64="0970549eb4a306f8825a84145a2534153badb4d7dcf3febd1967c706367c3d0e"
    CHECKSUM_arm64="2fcd2cf20f634ccdb41efac44048b204bfc867c115641f37a7420693ed480a18"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/dive.tar.gz https://github.com/wagoodman/dive/releases/download/v${DIVE_VERSION}/dive_${DIVE_VERSION}_linux_${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/dive.tar.gz" | sha256sum --check --status
    tar -C /usr/local/bin -zxvf /tmp/dive.tar.gz dive
    rm /tmp/dive.tar.gz
EOF

RUN <<EOF
    UV_VERSION=0.9.8
    ARCHNAME_amd64=x86_64
    ARCHNAME_arm64=aarch64
    ARCHNAME=ARCHNAME_${TARGETARCH}
    CHECKSUM_amd64="950bad4899800ab158db97081b30a601a3ff4237d705756fda8695342cfcc20a"
    CHECKSUM_arm64="8dc203a27c99f721545adc531279f0486ea3deb77c951a051e6bb9ea1d588a71"
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
    KUBECTL_VERSION=1.31.13
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="724a8082d31664e70320f954564548ab222bf9a60b5a117456e93c56d0e8c921fe427dc639e25f5d256d4886aff61ef74a2fe535dc1873e56a681cc62a322610"
    CHECKSUM_arm64="d275c20fd3e0c17511a294e352247794063bb2f8ab15713f9db7b6d0b771574736f7986debea51c59c8aecec312c8caf4c76cebf7ca7d070397ba0c460dda5d6"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /tmp -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
    mv /tmp/kubernetes/client/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    mv /tmp/kubernetes/client/bin/kubectl-convert /opt/kubernetes/bin/kubectl-convert@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
    rm -rf /tmp/kubernetes
EOF

RUN <<EOF
    KUBECTL_VERSION=1.32.9
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="80c2b593fcbee404fb70412a6ed8c428ed5eb256b79c1ca180f9b405f26de708629014401db0dff8ac4af3471411dd445b13d9f6f71149efbbd4956ac51db546"
    CHECKSUM_arm64="b126355cb3df897e19e6035e0665fc17bccf8b17455d1400599a2a75356d88568d3f29cc8deee3eaad22973a38e4b5a5e58fa30e1c6b4b6c9abdb5026e6ebffc"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /tmp -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
    mv /tmp/kubernetes/client/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    mv /tmp/kubernetes/client/bin/kubectl-convert /opt/kubernetes/bin/kubectl-convert@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
    rm -rf /tmp/kubernetes
EOF

RUN <<EOF
    KUBECTL_VERSION=1.33.5
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="c0959e01d4d82b293848202b4392f8c784ca7e27abb68ebae36c303e34abb83378bcfa5679699c15681f1a3fc6eb801ea1204e086fc8e6512f11023fb1e178e7"
    CHECKSUM_arm64="1d49d8cb369d6f61a21d7616b01e64e1ae52ddc22f3c96b713a00603a0d28634fcd0d425e2d81ea0e5132a64c5843927941f84979ba5ea1a0a72eb198a1a9f8b"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /tmp -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
    mv /tmp/kubernetes/client/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    mv /tmp/kubernetes/client/bin/kubectl-convert /opt/kubernetes/bin/kubectl-convert@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
    rm -rf /tmp/kubernetes
EOF

RUN <<EOF
    KUBECTL_VERSION=1.34.2
    KUBECTL_SHORT_VERSION=$(echo ${KUBECTL_VERSION} | cut -d. -f1,2)
    CHECKSUM_amd64="a9656e446054151390279b5d2a57cdc52cc0546ae5dcac17c370cc4435d93486da9d9603f560cfca64233ad2f9642dd41219c2f308196491af194faefe2cf2ae"
    CHECKSUM_arm64="ffbe3367b61531db26494388776f53fe9c83793dc7bab83c3435e5d884846ac25f8e9b8132be083bcdc120462d00ea108dcc033790ff50be20ccb396de6ab786"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz https://dl.k8s.io/v${KUBECTL_VERSION}/kubernetes-client-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz" | sha512sum --check --status
    tar -C /tmp -zxf /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
    mv /tmp/kubernetes/client/bin/kubectl /opt/kubernetes/bin/kubectl@${KUBECTL_SHORT_VERSION}
    mv /tmp/kubernetes/client/bin/kubectl-convert /opt/kubernetes/bin/kubectl-convert@${KUBECTL_SHORT_VERSION}
    rm -f /tmp/kubernetes-client-linux-${TARGETARCH}.tar.gz
    rm -rf /tmp/kubernetes
EOF

RUN <<EOF
    K9S_VERSION=0.50.16
    CHECKSUM_amd64="bda09dc030a08987fe2b3bed678b15b52f23d6705e872d561932d4ca07db7818"
    CHECKSUM_arm64="7f3b414bc5e6b584fbcb97f9f4f5b2c67a51cdffcbccb95adcadbaeab904e98e"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/k9s.tar.gz https://github.com/derailed/k9s/releases/download/v${K9S_VERSION}/k9s_Linux_${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/k9s.tar.gz" | sha256sum --check --status
    tar -C /tmp -zxf /tmp/k9s.tar.gz k9s
    mv /tmp/k9s /opt/kubernetes/bin/k9s
    rm /tmp/k9s.tar.gz
EOF

RUN <<EOF
    YTT_VERSION=0.52.1
    CHECKSUM_amd64="490f138ae5b6864071d3c20a5a231e378cee7487cd4aeffc79dbf66718e65408"
    CHECKSUM_arm64="7d86bd3299e43d1455201fc213d698bae7482cd88f3e05de2f935e6eab842db9"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/ytt https://github.com/carvel-dev/ytt/releases/download/v${YTT_VERSION}/ytt-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/ytt" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/ytt
EOF

RUN <<EOF
    IMGPKG_VERSION=0.46.1
    CHECKSUM_amd64="1bc6b735dbdd940a5c78661781f937090bd5fbc89172f01e600ee91fe122edbe"
    CHECKSUM_arm64="3e7cfba3cc55401ad1bbc80c7710e67a0376c56d8a72dc81864ffbbe64b30aba"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/imgpkg https://github.com/carvel-dev/imgpkg/releases/download/v${IMGPKG_VERSION}/imgpkg-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/imgpkg" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/imgpkg
EOF

RUN <<EOF
    KBLD_VERSION=0.46.0
    CHECKSUM_amd64="90e8d123d5e33ab13b849997bdb05626fa7f2384120d87a29eebd56490105ca9"
    CHECKSUM_arm64="f0202ff24fda501ead70f4b83f24d0e078bb45b52e8ce7217f0b78a6bfedebc3"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/kbld https://github.com/carvel-dev/kbld/releases/download/v${KBLD_VERSION}/kbld-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/kbld" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/kbld
EOF

RUN <<EOF
    KAPP_VERSION=0.64.2
    CHECKSUM_amd64="475ed4fc7ee538efceeb02972524a17cb580d9b3e59ab16c7a18de02427daedc"
    CHECKSUM_arm64="bffda57ed0c83cd2a8cc6faaaf97b3aa71f000658258eeaae6b0a31531a0e03a"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/kapp https://github.com/carvel-dev/kapp/releases/download/v${KAPP_VERSION}/kapp-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/kapp" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/kapp
EOF

RUN <<EOF
    KWT_VERSION=0.0.8
    CHECKSUM_amd64="1022483a8b59fe238e782a9138f1fee6ca61ecf7ccd1e5f0d98e95c56df94d87"
    CHECKSUM_arm64="7b94a134cbde5ff2e245d102f54b9ac9f81b3fcc5e54a5cefecc1e5845b8a65f"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/kwt https://github.com/carvel-dev/kwt/releases/download/v${KWT_VERSION}/kwt-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/kwt" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/kwt
EOF

RUN <<EOF
    VENDIR_VERSION=0.44.0
    CHECKSUM_amd64="a2befbb9dd4f174aac7a34fe0bd50b1e5dc356dadaed0183a24b817f2fd1d094"
    CHECKSUM_arm64="db33e705d818f4fa1fb3c19bd97167219188650b96e307a8e72620329aec9a91"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/vendir https://github.com/carvel-dev/vendir/releases/download/v${VENDIR_VERSION}/vendir-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/vendir" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/vendir
EOF

RUN <<EOF
    KCTRL_VERSION=0.58.1
    CHECKSUM_amd64="978fc078d53a827716dacde7483294d1265c3d42ace0c2582df62b5c38dd17ae"
    CHECKSUM_arm64="a9cb74f12effb45c56d16b56551aaf50e96fac0359b5ace6ec8e40d370762ac8"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/kctrl https://github.com/carvel-dev/kapp-controller/releases/download/v${KCTRL_VERSION}/kctrl-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/kctrl" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/kctrl
EOF

RUN <<EOF
    OCTANT_VERSION=0.12.1
    ARCHNAME_amd64=64bit
    ARCHNAME_arm64=arm64
    ARCHNAME=ARCHNAME_${TARGETARCH}
    CHECKSUM_amd64="b56ca09fb92314eb6a7b1a0ddcc65b582990e3fdef6e2a996cacd4a24b4e54bf"
    CHECKSUM_arm64="2808448a78d7c55e40ed34bcd3cd4db04b5cf847884938af047b73eb7a40bcd5"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/octant.tar.gz https://github.com/vmware-tanzu/octant/releases/download/v${OCTANT_VERSION}/octant_${OCTANT_VERSION}_Linux-${!ARCHNAME}.tar.gz
    tar -C /opt/kubernetes/bin --strip-components 1 -xf /tmp/octant.tar.gz octant_${OCTANT_VERSION}_Linux-${!ARCHNAME}/octant
    mv /opt/kubernetes/bin/octant /opt/kubernetes/bin/octant@${OCTANT_VERSION}
    rm -f /tmp/octant.tar.gz
EOF

RUN <<EOF
    OCTANT_VERSION=0.25.1
    ARCHNAME_amd64=64bit
    ARCHNAME_arm64=arm64
    ARCHNAME=ARCHNAME_${TARGETARCH}
    CHECKSUM_amd64="b12bb6752e43f4e0fe54278df8e98dee3439c4066f66cdb7a0ca4a1c7d8eaa1e"
    CHECKSUM_arm64="a3eb4973a0c869267e3916bd43e0b41b2bbc73b898376b795a617299c7b2a623"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/octant.tar.gz https://github.com/vmware-tanzu/octant/releases/download/v${OCTANT_VERSION}/octant_${OCTANT_VERSION}_Linux-${!ARCHNAME}.tar.gz
    tar -C /opt/kubernetes/bin --strip-components 1 -xf /tmp/octant.tar.gz octant_${OCTANT_VERSION}_Linux-${!ARCHNAME}/octant
    mv /opt/kubernetes/bin/octant /opt/kubernetes/bin/octant@${OCTANT_VERSION}
    ln -s octant@${OCTANT_VERSION} /opt/kubernetes/bin/octant@latest
    rm -f /tmp/octant.tar.gz
EOF

RUN <<EOF
    HELM_VERSION=3.19.1
    CHECKSUM_amd64="966bed9b1e0dda11268f59bd7268c3cd3e308b37b070546e1d78a02526ff63f2"
    CHECKSUM_arm64="ceed150305a1d1ef4a37923a7f66931a6807c34a38ea487fa8340e102dd2c7f7"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/helm.tar.gz https://get.helm.sh/helm-v${HELM_VERSION}-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/helm.tar.gz" | sha256sum --check --status
    tar -C /opt/kubernetes/bin --strip-components 1 -zxvf /tmp/helm.tar.gz linux-${TARGETARCH}/helm
    rm /tmp/helm.tar.gz
EOF

RUN <<EOF
    SKAFFOLD_VERSION=2.16.1
    CHECKSUM_amd64="1cbeea85aa14ba603dbc2bbdfa7bfde5644d7988beed0fdc0fd1c67298d4cf67"
    CHECKSUM_arm64="b68fa949b7a918935641061b5bf3917e0be3a5ecaa591169f7cc0f8362c3fb82"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /opt/kubernetes/bin/skaffold https://github.com/GoogleContainerTools/skaffold/releases/download/v${SKAFFOLD_VERSION}/skaffold-linux-${TARGETARCH}
    echo "${!CHECKSUM} /opt/kubernetes/bin/skaffold" | sha256sum --check --status
    chmod +x /opt/kubernetes/bin/skaffold
EOF

RUN <<EOF
    KUSTOMIZE_VERSION=5.8.0
    CHECKSUM_amd64="4dfa8307358dd9284aa4d2b1d5596766a65b93433e8fa3f9f74498941f01c5ef"
    CHECKSUM_arm64="a4f48b4c3d4ca97d748943e19169de85a2e86e80bcc09558603e2aa66fb15ce1"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    curl --silent --fail -L -o /tmp/kustomize.tar.gz https://github.com/kubernetes-sigs/kustomize/releases/download/kustomize/v${KUSTOMIZE_VERSION}/kustomize_v${KUSTOMIZE_VERSION}_linux_${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/kustomize.tar.gz" | sha256sum --check --status
    tar -C /opt/kubernetes/bin -zxvf /tmp/kustomize.tar.gz kustomize
    rm /tmp/kustomize.tar.gz
EOF

# VS Code editor and dashboard extension.
# Make sure when updating that AI features are disabled and working https://github.com/coder/code-server/issues/7540
RUN <<EOF
    CODE_VERSION=4.104.3
    CHECKSUM_amd64="bfcb80e826d2a448132994ea1583ecc7924e2b9300ea02e8e3dca09dbb13c1d9"
    CHECKSUM_arm64="902db76c1764df4670c584045db5854367ffcb135635607b76875cd41ebf510a"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    set -eo pipefail
    mkdir /opt/editor
    curl --silent --fail -L -o /tmp/code-server.tar.gz https://github.com/cdr/code-server/releases/download/v${CODE_VERSION}/code-server-${CODE_VERSION}-linux-${TARGETARCH}.tar.gz
    echo "${!CHECKSUM} /tmp/code-server.tar.gz" | sha256sum --check --status
    cd /opt/editor
    tar -zxf /tmp/code-server.tar.gz --strip-components=1
    rm /tmp/code-server.tar.gz
EOF

COPY --from=vscode-helper --chown=1001:0 /opt/helper/educates-0.0.1.vsix /opt/eduk8s/educates-0.0.1.vsix

# Git server.

RUN mkdir /opt/git /opt/git/bin /opt/git/repositories

COPY --from=builder-image /app/git-serve-0.0.5/git-serve /opt/git/bin/git-serve

# Dashboard applications.

COPY opt/. /opt/

RUN mkdir -p /opt/slides/reveal.js/3.9.2 && \
    cd /opt/slides/reveal.js/3.9.2 && \
    curl -sL -o src.tar.gz https://github.com/hakimel/reveal.js/archive/3.9.2.tar.gz && \
    tar --strip-components 1 -xf src.tar.gz && \
    rm src.tar.gz && \
    mkdir -p /opt/slides/reveal.js/4.6.1 && \
    cd /opt/slides/reveal.js/4.6.1 && \
    curl -sL -o src.tar.gz https://github.com/hakimel/reveal.js/archive/4.6.1.tar.gz && \
    tar --strip-components 1 -xf src.tar.gz && \
    rm src.tar.gz && \
    mkdir -p /opt/slides/reveal.js/5.2.1 && \
    cd /opt/slides/reveal.js/5.2.1 && \
    curl -sL -o src.tar.gz https://github.com/hakimel/reveal.js/archive/5.2.1.tar.gz && \
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
    npm cache clean --force

RUN cd /opt/renderer && \
    npm install && \
    npm run compile && \
    npm prune --production && \
    npm cache clean --force

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
    PATH=/home/eduk8s/bin:/opt/eduk8s/bin:/opt/kubernetes/bin:/opt/editor/bin:$PATH \
    PLATFORM_ARCH=${TARGETARCH}

ENV BASH_ENV=/opt/eduk8s/etc/profile \
    ENV=/opt/eduk8s/etc/profile \
    PROMPT_COMMAND=". /opt/eduk8s/etc/profile"

WORKDIR /home/eduk8s

USER 1001

ENTRYPOINT [ "container-entrypoint" ]

EXPOSE 10081

CMD [ "start-container" ]
