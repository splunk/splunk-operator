# Splunk Operator for Kubernetes

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/splunk/splunk-operator)](https://pkg.go.dev/github.com/splunk/splunk-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/splunk/splunk-operator)](https://goreportcard.com/report/github.com/splunk/splunk-operator)
[![CircleCI](https://circleci.com/gh/splunk/splunk-operator/tree/master.svg?style=shield)](https://circleci.com/gh/splunk/splunk-operator/tree/master)
[![Coverage Status](https://coveralls.io/repos/github/splunk/splunk-operator/badge.svg?branch=master)](https://coveralls.io/github/splunk/splunk-operator?branch=master)

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


## Prerequisites

You must have [Docker Engine](https://docs.docker.com/install/) installed to
build the Splunk Operator.

This project uses [Go modules](https://blog.golang.org/using-go-modules),
and requires [golang](https://golang.org/doc/install) 1.13 or later.
You must `export GO111MODULE=on` if cloning these repositories into your
`$GOPATH` (not recommended).

The [Kubernetes Operator SDK](https://github.com/operator-framework/operator-sdk)
must also be installed to build this project.

```
git clone -b v0.15.1 https://github.com/operator-framework/operator-sdk
cd operator-sdk
make tidy
make install
```

You may need to add `$GOPATH/bin` to you path to run the `operator-sdk`
command line tool:

```
export PATH=${PATH}:${GOPATH}/bin
```

It is also recommended that you install the following golang tools,
which are used by various `make` targets:

```
go get -u golang.org/x/lint/golint
go get -u golang.org/x/tools/cmd/cover
go get -u github.com/mattn/goveralls
go get -u github.com/mikefarah/yq/v3
go get -u github.com/go-delve/delve/cmd/dlv
```


## Cloning this repository

```
git clone git@github.com:splunk/splunk-operator.git
cd splunk-operator
```


## Repository overview

This repository consists of the following code used to build the splunk-operator binary:

* `cmd/manager/main.go`: Provides the main() function, where everything begins
* `pkg/apis/`: Source code for the operator's custom resource definition types
* `pkg/controllers/`: Used to register controllers that watch for changes to custom resources
* `pkg/splunk/enterprise/`: Source code for controllers that manage Splunk Enterprise resources
* `pkg/splunk/spark/`: Source code for controllers that manage Spark resources
* `pkg/splunk/controller/`: Common code shared across Splunk controllers
* `pkg/splunk/common/`: Common code used by most other splunk packages
* `pkg/splunk/client/`: Simple client for Splunk Enterprise REST API
* `pkg/splunk/test/`: Common code used by other packages for unit testing

`main()` uses `pkg/controllers` to register all the `enterprise` and `spark`
controllers that manage custom resources by watching for Kubernetes events.
The `enterprise` and `spark` controllers are implemented using common code provided
by the `controllers` package. The `enterprise` controllers also use the REST API client
provided in the `pkg/splunk/client` package. The types provided by `pkg/apis/` and
common code in the `pkg/splunk/common/` package are used universally. Note that the
source code for `main()` is generated from a template provided by the Operator SDK.

In addition to the source code, this repository includes:

* `build`: Build scripts, templates, etc. used to build the container image
* `deploy`: Kubernetes YAML templates used to install the Splunk Operator
* `docs`: Getting Started Guide and other documentation in Markdown format
* `test`: Integration test framework built using Ginko. See [docs](test/README.md) for more info.


## Building the operator

You can build the operator by just running `make`.

Other make targets include (more info below):

* `make all`: builds `splunk/splunk-operator` container image (same as `make image`)
* `make builder`: builds the `splunk/splunk-operator-builder` container image
* `make builder-image`: builds `splunk/splunk-operator` using the `splunk/splunk-operator-builder` image
* `make builder-test`: Runs unit tests using the `splunk/splunk-operator-builder` image
* `make image`: builds the `splunk/splunk-operator` container image without using `splunk/splunk-operator-builder`
* `make local`: builds the splunk-operator-local binary for test and debugging purposes
* `make test`: Runs unit tests with Coveralls code coverage output to coverage.out
* `make scorecard`: Runs operator-sdk scorecard tests using OLM installation bundle
* `make generate`: runs operator-generate k8s, crds and csv commands, updating installation YAML files and OLM bundle
* `make package`: generates tarball of the `splunk/splunk-operator` container image and installation YAML file
* `make clean`: removes the binary build output and `splunk/splunk-operator` container image
* `make run`: runs the splunk operator locally, monitoring the Kubernetes cluster configured in your current `kubectl` context
* `make fmt`: runs `go fmt` on all `*.go` source files in this project
* `make lint`: runs the `golint` utility on all `*.go` source files in this project


## Running the Splunk Operator

Ensure that you have the Custom Resource Definitions installed in your cluster:

```
kubectl apply -f deploy/crds
```

Use this to run the operator as a local foreground process on your machine:

```
make run
```

This will use your current Kubernetes context from `~/.kube/config` to manage
resources in your current namespace.

Please see the [Getting Started Documentation](docs/README.md) for more
information, including instructions on how to install the operator in your
cluster.
