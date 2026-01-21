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
    CHECKSUM_amd64="f2dc5418092c43003db8f9005c4a286e1c0104fea96ccdd49e8ebd037cac9219"
    CHECKSUM_arm64="e5c41a1ab0865ea5de9b4529bf8526005f1d4593090845387d14fe450ce39c33"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    curl --fail -sL -o /tmp/jdk21.tar.gz https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.8%2B9/OpenJDK21U-jdk_${!ARCHNAME}_linux_hotspot_21.0.8_9.tar.gz
    echo "${!CHECKSUM} /tmp/jdk21.tar.gz" | sha256sum --check --status
    tar -C /opt/java --strip-components 1 -zxf /tmp/jdk21.tar.gz
    rm /tmp/jdk21.tar.gz
EOF

RUN curl --fail -sL -o /tmp/maven.tar.gz https://archive.apache.org/dist/maven/maven-3/3.9.11/binaries/apache-maven-3.9.11-bin.tar.gz && \
    echo "bcfe4fe305c962ace56ac7b5fc7a08b87d5abd8b7e89027ab251069faebee516b0ded8961445d6d91ec1985dfe30f8153268843c89aa392733d1a3ec956c9978 /tmp/maven.tar.gz" | sha512sum --check --status && \
    tar -C /opt/maven --strip-components 1 -zxf /tmp/maven.tar.gz && \
    rm /tmp/maven.tar.gz

    RUN curl --fail -sL -o /tmp/gradle.zip https://services.gradle.org/distributions/gradle-8.8-bin.zip && \
    echo "a4b4158601f8636cdeeab09bd76afb640030bb5b144aafe261a5e8af027dc612 /tmp/gradle.zip" | sha256sum --check --status && \
    unzip -d /opt/gradle /tmp/gradle.zip && \
    mv /opt/gradle/gradle-8.8/* /opt/gradle/ && \
    rm -rf /opt/gradle/gradle-8.8 && \
    rm /tmp/gradle.zip

ENV PATH=/opt/java/bin:/opt/gradle/bin:/opt/maven/bin:$PATH \
    JAVA_HOME=/opt/java \
    M2_HOME=/opt/maven \
    GRADLE_HOME=/opt/gradle

COPY gradle.properties-arm64 .

RUN if [ "${TARGETARCH}" = "arm64" ]; then \
        export MAVEN_OPTS="-XX:UseSVE=0"; \
        export JAVA_OPTS="-XX:UseSVE=0"; \
    fi && \
    mvn archetype:generate -DgroupId=com.mycompany.app -DartifactId=my-app \
        -DarchetypeArtifactId=maven-archetype-quickstart \
        -DarchetypeVersion=1.4 -DinteractiveMode=false && \
    cd my-app && \
    mvn wrapper:wrapper

RUN if [ "${TARGETARCH}" = "arm64" ]; then \
        export MAVEN_OPTS="-XX:UseSVE=0"; \
        export JAVA_OPTS="-XX:UseSVE=0"; \
        mv gradle.properties-arm64 gradle.properties; \
    fi && \
    gradle init && \
    gradle wrapper --gradle-version=8.8 --distribution-type=bin && \
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
