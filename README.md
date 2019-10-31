# Splunk Operator for Kubernetes

[![License](https://img.shields.io/:license-apache-blue.svg)](http://www.apache.org/licenses/LICENSE-2.0.html)
[![Go Report Card](https://goreportcard.com/badge/github.com/splunk/splunk-operator)](https://goreportcard.com/report/github.com/splunk/splunk-operator)

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


## Pushing Your Splunk Operator Image

If you are using a local, single-node Kubernetes cluster like
[minikube](https://minikube.sigs.k8s.io/) or [Docker Desktop](https://www.docker.com/products/docker-desktop),
you only need to build the `splunk/splunk-operator` image.
You can skip the rest of this section.

If possible, we recommend re-tagging your custom-built images and pushing
them to a remote registry that your Kubernetes workers are able to pull from.
Please see the [Required Images Documentation](Images.md) for more information.


## Running the Splunk Operator

### Running as a foreground process

Use this to run the operator as a local foreground process on your machine:
```
make run
```
This will use your current Kubernetes context from `~/.kube/config`.


### Running in Local and Remote Clusters

You can install and start the operator by running
```
kubectl apply -f deploy/all-in-one.yaml
```

Note that `deploy/all-in-one.yaml` uses the image name `splunk/splunk-operator`.
If you pushed this image to a remote registry, you need to change the `image`
parameter in this file to refer to the correct location.

You can stop and remove the operator by running
```
kubectl delete -f deploy/all-in-one.yaml
```

Please see the [Getting Started Documentation](docs/README.md) for more
information.