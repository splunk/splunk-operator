# Build the manager binary
FROM golang:1.17 as builder

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
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -a -o manager main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM registry.access.redhat.com/ubi8/ubi:latest

ENV OPERATOR=/manager \
    USER_UID=1001 \
    USER_NAME=nonroot

RUN yum -y install shadow-utils
RUN useradd -ms /bin/bash nonroot -u 1001
RUN yum -y update-minimal --security --sec-severity=Important --sec-severity=Critical
RUN yum -y update-minimal --security --sec-severity=Moderate
RUN yum -y update-minimal --security --sec-severity=Low

LABEL name="splunk" \
      maintainer="support@splunk.com" \
      vendor="splunk" \
      version="1.1.0" \
      release="1" \
      summary="Simplify the Deployment & Management of Splunk Products on Kubernetes" \
      description="The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk Administrators to deploy and operate Enterprise deployments in a Kubernetes infrastructure. Packaged as a container, it uses the operator pattern to manage Splunk-specific custom resources, following best practices to manage all the underlying Kubernetes objects for you."

WORKDIR /
RUN mkdir /licenses 

COPY --from=builder /workspace/manager .
COPY tools/EULA_Red_Hat_Universal_Base_Image_English_20190422.pdf /licenses
COPY LICENSE /licenses/LICENSE-2.0.txt

USER 1001

ENTRYPOINT ["/manager"]
