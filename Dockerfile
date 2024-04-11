# Build the manager binary
FROM --platform=${BUILDPLATFORM:-linux/amd64}  golang:1.21.1 as builder

ARG TARGETPLATFORM
ARG BUILDPLATFORM
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY main.go main.go
COPY api/ api/
COPY controllers/ controllers/
COPY pkg/ pkg/
COPY tools/ tools/
COPY hack hack/

# Build
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM public.ecr.aws/amazonlinux/amazonlinux:2023

ENV OPERATOR=/manager \
    USER_UID=1001 \
    USER_NAME=nonroot

RUN yum -y install shadow-utils
RUN useradd -ms /bin/bash nonroot -u 1001
RUN yum update -y krb5-libs && yum clean all
RUN yum -y update-minimal --security --sec-severity=Important --sec-severity=Critical
RUN yum -y update-minimal --security --sec-severity=Moderate
RUN yum -y update-minimal --security --sec-severity=Low

LABEL name="splunk" \
      maintainer="support@splunk.com" \
      vendor="splunk" \
      version="2.2.1" \
      release="1" \
      summary="Simplify the Deployment & Management of Splunk Products on Kubernetes" \
      description="The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk Administrators to deploy and operate Enterprise deployments in a Kubernetes infrastructure. Packaged as a container, it uses the operator pattern to manage Splunk-specific custom resources, following best practices to manage all the underlying Kubernetes objects for you."

WORKDIR /
RUN mkdir /licenses
RUN mkdir -p /tools/k8_probes

COPY --from=builder /workspace/manager .
COPY tools/EULA_Red_Hat_Universal_Base_Image_English_20190422.pdf /licenses
COPY LICENSE /licenses/LICENSE-2.0.txt
COPY tools/k8_probes/livenessProbe.sh /tools/k8_probes/
COPY tools/k8_probes/readinessProbe.sh /tools/k8_probes/
COPY tools/k8_probes/startupProbe.sh /tools/k8_probes/

USER 1001

ENTRYPOINT ["/manager"]
