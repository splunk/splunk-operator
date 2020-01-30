# Note that go-toolset on UBI8 provides a FIPS-compatible compiler: https://developers.redhat.com/blog/2019/06/24/go-and-fips-140-2-on-red-hat-enterprise-linux/
# From https://access.redhat.com/containers/?tab=overview#/registry.access.redhat.com/ubi8/go-toolset
FROM registry.access.redhat.com/ubi8/go-toolset:1.12.8-18

USER root

ENV OPERATOR_SDK_VERSION 0.10.0
ENV OPERATOR_SDK_URL https://github.com/operator-framework/operator-sdk/releases/download/v${OPERATOR_SDK_VERSION}/operator-sdk-v${OPERATOR_SDK_VERSION}-x86_64-linux-gnu
ENV DOCKER_CLI_URL https://download.docker.com/linux/centos/7/x86_64/stable/Packages/docker-ce-cli-19.03.5-3.el7.x86_64.rpm

RUN dnf install -y git openssh tar gzip ca-certificates \
    && dnf clean all \
    && wget -O /usr/local/bin/operator-sdk ${OPERATOR_SDK_URL} \
    && chmod a+x /usr/local/bin/operator-sdk \
    && wget -O /tmp/docker-cli.rpm ${DOCKER_CLI_URL} \
    && rpm -ivh /tmp/docker-cli.rpm \
    && rm /tmp/docker-cli.rpm

USER default

COPY --chown=default:root go.mod go.sum ${HOME}/initcache/

ENV GOBIN /opt/app-root/bin

RUN mkdir -p ${GOBIN} \
    && go get -u golang.org/x/lint/golint \
    && go get -u golang.org/x/tools/cmd/cover \
    && go get -u github.com/mattn/goveralls \
    && cd ${HOME}/initcache \
    && go mod download \
    && rm -rf ${HOME}/go/pkg/mod/cache/vcs
