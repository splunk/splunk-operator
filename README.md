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

## Operator SDK

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

## Building the operator

You can build the operator by just running `make`. Use `make push` to push the image you've built to artifactory.

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
| splunkPassword        | string  | The password that can be used to login to splunk. instances                                                                                                           |
| splunkStartArgs       | string  | Arguments to launch each splunk instance with.                                                                                                                        |
| defaultsConfigMapName | string  | The name of the ConfigMap which stores the splunk defaults data.                                                                                                      |
| **Topology**          |         |                                                                                                                                                                       |
| standalones           | integer | The number of standalone instances to launch.                                                                                                                         |
| searchHeads           | integer | The number of search heads to launch. If this number is greater than 1 then a deployer will be launched as well to create a search head cluster.                      |
| indexers              | integer | The number of indexers to launch. When **searchHeads** is defined and **indexers** is defined a **cluster master** is also launched to create a clustered deployment. |
| enableDFS             | bool    | If this is true, DFS will be installed on **searchHeads** being launched.                                                                                             |
| sparkWorkers          | integer | The number of spark workers to launch. When this is defined, a **spark cluster master** will be launched as well to create a spark cluster.                           |

**Notes**
+ If **searchHeads** is defined then **indexers** must also be defined (and vice versa).
+ If **enableDFS** is defined then **sparkWorkers** must also be defined (and vice versa) or else a DFS search won't work.