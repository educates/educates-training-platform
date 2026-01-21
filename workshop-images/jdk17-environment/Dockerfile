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
    CHECKSUM_amd64="166774efcf0f722f2ee18eba0039de2d685b350ee14d7b69e6f83437dafd2af1"
    CHECKSUM_arm64="423416447885d9e45f96dd9e0b2c1367da5e1b0353e187cfdf9388c9820ac147"
    CHECKSUM=CHECKSUM_${TARGETARCH}
    curl --fail -sL -o /tmp/jdk17.tar.gz https://github.com/adoptium/temurin17-binaries/releases/download/jdk-17.0.16%2B8/OpenJDK17U-jdk_${!ARCHNAME}_linux_hotspot_17.0.16_8.tar.gz
    echo "${!CHECKSUM} /tmp/jdk17.tar.gz" | sha256sum --check --status
    tar -C /opt/java --strip-components 1 -zxf /tmp/jdk17.tar.gz
    rm /tmp/jdk17.tar.gz
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
