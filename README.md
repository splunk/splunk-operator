# Splunk Operator for Kubernetes

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/splunk/splunk-operator)](https://pkg.go.dev/github.com/splunk/splunk-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/splunk/splunk-operator)](https://goreportcard.com/report/github.com/splunk/splunk-operator)
[![Coverage Status](https://coveralls.io/repos/github/splunk/splunk-operator/badge.svg?branch=main)](https://coveralls.io/github/splunk/splunk-operator?branch=main)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator?ref=badge_shield)

The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk administrators
to deploy and operate Splunk Enterprise deployments on Kubernetes. Packaged as a
container, it uses the
[operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
to manage Splunk-specific
[custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/),
handling the underlying Kubernetes objects for you.

## Documentation

- **Published docs site**: https://splunk.github.io/splunk-operator/
- **In-repo docs index**: [`docs/README.md`](docs/README.md)
- **Getting started**: [`docs/GettingStarted.md`](docs/GettingStarted.md)
- **Custom resources & operations**: [`docs/operate/`](docs/operate/)
- **Architecture & internals**: [`docs/develop/Architecture.md`](docs/develop/Architecture.md)

## Quick Start

Prerequisites and full setup are documented in
[`docs/develop/DevelopmentSetup.md`](docs/develop/DevelopmentSetup.md).

```bash
make help      # list all available targets
make build     # compile the operator binary
make test      # run unit tests (Ginkgo/Gomega, envtest)
make install   # install CRDs into the current cluster
make deploy IMG=<image> NAMESPACE=<ns> SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
```

>Starting with operator version 3.0.0, which includes support for Splunk Enterprise version 10.x, an additional Docker-Splunk specific parameter is required to start containers. This is a breaking change, and user action is required.
>Starting in 10.x image versions of Splunk Enterprise, license acceptance requires an additional SPLUNK_GENERAL_TERMS=--accept-sgt-current-at-splunk-com argument. This indicates that users have read and accepted the current/latest version of the Splunk General Terms, available at https://www.splunk.com/en_us/legal/splunk-general-terms.html as may be updated from time to time. Unless you have jointly executed with Splunk a negotiated version of these General Terms that explicitly supersedes this agreement, by accessing or using Splunk software, you are agreeing to the Splunk General Terms posted at the time of your access and use and acknowledging its applicability to the Splunk software. Please read and make sure you agree to the Splunk General Terms before you access or use this software. Only after doing so should you include the --accept-sgt-current-at-splunk-com flag to indicate your acceptance of the current/latest Splunk General Terms and launch this software. All examples below have been updated with this change.
>If you use the below examples and the ‘–accept-sgt-current-at-splunk-com’ flag, you are indicating that you have read and accepted the current/latest version of the Splunk General Terms, as may be updated from time to time, and acknowledging its applicability to this software - as noted above.

## Development & Contributing

- **Development setup**: [`docs/develop/DevelopmentSetup.md`](docs/develop/DevelopmentSetup.md)
- **Contributing guide**: [`docs/develop/Contributing.md`](docs/develop/Contributing.md)
- **AI agent guide (routing & ownership)**: [`AGENTS.md`](AGENTS.md)

## License

Apache License 2.0. See [`LICENSE`](LICENSE).

[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator?ref=badge_large)
