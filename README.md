# CR Spec

After creating the relevant resources on your Kubernetes cluster, you will now be able to create resources of type **SplunkInstance**

Here is a sample yaml file that can be used to create a **SplunkInstance**

```yaml
apiVersion: "splunk-instance.splunk.com/v1alpha1"
kind: "SplunkInstance"
metadata:
	name: "example"
spec:
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
| Key          | Type    | Description                                                                                                                                                           |
| ------------ | ------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| standalones  | integer | The number of standalone instances to launch.                                                                                                                         |
| searchHeads  | integer | The number of search heads to launch. If this number is greater than 1 then a deployer will be launched as well to create a search head cluster.                      |
| indexers     | integer | The number of indexers to launch. When **searchHeads** is defined and **indexers** is defined a **cluster master** is also launched to create a clustered deployment. |
| enableDFS    | bool    | If this is true, DFS will be installed on **searchHeads** being launched.                                                                                             |
| sparkWorkers | integer | The number of spark workers to launch. When this is defined, a **spark cluster master** will be launched as well to create a spark cluster.                           |

**Notes**
+ If **searchHeads** is defined then **indexers** must also be defined (and vice versa).
+ If **enableDFS** is defined then **sparkWorkers** must also be defined (and vice versa) or else a DFS search won't work.