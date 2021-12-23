# Required Docker Images

The Splunk Operator requires these docker images to be present or available to your Kubernetes cluster:

* `splunk/splunk-operator`: The Splunk Operator image built by this repository or the [official release](https://hub.docker.com/r/splunk/splunk-operator) (1.0.5 or later)
* `splunk/splunk:<version>`: The [Splunk Enterprise image](https://github.com/splunk/docker-splunk) (8.2.3.3 or later)

All of these images are publicly available, and published on [Docker Hub](https://hub.docker.com/).

If your cluster does not have access to pull directly from Docker Hub, you will need to manually download and push these images to an accessible registry. You will also need to specify the location of these images by using an environment variable passed to the Operator, or by adding additional `spec` parameters to your 
custom resource definition.

Use the `RELATED_IMAGE_SPLUNK_ENTERPRISE` environment variable or the `image` custom resource parameter to change the location of your Splunk Enterprise image. 

For additional detail, see the [Advanced Installation Instructions](Install.md) page, and the [Custom Resource Guide](CustomResources.md) page.


## Using a private registry

If your Kubernetes workers have access to pull from a private registry, it is easy to retag and push the required images to directly to your private registry.

An example of tagging with an Amazon Elastic Container Registry: 

```
$(aws ecr get-login --no-include-email --region us-west-2)

docker tag splunk/splunk-operator:latest 111000.dkr.ecr.us-west-2.amazonaws.com/splunk/splunk-operator:latest

docker push 111000.dkr.ecr.us-west-2.amazonaws.com/splunk/splunk-operator:latest
```

Note that you need to replace "111000" with your account number, and "us-west-2" with your region.

An example of tagging using the Google Kubernetes Engine:

```
gcloud auth configure-docker

docker tag splunk/splunk-operator:latest gcr.io/splunk-operator-testing/splunk-operator:latest

docker push gcr.io/splunk-operator-testing/splunk-operator:latest
```

Note that you need to replace "splunk-operator-testing" with the name of your GKE cluster.


## Manually exporting and importing images

Another option is to export each of the required images as a tarball, transfer the tarball to each of your Kubernetes workers using a tool such as Ansible, Puppet, or Chef, and import the images on your workers.

For example, you can export the `splunk/splunk-operator`image to a tarball:

```
docker image save splunk/splunk-operator:latest | gzip -c > splunk-operator.tar.gz
```

And on your Kubernetes workers, you can import the tarball using:

```
docker load -i splunk-operator.tar.gz
```


## A simple script to push images

The script `build/push_images.sh`  is included to push Docker images to multiple remote hosts using SSH. The script takes the name of a container and an image path, and pushes the image to all the entries in `push_targets`. 

To use the script:

1. Create a file in your current working directory named `push_targets`. This file should include every host that you want to push images to, one `user@host` on each line. For example:

```
ubuntu@myvm1.splunk.com
ubuntu@myvm2.splunk.com
ubuntu@myvm3.splunk.com
```

2. Run the script with the image path. For example, you can push the `splunk/splunk-operator` image to each of these nodes by running:

```
./build/push_images.sh splunk/splunk-operator
```
