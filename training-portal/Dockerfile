FROM fedora:42

RUN INSTALL_PKGS=" \
        findutils \
        gcc \
        glibc-langpack-en \
        httpd-devel \
        procps \
        python3-devel \
        python3-pip \
        redhat-rpm-config \
        sqlite \
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

COPY --chown=1001:0 requirements.txt /opt/app-root/

ENV PATH=/opt/app-root/bin:/opt/app-root/venv/bin:$PATH

RUN python3 -m venv /opt/app-root/venv && \
    . /opt/app-root/venv/bin/activate && \
    pip install --no-cache-dir -U pip setuptools wheel && \
    pip install --no-cache-dir -r /opt/app-root/requirements.txt

COPY --chown=1001:0 ./ /opt/app-root

RUN . /opt/app-root/venv/bin/activate && \
    python manage.py collectstatic

CMD [ "start-container" ]

EXPOSE 8080
