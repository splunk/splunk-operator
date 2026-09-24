# Splunk Operator for Kubernetes

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/splunk/splunk-operator)](https://pkg.go.dev/github.com/splunk/splunk-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/splunk/splunk-operator)](https://goreportcard.com/report/github.com/splunk/splunk-operator)
[![Coverage Status](https://coveralls.io/repos/github/splunk/splunk-operator/badge.svg?branch=main)](https://coveralls.io/github/splunk/splunk-operator?branch=main)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator?ref=badge_shield)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/splunk/splunk-operator)

The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk
Administrators to deploy and operate Enterprise deployments in a Kubernetes
infrastructure. Packaged as a container, it uses the
[operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
to manage Splunk-specific [custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/),
following best practices to manage all the underlying Kubernetes objects for you.

This repository is used to build the Splunk
[Operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
for Kubernetes (SOK). If you are just looking for documentation on how to
deploy and use the latest release, please visit the published
[Splunk Operator documentation site](https://splunk.github.io/splunk-operator/)
or review the in-repo [Getting Started Documentation](GettingStarted.md).

## Splunk General Terms Acceptance

Starting with operator version 3.0.0, which includes support for Splunk Enterprise version 10.x, an additional Docker-Splunk specific parameter is required to start containers. **This is a breaking change, and user action is required.**

Starting in 10.x image versions of Splunk Enterprise, license acceptance requires an additional `SPLUNK_GENERAL_TERMS=--accept-sgt-current-at-splunk-com` argument. This indicates that users have read and accepted the current/latest version of the Splunk General Terms, available at https://www.splunk.com/en_us/legal/splunk-general-terms.html as may be updated from time to time. Unless you have jointly executed with Splunk a negotiated version of these General Terms that explicitly supersedes this agreement, by accessing or using Splunk software, you are agreeing to the Splunk General Terms posted at the time of your access and use and acknowledging its applicability to the Splunk software. Please read and make sure you agree to the Splunk General Terms before you access or use this software. Only after doing so should you include the `--accept-sgt-current-at-splunk-com` flag to indicate your acceptance of the current/latest Splunk General Terms and launch this software. All examples below have been updated with this change.

If you use the below examples and the ‘--accept-sgt-current-at-splunk-com’ flag, you are indicating that you have read and accepted the current/latest version of the Splunk General Terms, as may be updated from time to time, and acknowledging its applicability to this software - as noted above.

By default, the SPLUNK_GENERAL_TERMS environment variable will be set to an empty string. You must either manually update it to have the required additional value `--accept-sgt-current-at-splunk-com` in the splunk-operator-controller-manager deployment, or you can pass the `SPLUNK_GENERAL_TERMS` parameter with the required additional value to the `make deploy` command.

```
make deploy IMG=docker.io/splunk/splunk-operator:<tag name> WATCH_NAMESPACE="namespace1" RELATED_IMAGE_SPLUNK_ENTERPRISE="splunk/splunk:edge" SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
```

For more information about this change, see the [Splunk General Terms Migration Documentation](reference/SplunkGeneralTermsMigration.md).

## Development

If you are interested in building or contributing to the Splunk Operator, see the [Development Setup](develop/DevelopmentSetup.md) and [Contributing](develop/Contributing.md) guides.

Please see the [Getting Started Documentation](GettingStarted.md) for
information on how to install and use the operator in your cluster.


## License
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator?ref=badge_large)
