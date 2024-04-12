# Build the manager binary
FROM golang:1.17 as builder

#RUN yum -y install shadow-utils

ENV OPERATOR=/manager \
    USER_UID=1001  

LABEL name="splunk" \
      maintainer="support@splunk.com" \
      vendor="splunk" \
      version="1.1.0" \
      release="1" \
      summary="Simplify the Deployment & Management of Splunk Products on Kubernetes" \
      description="The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk Administrators to deploy and operate Enterprise deployments in a Kubernetes infrastructure. Packaged as a container, it uses the operator pattern to manage Splunk-specific custom resources, following best practices to manage all the underlying Kubernetes objects for you."

WORKDIR /
# Copy the Go Modules manifests
COPY . .

# Build
RUN CGO_ENABLED=0 go get -ldflags "-s -w -extldflags '-static'" github.com/go-delve/delve/cmd/dlv
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -gcflags="all=-N -l" -a -o manager main.go

FROM debian:stable

LABEL name="splunk" \
      maintainer="support@splunk.com" \
      vendor="splunk" \
      version="1.1.0" \
      release="1" \
      summary="Simplify the Deployment & Management of Splunk Products on Kubernetes" \
      description="The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk Administrators to deploy and operate Enterprise deployments in a Kubernetes infrastructure. Packaged as a container, it uses the operator pattern to manage Splunk-specific custom resources, following best practices to manage all the underlying Kubernetes objects for you."


RUN mkdir /.config 
RUN chmod -R 0777 /.config 
ARG USERNAME=nonroot
ARG USER_UID=1000
ARG USER_GID=$USER_UID

# Create the user
RUN groupadd --gid $USER_GID $USERNAME \
    && useradd --uid $USER_UID --gid $USER_GID -m $USERNAME \
    #
    # [Optional] Add sudo support. Omit if you don't need to install software after connecting.
    && apt-get update \
    && apt-get install -y sudo \
    && apt-get install -y git \
    && apt-get install -y curl \
    && apt-get install -y procps \ 
    && apt install -y wget \
    && curl -Lo skaffold https://storage.googleapis.com/skaffold/releases/latest/skaffold-linux-amd64 \
    && sudo install skaffold /usr/local/bin/ \
    && echo $USERNAME ALL=\(root\) NOPASSWD:ALL > /etc/sudoers.d/$USERNAME \
    && chmod 0440 /etc/sudoers.d/$USERNAME

ENV GOVERSION="1.17.7" \
    GOPATH=$HOME/go \
    PATH=/usr/local/go/bin:$PATH:$GOPATH/bin:/

RUN wget "https://golang.org/dl/go${GOVERSION}.linux-amd64.tar.gz" -4
RUN tar -C /usr/local -xvf "go${GOVERSION}.linux-amd64.tar.gz"

WORKDIR /
COPY . .
COPY --from=builder /go/bin/dlv dlv
COPY --from=builder /manager  manager


USER $USERNAME
ENTRYPOINT [ "/dlv" , "--listen=:40000", "--headless=true", "--api-version=2", "--accept-multiclient", "exec", "manager" ]