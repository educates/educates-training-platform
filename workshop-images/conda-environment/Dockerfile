#syntax=docker/dockerfile:1.3-labs

ARG IMAGE_REPOSITORY=localhost:5001
ARG BASE_IMAGE_NAME=educates-base-environment
ARG PACKAGE_VERSION=latest

FROM ${IMAGE_REPOSITORY}/${BASE_IMAGE_NAME}:${PACKAGE_VERSION}

ARG TARGETARCH

ENV CONDA_DIR=/opt/conda \
    PATH=/opt/conda/bin:$PATH

ENV MINICONDA_VERSION=25.5.1-1 \
    CONDA_VERSION=25.5.1

RUN <<EOF
    set -x
    set -eo pipefail
    ARCHNAME_amd64=x86_64
    ARCHNAME_arm64=aarch64
    ARCHNAME=ARCHNAME_${TARGETARCH}
    CHECKSUM_amd64="612af113b49db0368e2be41ac4d51b7088eebd5f31daeeb89f23fff8f920db58"
    CHECKSUM_arm64="b7d611dcaa638efd700a4a4eb24fbcb9f7b94cc1773d7c655959c330d0b68e16"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    mkdir -p $CONDA_DIR
    cd /tmp
    curl --fail --silent -L -o install-miniconda.sh "https://repo.anaconda.com/miniconda/Miniconda3-py313_${MINICONDA_VERSION}-Linux-${!ARCHNAME}.sh"
    echo "${!CHECKSUM} install-miniconda.sh" | sha256sum --check --status
    /bin/bash install-miniconda.sh -f -b -p $CONDA_DIR
    rm install-miniconda.sh
    # echo "conda ${CONDA_VERSION}" >> $CONDA_DIR/conda-meta/pinned
    conda config --system --prepend channels conda-forge
    conda config --system --set auto_update_conda false
    conda config --system --set show_channel_urls true
    conda config --system --set channel_priority strict
    conda config --system --set solver libmamba
    # conda list python | grep '^python ' | tr -s ' ' | cut -d '.' -f 1,2 | sed 's/$/.*/' >> $CONDA_DIR/conda-meta/pinned
    conda tos accept --override-channels --channel https://repo.anaconda.com/pkgs/main
    conda tos accept --override-channels --channel https://repo.anaconda.com/pkgs/r
    conda install --quiet --yes conda
    conda install --quiet --yes pip
    conda update --all --quiet --yes
    conda clean --all -f -y
    rm -rf /home/eduk8s/.cache/yarn
    fix-permissions $CONDA_DIR
    fix-permissions /home/eduk8s
EOF

RUN <<EOF
    set -x
    set -eo pipefail
    conda tos accept --override-channels --channel https://repo.anaconda.com/pkgs/main
    conda tos accept --override-channels --channel https://repo.anaconda.com/pkgs/r
    conda install --quiet --yes 'notebook=7.4.5' 'jupyterlab=4.4.6'
    conda clean --all -f -y
    npm cache clean --force
    rm -rf $CONDA_DIR/share/jupyter/lab/staging
    rm -rf /home/eduk8s/.cache/yarn
    fix-permissions $CONDA_DIR
    fix-permissions /home/eduk8s
EOF

COPY --chown=1001:0 opt/. /opt/

COPY --chown=1001:0 start-jupyterlab.sh /opt/conda

ENV ENABLE_JUPYTERLAB=false
