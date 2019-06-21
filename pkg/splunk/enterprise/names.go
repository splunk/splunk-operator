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
	SERVICE_TEMPLATE_STR = "splunk-%s-%s-headless" // identifier, instanceType

	SPLUNK_IMAGE = "splunk/splunk"

	LICENSE_MOUNT_LOCATION string = "/license"
	SPLUNK_DEFAULTS_MOUNT_LOCATION string = "/tmp/defaults"
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


func GetSplunkImage(cr *v1alpha1.SplunkEnterprise) string {
	splunkImage := SPLUNK_IMAGE
	if (cr.Spec.Config.SplunkImage != "") {
		splunkImage = cr.Spec.Config.SplunkImage
	} else {
		splunkImage = os.Getenv("SPLUNK_IMAGE")
		if (splunkImage == "") {
			splunkImage = SPLUNK_IMAGE
		}
	}
	return splunkImage
}
