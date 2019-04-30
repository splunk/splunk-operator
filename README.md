# splunk-operator

This repository is used to build the [Kubernetes operator](https://coreos.com/operators/) for Splunk.

## Vendor Dependencies

This project uses [dep](https://github.com/golang/dep) to manage dependencies. On MacOS, you can install `dep` using Homebrew:

```
$ brew install dep
$ brew upgrade dep
```

On other platforms you can use the `install.sh` script:

```
$ curl https://raw.githubusercontent.com/golang/dep/master/install.sh | sh
```


## Kubernetes Operator SDK

The Kubernetes [Operator SDK](https://github.com/operator-framework/operator-sdk) must also be installed to build this project.

```
$ mkdir -p $GOPATH/src/github.com/operator-framework
$ cd $GOPATH/src/github.com/operator-framework
$ git clone https://github.com/operator-framework/operator-sdk
$ cd operator-sdk
$ git checkout master
$ make dep
$ make install
```


## Cloning this repository

This repository should be cloned into your `~/go/src/git.splunk.com` directory:
```
$ mkdir -p ~/go/src/git.splunk.com
$ cd ~/go/src/git.splunk.com
$ git clone ssh://git@git.splunk.com:7999/tools/splunk-operator.git
$ cd splunk-operator
```


## Building the operator

You can build the operator by just running
```
$ make splunk-operator
```

Other make targets include (more info below):

* `make all`: builds the `splunk-operator` docker image (same as `make splunk-operator`)
* `make dep`: checks all vendor dependencies and ensures they are up to date
* `make splunk-operator`: builds the `splunk-operator` docker image
* `make splunk-dfs`: builds the `splunk-dfs` docker image (requires `splunk-debian-9`)
* `make splunk-spark`: builds the `splunk-spark` docker image
* `make push-operator`: pushes the `splunk-operator` docker image to all `push_targets`
* `make push-dfs`: pushes the `splunk-dfs` docker image to all `push_targets`
* `make push-spark`: pushes the `splunk-spark` docker image to all `push_targets`
* `make push`: pushes all docker images to all `push_targets`
* `make publish-repo`: publishes the `splunk-operator` docker image to `repo.splunk.com`
* `make publish-playground`: publishes the `splunk-operator` docker image to `cloudrepo-docker-playground.jfrog.io`
* `make publish`: publishes the `splunk-operator` docker image to all registries
* `make install`: installs required resources in current k8s target cluster
* `make uninstall`: removes required resources from current k8s target cluster
* `make start`: starts splunk operator in current k8s target cluster
* `make stop`: stops splunk operator (including all instances) in current k8s target cluster
* `make rebuild`: rebuilds and reinstalls splunk operator in current k8s target cluster


## Installing Required Resources

You must have administrator access for the Kubernetes namespace used by your current context. For Splunk8s playground,
your namespace will typically be "user-" + your username. You can set the default namespace used by the current
context by running
```
kubectl config set-context $(kubectl config current-context) --namespace=user-${USER}
``` 

The Splunk operator requires that your k8s target cluster have certain resources. These can be installed by running
```
$ make install
```

Among other things, this will create a license ConfigMap from the file `./splunk-enterprise.lic`. If the file does
not yet exist, it will attempt to download the current Splunk NFR license (assuming you are on VPN). You can use your
own license by copying it to this file before running `make install`, or re-generate the ConfigMap by running
```
kubectl delete configmap splunk-enterprise.lic
kubectl create configmap splunk-enterprise.lic --from-file=splunk-enterprise.lic
```
Note that you need to provide your own license for DFS because it is not currently included in the NFR license.

You can later remove all the installed resources by running
```
$ make uninstall
```


## Building Docker Images

The Splunk operator requires three docker images to be present or available to your k8s target cluster:

* `splunk/splunk`: The default Splunk Enterprise image (available from Docker Hub)
* `splunk-dfs`: The default Splunk Enterprise image used when DFS is enabled
* `splunk-spark`: The default Spark image used by DFS, when enabled
* `splunk-operator`: The Splunk operator image

Because DFS is not included in the current Splunk Enterprise releases, it is necessary to first clone the
[docker-splunk repository](https://github.com/splunk/docker-splunk) and build a `splunk-debian-9` base image using
7.3.0 (or later). After 7.3.0 is released, you will be able to skip this step and use `splunk/splunk` instead.
```
$ git clone git@github.com:splunk/docker-splunk.git
$ cd docker-splunk
$ make SPLUNK_LINUX_BUILD_URL=http://releases.splunk.com/dl/current_builds/7.3.0-20190125-0301/splunk-7.3.0-dd20fc0b4444-Linux-x86_64.tgz SPLUNK_LINUX_FILENAME=splunk-7.3.0-dd20fc0b4444-Linux-x86_64.tgz splunk-debian-
```
After you have built `splunk-debian-9` you can build `splunk-dfs` by running
```
$ make splunk-dfs
```

The `splunk-spark` image can be built by running
```
$ make splunk-spark
```

The `splunk-operator` image can be built by running
```
$ make splunk-operator
```


## Publishing and Pushing Docker Images

The Splunk operator requires that your k8s cluster has access to the Docker images listed above.


### Local Clusters

If you are using a local single-node k8s cluster, you only need to build the images. You can skip this section.


### UCP Clusters

* `splunk/splunk`: available from Docker Hub
* `splunk-dfs`: available via `repo.splunk.com`
* `splunk-spark`: available via `repo.splunk.com`

The `splunk-operator` image is published and available via `repo.splunk.com`.
You can publish a new local build by running
```
$ make publish-repo
```

This will tag the image as `repo.splunk.com/splunk/products/splunk-operator:[COMMIT_ID]`.


### Splunk8s Clusters

* `splunk/splunk`: available from Docker Hub
* `splunk-dfs`: available via `cloudrepo-docker-playground.jfrog.io`
* `splunk-spark`: available via `cloudrepo-docker-playground.jfrog.io`

The `splunk-operator` image is published and available via `cloudrepo-docker-playground.jfrog.io`.
You can publish a new local build by running
```
$ make publish-playground
```

This will tag the image as `cloudrepo-docker-playground.jfrog.io/pcp/splunk-operator:[COMMIT_ID]`.


### Other Clusters

You can still run the Splunk operator on clusters that do not have access to Splunk's Docker Registries.
THIS IS PRE-RELEASE SOFTWARE, SO PLEASE BE VERY CAREFUL NOT TO PUBLISH THESE IMAGES TO ANY PUBLIC REGISTRIES.

There are additional "push" targets that can be used to upload the images directly to each k8s worker node.
Create a file in the top level directory of this repository named `push_targets`. This file
should include every worker node in your K8s cluster, one user@host for each line. For example:

```
ubuntu@myvm1.splunk.com
ubuntu@myvm2.splunk.com
ubuntu@myvm3.splunk.com
```

You can push the `splunk-dfs` image to each of these nodes by running
```
$ make push-dfs
```

You can push the `splunk-spark` image to each of these nodes by running
```
$ make push-spark
```

You can push the `splunk-operator` image to each of these nodes by running
```
$ make push-operator
```

Or, just push all three using
```
$ make push
```


## Running the Splunk Operator


### UCP Clusters

You can start the operator in clusters with access to `repo.splunk.com` by running
```
$ kubectl create -f deploy/operator-repo.yaml
```

You can stop the operator by running
```
$ kubectl delete -f deploy/operator-repo.yaml
```


### Splunk8s Clusters

You can start the operator in clusters with access to `cloudrepo-docker-playground.jfrog.io` by running
```
$ kubectl create -f deploy/operator-playground.yaml
```

You can stop the operator by running
```
$ kubectl delete -f deploy/operator-playground.yaml
```


### Local and Other Clusters

You can start the operator in local clusters, or any clusters you are "pushing" the images into, by just running
```
$ make start
```

You can stop the operator by running
```
$ make stop
```


## Creating Splunk Enterprise Instances

To create a new Splunk Enterprise instance, run
```
$ kubectl create -f deploy/crds/enterprise_v1alpha1_splunkenterprise_cr.yaml
```

To remove the instance, run
```
$ kubectl delete -f deploy/crds/enterprise_v1alpha1_splunkenterprise_cr.yaml
```


## Running Splunk Enterprise with DFS

To create a new Splunk Enterprise instance with DFS (including Spark), run
```
$ kubectl create -f deploy/crds/enterprise_v1alpha1_splunkenterprise_cr_dfs.yaml
```

To remove the instance, run
```
$ kubectl delete -f deploy/crds/enterprise_v1alpha1_splunkenterprise_cr_dfs.yaml
```


## CR Spec

Here is a sample yaml file that can be used to create a **SplunkEnterprise** instance

```yaml
apiVersion: "enterprise.splunk.com/v1alpha1"
kind: "SplunkEnterprise"
metadata:
	name: "example"
spec:
	config:
		splunkPassword: helloworld
		splunkStartArgs: --accept-license
	topology:
		indexers: 1
		searchHeads: 1
```

### Relevant Parameters

#### Metadata
| Key       | Type   | Description                                                                                                       |
| --------- | ------ | ----------------------------------------------------------------------------------------------------------------- |
| name      | string | Your splunk deployments will be distinguished using this name.                                                    |
| namespace | string | Your splunk deployments will be created in this namespace. You must insure that this namespace exists beforehand. |

#### Spec
| Key                   | Type    | Description                                                                                                                                                           |
| --------------------- | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Config**            |         |                                                                                                                                                                       |
| splunkPassword        | string  | The password that can be used to login to splunk instances.                                                                                                           |
| splunkStartArgs       | string  | Arguments to launch each splunk instance with.                                                                                                                        |
| splunkImage           | string  | Docker image to use for Splunk instances (overrides SPLUNK_IMAGE and SPLUNK_DFS_IMAGE environment variables)                                                          |
| sparkImage            | string  | Docker image to use for Spark instances (overrides SPLUNK_SPARK_IMAGE environment variables)                                                                          |
| splunkLicense         |         | Specify a splunk license to use for clustered deployments.                                                                                                            |
| splunkCpuRequest      | string  | Sets the CPU request (minimum) for Splunk pods (default="0.1")                                                                                                        |
| sparkCpuRequest       | string  | Sets the CPU request (minimum) for Spark pods (default="0.1")                                                                                                         |
| splunkMemoryRequest   | string  | Sets the memory request (minimum) for Splunk pods (default="1Gi")                                                                                                     |
| sparkMemoryRequest    | string  | Sets the memory request (minimum) for Spark pods (default="1Gi")                                                                                                      |
| splunkCpuLimit        | string  | Sets the CPU limit (maximum) for Splunk pods (default="4")                                                                                                            |
| sparkCpuLimit         | string  | Sets the CPU limit (maximum) for Spark pods (default="4")                                                                                                             |
| splunkMemoryLimit     | string  | Sets the memory limit (maximum) for Splunk pods (default="8Gi")                                                                                                       |
| sparkMemoryLimit      | string  | Sets the memory limit (maximum) for Spark pods (default="8Gi")                                                                                                        |
| splunkEtcStorage      | string  | Storage capacity to request for Splunk etc volume claims (default="1Gi")                                                                                              |
| splunkVarStorage      | string  | Storage capacity to request for Splunk var volume claims (default="50Gi")                                                                                             |
| splunkIndexerStorage  | string  | Storage capacity to request for Splunk var volume claims on indexers (default="200Gi")                                                                                |
| storageClassName      | string  | Name of StorageClass to use for persistent volume claims                                                                                                              |
| imagePullPolicy       | string  | Sets pull policy for all images (either "Always" or the default: "IfNotPresent")                                                                                      |
| volumeSource          | volume  | Any Kubernetes supported volume (awsElasticBlockStore, gcePersistentDisk, nfs, etc...). This will be mounted as a directory inside the container.                     |
| licensePath           | string  | The location (and name) of the license inside the volume to be mounted.                                                                                               |
| defaultsConfigMapName | string  | The name of the ConfigMap which stores the splunk defaults data.                                                                                                      |
| enableDFS             | bool    | If this is true, DFS will be installed on **searchHeads** being launched.                                                                                             |
| **Topology**          |         |                                                                                                                                                                       |
| standalones           | integer | The number of standalone instances to launch.                                                                                                                         |
| searchHeads           | integer | The number of search heads to launch. If this number is greater than 1 then a deployer will be launched as well to create a search head cluster.                      |
| indexers              | integer | The number of indexers to launch. When **searchHeads** is defined and **indexers** is defined a **cluster master** is also launched to create a clustered deployment. |
| sparkWorkers          | integer | The number of spark workers to launch. When this is defined, a **spark cluster master** will be launched as well to create a spark cluster.                           |

**Notes**
+ If **searchHeads** is defined then **indexers** must also be defined (and vice versa).
+ If **enableDFS** is defined then **sparkWorkers** must also be defined (and vice versa) or else a DFS search won't work.

## Examples

### License Mount

Suppose we create a ConfigMap of a dfs license in our kubernetes cluster named **dfs-license.lic**. Then to use that license for our deployment the yaml would look like:

```yaml
apiVersion: "enterprise.splunk.com/v1alpha1"
kind: "SplunkEnterprise"
metadata:
	name: "example"
spec:
	config:
		splunkPassword: helloworld456
		splunkStartArgs: --accept-license
		splunkLicense:
			volumeSource:
				configMap:
					name: dfs-license
			licensePath: /dfs-license.lic
	topology:
		indexers: 3
		searchHeads: 3
```