FROM golang:1.22-alpine AS build
WORKDIR /src
COPY main.go .
RUN CGO_ENABLED=0 go build -o /splunk-stub .

FROM alpine:3.19
RUN apk add --no-cache bash coreutils && \
    mkdir -p /opt/splunk/bin /opt/container_artifact && \
    echo "started" > /opt/container_artifact/splunk-container.state

# Fake splunk CLI that succeeds for any subcommand (maintenance-mode, etc.)
RUN printf '#!/bin/bash\necho "stub: $@"\nexit 0\n' > /opt/splunk/bin/splunk && \
    chmod +x /opt/splunk/bin/splunk

COPY --from=build /splunk-stub /usr/local/bin/splunk-stub

ENV SPLUNK_HOME=/opt/splunk
ENV CONTAINER_ARTIFACT_DIR=/opt/container_artifact

EXPOSE 8089 8088 9997 8000

ENTRYPOINT ["splunk-stub"]
