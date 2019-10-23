FROM registry.access.redhat.com/ubi8/ubi-minimal:latest

ENV OPERATOR=/usr/local/bin/splunk-operator \
    USER_UID=1001 \
    USER_NAME=splunk-operator

LABEL name="splunk" \
      maintainer="support@splunk.com" \
      vendor="splunk" \
      version="0.0.4" \
      release="1" \
      summary="Simplify the Deployment & Management of Splunk Products on Kubernetes" \
      description="The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk Administrators to deploy and operate Enterprise deployments in a Kubernetes infrastructure. Packaged as a container, it uses the operator pattern to manage Splunk-specific custom resources, following best practices to manage all the underlying Kubernetes objects for you."

# install operator binary
COPY build/_output/bin/splunk-operator ${OPERATOR}

COPY build/bin /usr/local/bin
RUN mkdir /licenses \
    && curl -o /licenses/apache-2.0.txt https://www.apache.org/licenses/LICENSE-2.0.txt \
    && curl -o /licenses/EULA_Red_Hat_Universal_Base_Image_English_20190422.pdf https://www.redhat.com/licenses/EULA_Red_Hat_Universal_Base_Image_English_20190422.pdf \
    && /usr/local/bin/user_setup

ENTRYPOINT ["/usr/local/bin/entrypoint"]

USER ${USER_UID}
