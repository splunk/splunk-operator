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


## Building the splunk-debian-9 image (required to build splunk-dfs image)

Clone the docker-splunk repository and create splunk-debian-9 image using a DFS build:

```
$ git clone git@github.com:splunk/docker-splunk.git
$ cd docker-splunk
$ make SPLUNK_LINUX_BUILD_URL=http://releases.splunk.com/dl/epic-dfs_builds/7.3.0-20181205-0400/splunk-7.3.0-ce483f77eb7f-Linux-x86_64.tgz SPLUNK_LINUX_FILENAME=splunk-7.3.0-ce483f77eb7f-Linux-x86_64.tgz splunk-debian-9

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

You can build the operator by just running `make splunk-operator`.

You can install the operator in current kubernetes target cluster by running `make install` or remove it by running `make uninstall`.

Other make targets include:

* `make all`: builds the splunk-operator, splunk-dfs and splunk-spark docker images
* `make dep`: checks all vendor dependencies and ensures they are up to date
* `make splunk-operator`: builds the `splunk-operator` docker image
* `make splunk-dfs`: builds the `splunk-dfs` docker image
* `make splunk-spark`: builds the `splunk-spark` docker image
* `make push`: pushes the `splunk-operator` docker image to `repo.splunk.com`
* `make install`: installs splunk operator in current k8s target cluster
* `make uninstall`: removes splunk operator (including all instances) from current k8s target cluster
* `make rebuild`: rebuilds and reinstalls splunk operator in current k8s target cluster


## Running Splunk Enterprise

To create a new Splunk Enterprise instance, run `kubectl create -f deploy/crds/enterprise_v1alpha1_splunkenterprise_cr.yaml`.

To remove the instance, run `kubectl delete -f deploy/crds/enterprise_v1alpha1_splunkenterprise_cr.yaml`


## CR Spec

After creating the relevant resources on your Kubernetes cluster, you will now be able to create resources of type **SplunkInstance**

Here is a sample yaml file that can be used to create a **SplunkEnterprise**

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