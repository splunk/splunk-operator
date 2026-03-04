FROM mcr.microsoft.com/devcontainers/base:bookworm

ARG GO_VERSION=1.25.7
ARG KUBECTL_VERSION=v1.29.1
ARG OPERATOR_SDK_VERSION=v1.42.0
ARG SKAFFOLD_VERSION=v2.16.1
ARG KIND_VERSION=v0.29.0

RUN apt-get update && \
    apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      git \
      jq \
      make \
      python3 \
      python3-pip \
      shellcheck \
      unzip && \
    rm -rf /var/lib/apt/lists/*

RUN curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C /usr/local -xz
ENV PATH="/usr/local/go/bin:${PATH}"

RUN curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" && \
    chmod +x /usr/local/bin/kubectl

RUN curl -fsSLo /usr/local/bin/operator-sdk "https://github.com/operator-framework/operator-sdk/releases/download/${OPERATOR_SDK_VERSION}/operator-sdk_linux_amd64" && \
    chmod +x /usr/local/bin/operator-sdk

RUN curl -fsSLo /usr/local/bin/skaffold "https://storage.googleapis.com/skaffold/releases/${SKAFFOLD_VERSION}/skaffold-linux-amd64" && \
    chmod +x /usr/local/bin/skaffold

RUN curl -fsSLo /usr/local/bin/kind "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-amd64" && \
    chmod +x /usr/local/bin/kind
