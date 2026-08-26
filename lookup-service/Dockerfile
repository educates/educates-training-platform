FROM fedora:44

COPY --from=ghcr.io/astral-sh/uv:0.11 /uv /uvx /usr/local/bin/

RUN INSTALL_PKGS=" \
        findutils \
        glibc-langpack-en \
        procps \
        python3 \
        which \
    " && \
    dnf install -y --setopt=tsflags=nodocs $INSTALL_PKGS && \
    dnf clean -y --enablerepo='*' all && \
    useradd -u 1001 -g 0 -M -d /opt/app-root/src default && \
    mkdir -p /opt/app-root/src && \
    chown -R 1001:0 /opt/app-root

WORKDIR /opt/app-root/src

ENV PYTHONUNBUFFERED=1 \
    PYTHONIOENCODING=UTF-8 \
    LC_ALL=en_US.UTF-8 \
    LANG=en_US.UTF-8

USER 1001

ENV PATH=/opt/app-root/bin:/opt/app-root/venv/bin:$PATH \
    UV_PYTHON_DOWNLOADS=never \
    UV_PROJECT_ENVIRONMENT=/opt/app-root/venv \
    UV_COMPILE_BYTECODE=1

COPY --chown=1001:0 pyproject.toml uv.lock /opt/app-root/src/

RUN uv sync --frozen --no-dev --no-cache

COPY --chown=1001:0 ./ /opt/app-root/src

CMD [ "/opt/app-root/src/start-service.sh" ]
