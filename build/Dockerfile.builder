# Note that go-toolset on UBI8 provides a FIPS-compatible compiler: https://developers.redhat.com/blog/2019/06/24/go-and-fips-140-2-on-red-hat-enterprise-linux/
FROM registry.access.redhat.com/ubi8/go-toolset

USER root

RUN dnf install -y git openssh tar gzip ca-certificates && dnf clean all

USER default

COPY --chown=default:root go.mod go.sum ${HOME}/

RUN go mod download && rm -rf go/pkg/mod/cache/vcs
