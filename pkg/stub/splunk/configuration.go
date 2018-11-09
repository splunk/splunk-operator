package splunk


import (
	"k8s.io/api/core/v1"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
)


func GetSplunkAppLabels(identifier string, typeLabel string) map[string]string {
	labels := map[string]string{
		"app": "splunk",
		"for": identifier,
		"type": typeLabel,
	}

	return labels
}


func GetSplunkPorts() []v1.ContainerPort {
	return []v1.ContainerPort{
		{
			Name: "splunkweb",
			ContainerPort: 8000,
		}, {
			Name: "splunkd",
			ContainerPort: 8089,
		},
	}
}


func GetSplunkPortsForService() []v1.ServicePort {
	return []v1.ServicePort{
		{
			Name: "splunkweb",
			Port: 8000,
		}, {
			Name: "splunkd",
			Port: 8089,
		},
	}
}


func GetSplunkConfiguration(overrides map[string]string) []v1.EnvVar {
	conf := []v1.EnvVar{
		{
			Name: "SPLUNK_HOME",
			Value: "/opt/splunk",
		},{
			Name: "SPLUNK_PASSWORD",
			Value: "helloworld",
		},{
			Name: "SPLUNK_START_ARGS",
			Value: "--accept-license",
		},
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


func GetSplunkClusterConfiguration(cr *v1alpha1.SplunkInstance, overrides map[string]string) []v1.EnvVar {
	urls := []v1.EnvVar{
		{
			Name: "SPLUNK_CLUSTER_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_CLUSTER_MASTER, GetIdentifier(cr)),
		},{
			Name: "SPLUNK_INDEXER_URL",
			Value: GetSplunkStatefulsetUrls(cr.Namespace, SPLUNK_INDEXER, GetIdentifier(cr), cr.Spec.Indexers, false),
		},{
			Name: "SPLUNK_SEARCH_HEAD_URL",
			Value: GetSplunkStatefulsetUrls(cr.Namespace, SPLUNK_SEARCH_HEAD, GetIdentifier(cr), cr.Spec.Indexers, false),
		},{
			Name: "SPLUNK_DEPLOYER_URL",
			Value: GetSplunkServiceName(SPLUNK_DEPLOYER, GetIdentifier(cr)),
		},{
			Name: "SPLUNK_LICENSE_MASTER_URL",
			Value: GetSplunkServiceName(SPLUNK_LICENSE_MASTER, GetIdentifier(cr)),
		},
	}

	return append(urls, GetSplunkConfiguration(overrides)...)
}


func GetImagePullSecrets() []v1.LocalObjectReference {
	return []v1.LocalObjectReference{
		{
			Name: "dockerhubtmalikkey",
		},
	}
}