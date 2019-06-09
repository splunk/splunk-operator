package enterprise

import (
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"
	"k8s.io/api/core/v1"
	"strings"
)


func GetSplunkAppLabels(identifier string, typeLabel string) map[string]string {
	labels := map[string]string{
		"app": "splunk",
		"for": identifier,
		"type": typeLabel,
	}

	return labels
}


func GetSplunkPorts() map[string]int {
	return map[string]int{
		"splunkweb": 8000,
		"splunkd": 8089,
		"dfccontrol": 17000,
		"datarecieve": 19000,
		"dfsmaster": 9000,
	}
}


func GetSplunkContainerPorts() []v1.ContainerPort {
	l := []v1.ContainerPort{}
	for key, value := range GetSplunkPorts() {
		l = append(l, v1.ContainerPort{
			Name: key,
			ContainerPort: int32(value),
		})
	}
	return l
}


func GetSplunkServicePorts() []v1.ServicePort {
	l := []v1.ServicePort{}
	for key, value := range GetSplunkPorts() {
		l = append(l, v1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}


func AppendSplunkDfsOverrides(cr *v1alpha1.SplunkEnterprise, overrides map[string]string) {
	// assertion: overrides is not nil
	if cr.Spec.Config.EnableDFS {
		overrides["ENABLE_DFS"] = "true"
		overrides["DFS_MASTER_PORT"] = "9000"
		overrides["SPARK_MASTER_HOSTNAME"] = spark.GetSparkServiceName(spark.SPARK_MASTER, GetIdentifier(cr))
		overrides["SPARK_MASTER_WEBUI_PORT"] = "8009"
		overrides["DFS_EXECUTOR_STARTING_PORT"] = "17500"
		if cr.Spec.Topology.SearchHeads > 1 {
			overrides["DFW_NUM_SLOTS_ENABLED"] = "true"
		} else {
			overrides["DFW_NUM_SLOTS_ENABLED"] = "false"
		}
		overrides["SPLUNK_ANSIBLE_POST_TASKS"] = "/opt/dfs-ansible/tasks.yml"
	}
}


func GetSplunkConfiguration(cr *v1alpha1.SplunkEnterprise, overrides map[string]string) []v1.EnvVar {
	conf := []v1.EnvVar{
		{
			Name: "SPLUNK_HOME",
			Value: "/opt/splunk",
		},
	}

	if cr.Spec.Config.SplunkStartArgs != "" {
		conf = append(conf, v1.EnvVar{
			Name: "SPLUNK_START_ARGS",
			Value: cr.Spec.Config.SplunkStartArgs,
		})
	}

	if cr.Spec.Config.SplunkPassword != "" {
		conf = append(conf, v1.EnvVar{
			Name: "SPLUNK_PASSWORD",
			Value: cr.Spec.Config.SplunkPassword,
		})
	}

	if overrides != nil {
		for k, v := range overrides {
			conf = append(conf, v1.EnvVar{
				Name: k,
				Value: v,
			})
		}
	}

	return conf
}


func GetSplunkClusterConfiguration(cr *v1alpha1.SplunkEnterprise, searchHeadCluster bool, overrides map[string]string) []v1.EnvVar {
	urls := []v1.EnvVar{
		{
			Name: "SPLUNK_CLUSTER_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_CLUSTER_MASTER, GetIdentifier(cr)),
		},{
			Name: "SPLUNK_INDEXER_URL",
			Value: GetSplunkStatefulsetUrls(cr.Namespace, SPLUNK_INDEXER, GetIdentifier(cr), cr.Spec.Topology.Indexers, true),
		},{
			Name: "SPLUNK_LICENSE_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_LICENSE_MASTER, GetIdentifier(cr)),
		},
	}

	searchHeadUrlsStr := GetSplunkStatefulsetUrls(cr.Namespace, SPLUNK_SEARCH_HEAD, GetIdentifier(cr), cr.Spec.Topology.SearchHeads, true)
	searchHeadConf := []v1.EnvVar{
		{
			Name: "SPLUNK_SEARCH_HEAD_URL",
			Value: searchHeadUrlsStr,
		},
	}
	if searchHeadCluster {
		searchHeadUrls := strings.Split(searchHeadUrlsStr, ",")
		searchHeadConf = []v1.EnvVar{
			{
				Name: "SPLUNK_SEARCH_HEAD_URL",
				Value: strings.Join(searchHeadUrls[1:], ","),
			},{
				Name: "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
				Value: searchHeadUrls[0],
			},{
				Name: "SPLUNK_DEPLOYER_URL",
				Value: GetSplunkServiceName(SPLUNK_DEPLOYER, GetIdentifier(cr)),
			},
		}
	}

	return append(append(urls, searchHeadConf...), GetSplunkConfiguration(cr, overrides)...)
}


func GetSplunkDNSConfiguration(cr *v1alpha1.SplunkEnterprise) []string {
	return []string{
		GetSplunkDNSUrl(cr.Namespace, SPLUNK_INDEXER, GetIdentifier(cr)),
		GetSplunkDNSUrl(cr.Namespace, SPLUNK_SEARCH_HEAD, GetIdentifier(cr)),
	}
}
