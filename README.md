# Splunk Operator for Kubernetes

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/splunk/splunk-operator)](https://pkg.go.dev/github.com/splunk/splunk-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/splunk/splunk-operator)](https://goreportcard.com/report/github.com/splunk/splunk-operator)
[![Coverage Status](https://coveralls.io/repos/github/splunk/splunk-operator/badge.svg?branch=master)](https://coveralls.io/github/splunk/splunk-operator?branch=master)
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator?ref=badge_shield)

The Splunk Operator for Kubernetes (SOK) makes it easy for Splunk
Administrators to deploy and operate Enterprise deployments in a Kubernetes
infrastructure. Packaged as a container, it uses the
[operator pattern](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
to manage Splunk-specific [custom resources](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/custom-resources/),
following best practices to manage all the underlying Kubernetes objects for you.

This repository is used to build the Splunk
[Operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
for Kubernetes (SOK). If you are just looking for documentation on how to
deploy and use the latest release, please see the
[Getting Started Documentation](docs/README.md).

## Splunk General Terms Acceptance

Starting with operator version 3.0.0, which includes support for Splunk Enterprise version 10.x, an additional Docker-Splunk specific parameter is required to start containers. **This is a breaking change, and user action is required.**

Starting in 10.x image versions of Splunk Enterprise, license acceptance requires an additional `SPLUNK_GENERAL_TERMS=--accept-sgt-current-at-splunk-com` argument. This indicates that users have read and accepted the current/latest version of the Splunk General Terms, available at https://www.splunk.com/en_us/legal/splunk-general-terms.html as may be updated from time to time. Unless you have jointly executed with Splunk a negotiated version of these General Terms that explicitly supersedes this agreement, by accessing or using Splunk software, you are agreeing to the Splunk General Terms posted at the time ofyour access and use and acknowledging its applicability to the Splunk software. Please read and make sure you agree to the Splunk General Terms before you access or use this software. Only after doing so should you include the `--accept-sgt-current-at-splunk-com` flag to indicate your acceptance of the current/latest Splunk General Terms and launch this software. All examples below have been updated with this change.

If you use the below examples and the ‘accept-sgt-current-at-splunk-com’ flag, you are indicating that you have read and accepted the current/latest version of the Splunk General Terms, as may be updated from time to time, and acknowledging its applicability to this software - as noted above.

By default, the SPLUNK_GENERAL_TERMS environment variable will be set to an empty string. You must either manually update it to have the required additional value `--accept-sgt-current-at-splunk-com` in the splunk-operator-controller-manager deployment, or you can pass the `SPLUNK_GENERAL_TERMS` parameter with the required additional value to the `make deploy` command.

```
make deploy IMG=docker.io/splunk/splunk-operator:<tag name> WATCH_NAMESPACE="namespace1" RELATED_IMAGE_SPLUNK_ENTERPRISE="splunk/splunk:edge" SPLUNK_GENERAL_TERMS="--accept-sgt-current-at-splunk-com"
```

For more information about this change, see the [Splunk General Terms Migration Documentation](docs/SplunkGeneralTermsMigration.md).

## Prerequisites

You must have [Docker Engine](https://docs.docker.com/install/) installed to
build the Splunk Operator.

This project uses [Go modules](https://blog.golang.org/using-go-modules),
and requires [golang](https://golang.org/doc/install) 1.23.0 or later.
You must `export GO111MODULE=on` if cloning these repositories into your
`$GOPATH` (not recommended).

The [Kubernetes Operator SDK](https://github.com/operator-framework/operator-sdk)
must also be installed to build this project.

```
git clone -b v1.31.0 https://github.com/operator-framework/operator-sdk
cd operator-sdk
make tidy
make install
```

You may need to add `$GOPATH/bin` to your path to run the `operator-sdk`
command line tool:

```
export PATH=${PATH}:${GOPATH}/bin
```

It is also recommended that you install the following golang tools,
which are used by various `make` targets:

```shell
go install golang.org/x/lint/golint
go install golang.org/x/tools/cmd/cover
go install github.com/mattn/goveralls
go get -u github.com/mikefarah/yq/v3
go get -u github.com/go-delve/delve/cmd/dlv
```

## Cloning this repository

```shell
git clone git@github.com:splunk/splunk-operator.git
cd splunk-operator
```

## Repository overview

This repository consists of the following code used to build the splunk-operator binary:

* `main.go`: Provides the main() function, where everything begins
* `apis/`: Source code for the operator's custom resource definition types
* `controllers/`: Used to register controllers that watch for changes to custom resources
* `pkg/splunk/enterprise/`: Source code for controllers that manage Splunk Enterprise resources
* `pkg/splunk/controller/`: Common code shared across Splunk controllers
* `pkg/splunk/common/`: Common code used by most other splunk packages
* `pkg/splunk/client/`: Simple client for Splunk Enterprise REST API
* `pkg/splunk/test/`: Common code used by other packages for unit testing

`main()` uses `controllers` to register all the `enterprise` controllers
that manage custom resources by watching for Kubernetes events.
The `enterprise`  controllers are implemented using common code provided
by the `controllers` package. The `enterprise` controllers also use the REST API client
provided in the `pkg/splunk/client` package. The types provided by `apis/` and
common code in the `pkg/splunk/common/` package are used universally. Note that the
source code for `main()` is generated from a template provided by the Operator SDK.

In addition to the source code, this repository includes:

* `tools`: Build scripts, templates, etc. used to build the container image
* `config`: Kubernetes YAML templates used to install the Splunk Operator
* `docs`: Getting Started Guide and other documentation in Markdown format
* `test`: Integration test framework built using Ginko. See [docs](test/README.md) for more info.

## Building the operator

You can build the operator by just running `make`.

Other make targets include (more info below):

* `make all`: builds `manager` executable
* `make test`: Runs unit tests with Coveralls code coverage output to coverage.out
* `make scorecard`: Runs operator-sdk scorecard tests using OLM installation bundle
* `make generate`: runs operator-generate k8s, crds and csv commands, updating installation YAML files and OLM bundle
* `make docker-build`: generates `splunk-operator` container image  example `make docker-build IMG=docker.io/splunk/splunk-operator:<tag name>`
* `make docker-buildx`: generates `splunk-operator` container image  for multiple platforms, example `make docker-buildx IMG=docker.io/splunk/splunk-operator:<tag name>`
* `make docker-push`: push docker image to given repository example `make docker-push IMG=docker.io/splunk/splunk-operator:<tag name>`
* `make clean`: removes the binary build output and `splunk-operator` container image example `make docker-push IMG=docker.io/splunk/splunk-operator:<tag name>`
* `make run`: runs the Splunk Operator locally, monitoring the Kubernetes cluster configured in your current `kubectl` context
* `make fmt`: runs `go fmt` on all `*.go` source files in this project
* `make bundle-build`: generates `splunk-operator-bundle` bundle container image for OLM example `make bundle-build IMAGE_TAG_BASE=docker.io/splunk/splunk-operator VERSION=<tag name>  IMG=docker.io/splunk/splunk-operator:<tag name>`
* `make bundle-push`: push OLM bundle docker image to given repository example `make bundle-push IMAGE_TAG_BASE=docker.io/splunk/splunk-operator VERSION=<tag name> IMG=docker.io/splunk/splunk-operator:<tag name>`
* `make catalog-build`: generates `splunk-operator-catalog` catalog container image example `make catalog-build IMAGE_TAG_BASE=docker.io/splunk/splunk-operator VERSION=<tag name> IMG=docker.io/splunk/splunk-operator:<tag name>`
* `make catalog-push`: push catalog docker image to given repository example`make catalog-push IMAGE_TAG_BASE=docker.io/splunk/splunk-operator VERSION=<tag name> IMG=docker.io/splunk/splunk-operator:<tag name>`

## Deploying the Splunk Operator
`make deploy` command will deploy all the necessary resources to run Splunk Operator like RBAC policies, services, configmaps, deployment. Operator will be installed in `splunk-operator` namespace. If `splunk-operator` namespace does not exist, it will create the namespace. By default `make deploy` will install operator clusterwide. Operator will watch all the namespaces for any splunk enterprise custom resources.

```shell
make deploy IMG=docker.io/splunk/splunk-operator:<tag name>
```

If you want operator for specific namespace then you must pass `WATCH_NAMESPACE` parameter to `make deploy` command

```
make deploy IMG=docker.io/splunk/splunk-operator:<tag name> WATCH_NAMESPACE="namespace1"
```

If you want operator to use specific version of splunk instance, then you must pass `RELATED_IMAGE_SPLUNK_ENTERPRISE` parameter to `make deploy` command

```
make deploy IMG=docker.io/splunk/splunk-operator:<tag name> WATCH_NAMESPACE="namespace1" RELATED_IMAGE_SPLUNK_ENTERPRISE="splunk/splunk:edge"
```

Use this to run the operator as a local foreground process on your machine:

```shell
make run
```

This will use your current Kubernetes context from `~/.kube/config` to manage
resources in your current namespace.

Please see the [Getting Started Documentation](docs/README.md) for more
information, including instructions on how to install the operator in your
cluster.


## License
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2Fsplunk%2Fsplunk-operator?ref=badge_large)