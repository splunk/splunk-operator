package enterprise

import (
	"fmt"
	"strings"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
)


const (
	DEPLOYMENT_TEMPLATE_STR = "splunk-instance-%s-%s" // instance type (ex: standalone, indexers, etc...), identifier
	STATEFULSET_TEMPLATE_STR = "splunk-instance-%s-%s" // instance type (ex: standalone, indexers, etc...), identifier
	STATEFULSET_POD_TEMPLATE_STR = "splunk-instance-%s-%s-%d" // instanceType, identifier, index (ex: 0, 1, 2, ...)
	HEADLESS_SERVICE_TEMPLATE_STR = "splunk-headless-%s-%s" // instanceType, identifier
	SERVICE_TEMPLATE_STR = "splunk-service-%s-%s" // instanceType, identifier

	SPLUNK_IMAGE = "repo.splunk.com/tmalik/dfs"
)


func GetIdentifier(cr *v1alpha1.SplunkEnterprise) string {
	return cr.GetObjectMeta().GetName()
}


func GetSplunkDeploymentName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(DEPLOYMENT_TEMPLATE_STR, instanceType, identifier)
}


func GetSplunkStatefulsetName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(STATEFULSET_TEMPLATE_STR, instanceType, identifier)
}


func GetSplunkStatefulsetPodName(instanceType SplunkInstanceType, identifier string, index int) string {
	return fmt.Sprintf(STATEFULSET_POD_TEMPLATE_STR, instanceType, identifier, index)
}


func GetSplunkHeadlessServiceName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR, instanceType, identifier)
}


func GetSplunkServiceName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(SERVICE_TEMPLATE_STR, instanceType, identifier)
}


func GetSplunkStatefulsetUrls(namespace string, instanceType SplunkInstanceType, identifier string, replicas int, hostnameOnly bool) string {
	urls := make([]string, replicas)
	for i := 0; i < replicas; i++ {
		urls[i] = GetSplunkStatefulsetUrl(namespace, instanceType, identifier, i, hostnameOnly)
	}
	return strings.Join(urls, ",")
}


func GetSplunkStatefulsetUrl(namespace string, instanceType SplunkInstanceType, identifier string, index int, hostnameOnly bool) string {
	if hostnameOnly {
		return GetSplunkStatefulsetPodName(instanceType, identifier, index)
	} else {
		return fmt.Sprintf(
			"%s.%s.%s.svc.cluster.local",
			GetSplunkStatefulsetPodName(instanceType, identifier, index),
			GetSplunkHeadlessServiceName(instanceType, identifier),
			namespace,
		)
	}
}


func GetSplunkDNSUrl(namespace string, instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(
		"%s.%s.svc.cluster.local",
		GetSplunkHeadlessServiceName(instanceType, identifier),
		namespace,
	)
}
