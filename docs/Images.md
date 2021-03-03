# Required Docker Images

The Splunk operator requires three docker images to be present or available
to your Kubernetes cluster:

* `splunk/splunk-operator`: The Splunk Operator image (built by this repository)
* `splunk/splunk:8.1.0`: The [Splunk Enterprise image](https://github.com/splunk/docker-splunk) (8.1.0 or later)

All of these images are publicly available on [Docker Hub](https://hub.docker.com/).
If your cluster does not have access to pull from Docker Hub, you will need to
manually download and push these images to an accessible registry. You will
also need to specify the location of these images by using either an environment
variable passed to the operator or adding additional `spec` parameters to your 
custom resource definition.

Use the `RELATED_IMAGE_SPLUNK_ENTERPRISE` environment variable or the `image`
custom resource parameter to change the location of the Splunk Enterprise
image. Please see the
[Advanced Installation Instructions](Install.md) or
[Custom Resource Guide](CustomResources.md) for more details.


## Using a Private Registry

If your Kubernetes workers have access to pull from a Private registry, it is
easiest to retag and push the required images to directly to the private registry.

For example, users of Amazon’s Elastic Container Registry may do something
like the following:

```
$(aws ecr get-login --no-include-email --region us-west-2)

docker tag splunk/splunk-operator:latest 111000.dkr.ecr.us-west-2.amazonaws.com/splunk/splunk-operator:latest

docker push 111000.dkr.ecr.us-west-2.amazonaws.com/splunk/splunk-operator:latest
```

(Note that you need to replace "111000" with your account number, and
"us-west-2" with your region)

Users of Google Kubernetes Engine may do something like the following:

```
gcloud auth configure-docker

docker tag splunk/splunk-operator:latest gcr.io/splunk-operator-testing/splunk-operator:latest

docker push gcr.io/splunk-operator-testing/splunk-operator:latest
```

(Note that you need to replace "splunk-operator-testing" with the name of your GKE cluster)


## Manually Exporting and Importing Images

Another option is to export each of the required images to a tarball, transfer
the tarball to each of your Kubernetes workers (perhaps using something like
Ansible, Puppet or Chef), and import the images on your workers.

For example, you can use the following to export the `splunk/splunk-operator`
image to a tarball:

```
docker image save splunk/splunk-operator:latest | gzip -c > splunk-operator.tar.gz
```

On your Kubernetes workers, you can then import this image using:

```
docker load -i splunk-operator.tar.gz
```


## Simple Script to Push Images

`build/push_images.sh` is a simple script that makes it easier to push docker
images to multiple remote hosts, using the SSH protocol.

Create a file in your current working directory named `push_targets`. This
file should include every host that you want to push images to, one user@host
on each line. For example:

```
ubuntu@myvm1.splunk.com
ubuntu@myvm2.splunk.com
ubuntu@myvm3.splunk.com
```

This script takes one argument, the name of a container, and pushes it to
all the entries in `push_targets`. For example, you can push the
`splunk/splunk-operator` image to each of these nodes by running

```
./build/push_images.sh splunk/splunk-operator
```
