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
    CHECKSUM_amd64="7dfd551795a8884b26cbb02e0301da95db40160bb194f48271dc2ef9367f50c2"
    CHECKSUM_arm64="32c316cb3998a9c9dee2829fbb577ea1c0ed666700cec73e049d44c342bb19af"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    curl --fail -sL -o /tmp/jdk11.tar.gz https://github.com/adoptium/temurin11-binaries/releases/download/jdk-11.0.28%2B6/OpenJDK11U-jdk_${!ARCHNAME}_linux_hotspot_11.0.28_6.tar.gz
    echo "${!CHECKSUM} /tmp/jdk11.tar.gz" | sha256sum --check --status
    tar -C /opt/java --strip-components 1 -zxf /tmp/jdk11.tar.gz
    rm /tmp/jdk11.tar.gz
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
