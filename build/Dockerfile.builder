FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV RELEASE_VERSION=v0.10.0
ENV KEY_ID=0CF50BEE7E4DF6445E08C0EA9AFDE59E90D2B445

# This installs golang toolset and operator-sdk
# Note that go-toolset on UBI8 will provide a FIPS-compatible compiler: https://developers.redhat.com/blog/2019/06/24/go-and-fips-140-2-on-red-hat-enterprise-linux/
# See also https://github.com/operator-framework/operator-sdk/blob/master/doc/user/install-operator-sdk.md
RUN microdnf install -y git go-toolset \
    && cd ${HOME} \
    && curl -LO https://github.com/operator-framework/operator-sdk/releases/download/${RELEASE_VERSION}/operator-sdk-${RELEASE_VERSION}-x86_64-linux-gnu \
    && curl -LO https://github.com/operator-framework/operator-sdk/releases/download/${RELEASE_VERSION}/operator-sdk-${RELEASE_VERSION}-x86_64-linux-gnu.asc \
    && gpg --recv-key "$KEY_ID" \
    && gpg --verify operator-sdk-${RELEASE_VERSION}-x86_64-linux-gnu.asc \
    && chmod +x operator-sdk-${RELEASE_VERSION}-x86_64-linux-gnu \
    && mkdir -p /usr/local/bin/ \
    && cp operator-sdk-${RELEASE_VERSION}-x86_64-linux-gnu /usr/local/bin/operator-sdk \
    && rm -f operator-sdk-${RELEASE_VERSION}-x86_64-linux-gnu

COPY go.mod go.sum ${HOME}/

RUN go mod download