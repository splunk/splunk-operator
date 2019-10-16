# Splunk Operator for Kubernetes

This repository is used to build the
[Kubernetes operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/)
for Splunk. If you are just looking for documentation on how to deploy and use
the latest release, please see the [Getting Started Documentation](docs/README.md).


## Prerequisites 

This project now uses [Go modules](https://blog.golang.org/using-go-modules), which requires [golang](https://golang.org/doc/install) 1.12 or later.
You must `export GO111MODULE=on` if cloning these repositories into your `$GOPATH` (not recommended).

The [Kubernetes Operator SDK](https://github.com/operator-framework/operator-sdk) must also be installed to build this project.

```
git clone -b v0.10.0 https://github.com/operator-framework/operator-sdk
cd operator-sdk
make install
```


## Cloning this repository

```
git clone git@github.com:splunk/splunk-operator.git
cd splunk-operator
```


## Building the operator

You can build the operator by just running `make`.

Other make targets include (more info below):

* `make all`: builds the `splunk/splunk-operator` docker image (same as `make splunk-operator`)
* `make splunk-operator`: builds the `splunk/splunk-operator` docker image
* `make package`: generates tarball of the `splunk/splunk-operator` docker image and installation YAML file
* `make run`: runs the splunk operator locally, monitoring the Kubernetes cluster configured in your current `kubectl` context


## Pushing Your Splunk Operator Image

If you are using a local, single-node Kubernetes cluster like
[minikube](https://minikube.sigs.k8s.io/) or [Docker Desktop](https://www.docker.com/products/docker-desktop),
you only need to build the `splunk/splunk-operator` image.
You can skip the rest of this section.

If possible, we recommend re-tagging your custom-built images and pushing
them to a remote registry that your Kubernetes workers are able to pull from.
Please see the [Air Gap Documentation](docs/AirGap.md) for more information.


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