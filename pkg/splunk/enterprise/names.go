package enterprise

import (
	"fmt"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"os"
	"strings"
)


const (
	DEPLOYMENT_TEMPLATE_STR = "splunk-%s-%s" // identifier, instanceType (ex: standalone, indexers, etc...)
	STATEFULSET_TEMPLATE_STR = "splunk-%s-%s" // identifier, instanceType (ex: standalone, indexers, etc...)
	STATEFULSET_POD_TEMPLATE_STR = "splunk-%s-%s-%d" // identifier, instanceType, index (ex: 0, 1, 2, ...)
	HEADLESS_SERVICE_TEMPLATE_STR = "splunk-%s-%s-headless" // identifier, instanceType
	SERVICE_TEMPLATE_STR = "splunk-%s-%s-service" // identifier, instanceType
	SECRETS_TEMPLATE_STR = "splunk-%s-secrets" // identifier
	DEFAULTS_TEMPLATE_STR = "splunk-%s-defaults" // identifier

	SPLUNK_IMAGE = "splunk/splunk"
)


func GetIdentifier(cr *v1alpha1.SplunkEnterprise) string {
	return cr.GetObjectMeta().GetName()
}


func GetNamespace(cr *v1alpha1.SplunkEnterprise) string {
	return cr.GetObjectMeta().GetNamespace()
}


func GetSplunkDeploymentName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(DEPLOYMENT_TEMPLATE_STR, identifier, instanceType)
}


func GetSplunkStatefulsetName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(STATEFULSET_TEMPLATE_STR, identifier, instanceType)
}


func GetSplunkStatefulsetPodName(instanceType SplunkInstanceType, identifier string, index int) string {
	return fmt.Sprintf(STATEFULSET_POD_TEMPLATE_STR, identifier, instanceType, index)
}


func GetSplunkHeadlessServiceName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR, identifier, instanceType)
}


func GetSplunkServiceName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(SERVICE_TEMPLATE_STR, identifier, instanceType)
}


func GetSplunkSecretsName(identifier string) string {
	return fmt.Sprintf(SECRETS_TEMPLATE_STR, identifier)
}


func GetSplunkDefaultsName(identifier string) string {
	return fmt.Sprintf(DEFAULTS_TEMPLATE_STR, identifier)
}


func GetSplunkStatefulsetUrls(namespace string, instanceType SplunkInstanceType, identifier string, replicas int, hostnameOnly bool) string {
	urls := make([]string, replicas)
	for i := 0; i < replicas; i++ {
		urls[i] = GetSplunkStatefulsetUrl(namespace, instanceType, identifier, i, hostnameOnly)
	}
	return strings.Join(urls, ",")
}


func GetSplunkStatefulsetUrl(namespace string, instanceType SplunkInstanceType, identifier string, index int, hostnameOnly bool) string {
	podName := GetSplunkStatefulsetPodName(instanceType, identifier, index)

	if hostnameOnly {
		return podName
	}

	return GetServiceFQDN(namespace,
		fmt.Sprintf(
			"%s.%s",
			podName,
			GetSplunkHeadlessServiceName(instanceType, identifier),
		))
}


func GetServiceFQDN(namespace string, name string) string {
	clusterDomain := os.Getenv("CLUSTER_DOMAIN")
	if clusterDomain == "" {
		clusterDomain = "cluster.local"
	}
	return fmt.Sprintf(
		"%s.%s.svc.%s",
		name, namespace, clusterDomain,
	)
}


func GetSplunkImage(cr *v1alpha1.SplunkEnterprise) string {
	splunkImage := SPLUNK_IMAGE
	if (cr.Spec.SplunkImage != "") {
		splunkImage = cr.Spec.SplunkImage
	} else {
		splunkImage = os.Getenv("SPLUNK_IMAGE")
		if (splunkImage == "") {
			splunkImage = SPLUNK_IMAGE
		}
	}
	return splunkImage
}
