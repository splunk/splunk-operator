# Splunk Operator for Kubernetes

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)
[![GoDoc](https://godoc.org/github.com/splunk/splunk-operator?status.svg)](https://godoc.org/github.com/splunk/splunk-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/splunk/splunk-operator)](https://goreportcard.com/report/github.com/splunk/splunk-operator)
[![CircleCI](https://circleci.com/gh/splunk/splunk-operator/tree/develop.svg?style=shield)](https://circleci.com/gh/splunk/splunk-operator/tree/develop)
[![Coverage Status](https://coveralls.io/repos/github/splunk/splunk-operator/badge.svg?branch=develop)](https://coveralls.io/github/splunk/splunk-operator?branch=develop)

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

This project now uses [Go modules](https://blog.golang.org/using-go-modules),
which requires [golang](https://golang.org/doc/install) 1.13 or later.
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


## Cloning this repository

```
git clone git@github.com:splunk/splunk-operator.git
cd splunk-operator
```


## Repository overview

This repository consists of the following code used to build the splunk-operator binary:

* `cmd/manager/main.go`: Provides the main() function, where everything begins
* `pkg/apis/`: Source code for the operator's custom resource definition types
* `pkg/controllers/`: Source code for CRD controllers that watch for changes
* `pkg/splunk/deploy/`: Source code the controllers use to interact with Kubernetes APIs
* `pkg/splunk/enterprise/`: Source code for managing Splunk Enterprise deployments
* `pkg/splunk/spark/`: Source code for managing Spark cluster deployments
* `pkg/splunk/resources/`: Generic utility code used by other splunk modules

`main()` basically just instantiates the `controllers`, and the `controllers` call
into the `deploy` module to perform actions. The `deploy` module uses the `enterprise`
and `spark` modules. The types provided by `pkg/apis/` and generic utility code in
`pkg/splunk/resources/` are used universally. Note that the source code for `main()`,
`pkg/apis` and `pkg/controllers` are all generated from templates provided by the
Operator SDK.

In addition to the source code, this repository includes:

* `build`: Build scripts, templates, etc. used to build the container image
* `deploy`: Kubernetes YAML templates used to install the Splunk Operator
* `docs`: Getting Started Guide and other documentation in Markdown format


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
* `make generate`: runs operator-generate k8s and crds commands, updating installation YAML files
* `make package`: generates tarball of the `splunk/splunk-operator` container image and installation YAML file
* `make clean`: removes the binary build output and `splunk/splunk-operator` container image
* `make run`: runs the splunk operator locally, monitoring the Kubernetes cluster configured in your current `kubectl` context
* `make fmt`: runs `go fmt` on all `*.go` source files in this project
* `make lint`: runs the `golint` utility on all `*.go` source files in this project


## Running the Splunk Operator

Ensure that you have the `SplunkEnterprise` Custom Resource Definition installed
in your cluster:

```
kubectl apply -f deploy/crds/enterprise_v1alpha1_splunkenterprise_crd.yaml
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
