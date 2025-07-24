# Setup defaults for build arguments
ARG PLATFORMS=linux/amd64

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
# This sha relates to ubi minimal version 8.10-1295.1749680713, which is tagged as 8.10 and latest as of Jun 23, 2025
ARG BASE_IMAGE=registry.access.redhat.com/ubi8/ubi-minimal@sha256
ARG BASE_IMAGE_VERSION=3b0f20d81f5fc0dfb3f96cbe9912e02959d1e508411e0e46fad52520208a651c

# Build the manager binary
FROM golang:1.24.2 AS builder

WORKDIR /workspace

# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# Cache dependencies before building and copying source to reduce re-downloading
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/controller/ internal/controller/
COPY pkg/ pkg/
COPY tools/ tools/
COPY hack hack/

# Build
# TARGETOS and TARGETARCH are provided(inferred) by buildx via the --platforms flag
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Use BASE_IMAGE as the base image
FROM ${BASE_IMAGE}:${BASE_IMAGE_VERSION}

ENV OPERATOR=/manager \
    USER_UID=1001 \
    USER_NAME=nonroot

# Install necessary packages and configure user
RUN if grep -q 'Ubuntu' /etc/os-release; then \
        apt-get update && \
        apt-get install -y --no-install-recommends passwd && \
        apt-get install -y --no-install-recommends krb5-locales && \
        apt-get install -y --no-install-recommends unattended-upgrades && \
        useradd -ms /bin/bash nonroot -u 1001 && \
        apt-get install -y --no-install-recommends ca-certificates && \
        update-ca-certificates && \
        unattended-upgrades -v && \
        apt-get clean && rm -rf /var/lib/apt/lists/*; \
    elif grep -q 'Amazon Linux' /etc/os-release; then \
        yum -y install shadow-utils && \
        useradd -ms /bin/bash nonroot -u 1001 && \
        yum install -y ca-certificates && \
        update-ca-trust &&  \
        yum update -y krb5-libs && yum clean all && \
        yum -y update-minimal --security --sec-severity=Important --sec-severity=Critical && \
        yum -y update-minimal --security --sec-severity=Moderate && \
        yum -y update-minimal --security --sec-severity=Low; \
    else \
        microdnf -y install shadow-utils && \
        useradd -ms /bin/bash nonroot -u 1001 && \
        microdnf install -y ca-certificates && \
        update-ca-trust &&  \
        microdnf update -y krb5-libs && \
        microdnf update -y libstdc++ && \
        microdnf update -y libxml2 && \
        microdnf update -y libgcc && \
        microdnf clean all; \
    fi

# Metadata
LABEL name="splunk" \
      maintainer="support@splunk.com" \
      vendor="splunk" \
      version="2.8.1" \
      release="1" \
      summary="Simplify the Deployment & Management of Splunk Products on Kubernetes" \
      description="The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk Administrators to deploy and operate Enterprise deployments in a Kubernetes infrastructure. Packaged as a container, it uses the operator pattern to manage Splunk-specific custom resources, following best practices to manage all the underlying Kubernetes objects for you."

# Set up workspace
WORKDIR /
RUN mkdir /licenses && \
    mkdir -p /tools/k8_probes

# Copy necessary files from the builder stage and other resources
COPY --from=builder /workspace/manager .
COPY tools/EULA_Red_Hat_Universal_Base_Image_English_20190422.pdf /licenses
COPY LICENSE /licenses/LICENSE-2.0.txt
COPY tools/k8_probes/livenessProbe.sh /tools/k8_probes/
COPY tools/k8_probes/readinessProbe.sh /tools/k8_probes/
COPY tools/k8_probes/startupProbe.sh /tools/k8_probes/

# Set the user
USER 1001

# Start the manager
ENTRYPOINT ["/manager"]