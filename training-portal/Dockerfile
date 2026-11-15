# syntax=docker/dockerfile:1

# ---- builder: uv, managed CPython, venv, mod_wsgi compile, static files ----

FROM debian:trixie-slim AS builder

COPY --from=ghcr.io/astral-sh/uv:0.13 /uv /usr/local/bin/

ENV DEBIAN_FRONTEND=noninteractive

# mod_wsgi is compiled against the Debian Apache during `uv sync`, which
# needs apxs from apache2-dev plus a C toolchain. None of this reaches the
# final image. The managed interpreter ships its own headers.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        apache2-dev \
        ca-certificates \
        gcc \
        make \
    && rm -rf /var/lib/apt/lists/* && \
    mkdir -p /opt/app-root/src && \
    chown -R 1001:0 /opt/app-root

USER 1001

ENV PATH=/opt/app-root/bin:/opt/app-root/venv/bin:$PATH \
    UV_PYTHON_DOWNLOADS=automatic \
    UV_PYTHON_PREFERENCE=only-managed \
    UV_PYTHON_INSTALL_DIR=/opt/app-root/python \
    UV_PROJECT_ENVIRONMENT=/opt/app-root/venv \
    UV_COMPILE_BYTECODE=1

COPY --chown=1001:0 pyproject.toml uv.lock .python-version /opt/app-root/

# Trim the managed interpreter in the same step that installs it so the
# removed files never land in a layer: the stdlib test suite, the static
# library, IDLE, tkinter, ensurepip and turtledemo are not needed at runtime.
# Only paths under the interpreter prefix; never the venv's site-packages.
RUN uv sync --project /opt/app-root --frozen --no-dev --no-cache && \
    rm -rf /opt/app-root/python/*/lib/python3.*/test \
           /opt/app-root/python/*/lib/python3.*/idlelib \
           /opt/app-root/python/*/lib/python3.*/tkinter \
           /opt/app-root/python/*/lib/python3.*/ensurepip \
           /opt/app-root/python/*/lib/python3.*/turtledemo \
           /opt/app-root/python/*/lib/libpython3.*.a

COPY --chown=1001:0 ./ /opt/app-root

WORKDIR /opt/app-root/src

# Loading the settings to collect the static files writes a new secret key
# into the data directory. Remove it in the same step so no key ships in the
# image: each training portal generates its own on first start, in the data
# volume mounted over that directory.
RUN python manage.py collectstatic --no-input && \
    rm -f /opt/app-root/data/secret-key.txt

# ---- final: runtime only --------------------------------------------------

FROM debian:trixie-slim

ENV DEBIAN_FRONTEND=noninteractive

# apache2-bin is the httpd binary and modules mod_wsgi-express runs;
# media-types supplies /etc/mime.types for its generated configuration.
# The apache2 metapackage is deliberately not used, as it drags in perl,
# procps and the Debian site configuration that mod_wsgi-express ignores.
# curl is included deliberately: it is the one debugging tool worth having
# when using kubectl exec against a running pod, and it honours the CA bundle
# variables set below.
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
        apache2-bin \
        ca-certificates \
        curl \
        media-types \
    && rm -rf /var/lib/apt/lists/* && \
    useradd -u 1001 -g 0 -M -d /opt/app-root/src default && \
    mkdir -p /opt/app-root && \
    cp /etc/ssl/certs/ca-certificates.crt /opt/app-root/ca-bundle.crt && \
    chown -R 1001:0 /opt/app-root

# The venv records the interpreter by absolute path and mod_wsgi records the
# Apache paths it was built against, so the tree must land where it was built.
# This brings the interpreter, venv, application source and collected static
# files across in one step.
COPY --from=builder --chown=1001:0 /opt/app-root /opt/app-root

WORKDIR /opt/app-root/src

ENV PYTHONUNBUFFERED=1 \
    PYTHONIOENCODING=UTF-8 \
    LC_ALL=C.UTF-8 \
    LANG=C.UTF-8 \
    PATH=/opt/app-root/bin:/opt/app-root/venv/bin:$PATH

# CA bundle overrides. The file exists from build time (system roots only,
# copied above) so every consumer works with no CA mounted; the start script
# overwrites it with system roots plus the ingress CA when one is mounted.
# Set here, not in the start script, so kubectl exec and overridden commands
# see them.
ENV SSL_CERT_FILE=/opt/app-root/ca-bundle.crt \
    REQUESTS_CA_BUNDLE=/opt/app-root/ca-bundle.crt \
    CURL_CA_BUNDLE=/opt/app-root/ca-bundle.crt \
    GIT_SSL_CAINFO=/opt/app-root/ca-bundle.crt \
    NODE_EXTRA_CA_CERTS=/opt/app-root/ca-bundle.crt

USER 1001

CMD [ "start-container" ]

EXPOSE 8080
