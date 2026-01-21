#syntax=docker/dockerfile:1.3-labs

ARG IMAGE_REPOSITORY=localhost:5001
ARG BASE_IMAGE_NAME=educates-base-environment
ARG PACKAGE_VERSION=latest

FROM ${IMAGE_REPOSITORY}/${BASE_IMAGE_NAME}:${PACKAGE_VERSION} AS scratch-image

ARG TARGETARCH

RUN mkdir -p /opt/{java,gradle,maven}

RUN <<EOF
    set -eo pipefail
    ARCHNAME_amd64=x64
    ARCHNAME_arm64=aarch64
    ARCHNAME=ARCHNAME_${TARGETARCH}
    CHECKSUM_amd64="5d64ae542b59a962b3caadadd346f4b1c3010879a28bb02d928326993de16e79"
    CHECKSUM_arm64="19552c1cf7f5c18290a6bdcd6757f70ea5c331a2bc0dd7a3b3120e8dbc4b4891"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    curl --fail -sL -o /tmp/jdk8.tar.gz https://github.com/adoptium/temurin8-binaries/releases/download/jdk8u462-b08/OpenJDK8U-jdk_${!ARCHNAME}_linux_hotspot_8u462b08.tar.gz
    echo "${!CHECKSUM} /tmp/jdk8.tar.gz" | sha256sum --check --status
    tar -C /opt/java --strip-components 1 -zxf /tmp/jdk8.tar.gz
    rm /tmp/jdk8.tar.gz
EOF

RUN curl --fail -sL -o /tmp/maven.tar.gz https://archive.apache.org/dist/maven/maven-3/3.9.11/binaries/apache-maven-3.9.11-bin.tar.gz && \
    echo "bcfe4fe305c962ace56ac7b5fc7a08b87d5abd8b7e89027ab251069faebee516b0ded8961445d6d91ec1985dfe30f8153268843c89aa392733d1a3ec956c9978 /tmp/maven.tar.gz" | sha512sum --check --status && \
    tar -C /opt/maven --strip-components 1 -zxf /tmp/maven.tar.gz && \
    rm /tmp/maven.tar.gz

RUN curl --fail -sL -o /tmp/gradle.zip https://services.gradle.org/distributions/gradle-8.5-bin.zip && \
    echo "9d926787066a081739e8200858338b4a69e837c3a821a33aca9db09dd4a41026 /tmp/gradle.zip" | sha256sum --check --status && \
    unzip -d /opt/gradle /tmp/gradle.zip && \
    mv /opt/gradle/gradle-8.5/* /opt/gradle/ && \
    rm -rf /opt/gradle/gradle-8.5 && \
    rm /tmp/gradle.zip

ENV PATH=/opt/java/bin:/opt/gradle/bin:/opt/maven/bin:$PATH \
    JAVA_HOME=/opt/java \
    M2_HOME=/opt/maven

RUN mvn archetype:generate -DgroupId=com.mycompany.app -DartifactId=my-app \
        -DarchetypeArtifactId=maven-archetype-quickstart \
        -DarchetypeVersion=1.4 -DinteractiveMode=false && \
    cd my-app && \
    mvn wrapper:wrapper

RUN gradle init && \
    gradle wrapper --gradle-version=8.5 --distribution-type=bin && \
    ./gradlew build

FROM ${IMAGE_REPOSITORY}/${BASE_IMAGE_NAME}:${PACKAGE_VERSION}

COPY --from=scratch-image --chown=1001:0 /opt/java /opt/java
COPY --from=scratch-image --chown=1001:0 /opt/gradle /opt/gradle
COPY --from=scratch-image --chown=1001:0 /opt/maven /opt/maven

COPY --from=scratch-image --chown=1001:0 /home/eduk8s/.m2 /home/eduk8s/.m2
COPY --from=scratch-image --chown=1001:0 /home/eduk8s/.gradle /home/eduk8s/.gradle

COPY --chown=1001:0 opt/. /opt/

ENV PATH=/opt/java/bin:/opt/gradle/bin:/opt/maven/bin:$PATH \
    JAVA_HOME=/opt/java \
    M2_HOME=/opt/maven
