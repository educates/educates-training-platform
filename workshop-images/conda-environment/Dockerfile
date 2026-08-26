#syntax=docker/dockerfile:1.3-labs

ARG IMAGE_REPOSITORY=localhost:5001
ARG BASE_IMAGE_NAME=educates-base-environment
ARG PACKAGE_VERSION=latest

FROM ${IMAGE_REPOSITORY}/${BASE_IMAGE_NAME}:${PACKAGE_VERSION}

ARG TARGETARCH

ENV CONDA_DIR=/opt/conda \
    PATH=/opt/conda/bin:$PATH

ENV MINICONDA_VERSION=26.5.3-2 \
    CONDA_VERSION=26.5.3

RUN <<EOF
    set -x
    set -eo pipefail
    ARCHNAME_amd64=x86_64
    ARCHNAME_arm64=aarch64
    ARCHNAME=ARCHNAME_${TARGETARCH}
    CHECKSUM_amd64="80bc27f13c4de90f10e387aa45e864de4f0860692c1221aef5900009a2b55302"
    CHECKSUM_arm64="999e8761f4dc74fb8d0dc9f74d5374f6708ff81eebbb0af677a4d5307d38b2e5"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    mkdir -p $CONDA_DIR
    cd /tmp
    curl --fail --silent -L -o install-miniconda.sh "https://repo.anaconda.com/miniconda/Miniconda3-py314_${MINICONDA_VERSION}-Linux-${!ARCHNAME}.sh"
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
    conda install --quiet --yes 'notebook=7.6.1' 'jupyterlab=4.6.2'
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
