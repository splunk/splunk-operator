# Splunk Operator for Kubernetes

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)
[![GoDoc](https://godoc.org/github.com/splunk/splunk-operator?status.svg)](https://godoc.org/github.com/splunk/splunk-operator)
[![Go Report Card](https://goreportcard.com/badge/github.com/splunk/splunk-operator)](https://goreportcard.com/report/github.com/splunk/splunk-operator)
[![CircleCI](https://circleci.com/gh/splunk/splunk-operator/tree/develop.svg?style=shield)](https://circleci.com/gh/splunk/splunk-operator/tree/develop)

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
which requires [golang](https://golang.org/doc/install) 1.12 or later.
You must `export GO111MODULE=on` if cloning these repositories into your
`$GOPATH` (not recommended).

The [Kubernetes Operator SDK](https://github.com/operator-framework/operator-sdk)
must also be installed to build this project.

```
git clone -b v0.10.0 https://github.com/operator-framework/operator-sdk
cd operator-sdk
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


## Building the operator

You can build the operator by just running `make`.

Other make targets include (more info below):

* `make all`: builds the `splunk/splunk-operator` docker image (same as `make image`)
* `make image`: builds the `splunk/splunk-operator` docker image
* `make package`: generates tarball of the `splunk/splunk-operator` docker image and installation YAML file
* `make local`: builds the splunk-operator-local binary for test and debugging purposes
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
