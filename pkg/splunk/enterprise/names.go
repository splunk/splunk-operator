// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package enterprise

import (
	"fmt"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/resources"
	"os"
	"strings"
)

const (
	DEPLOYMENT_TEMPLATE_STR       = "splunk-%s-%s"          // identifier, instanceType (ex: standalone, indexers, etc...)
	STATEFULSET_TEMPLATE_STR      = "splunk-%s-%s"          // identifier, instanceType (ex: standalone, indexers, etc...)
	STATEFULSET_POD_TEMPLATE_STR  = "splunk-%s-%s-%d"       // identifier, instanceType, index (ex: 0, 1, 2, ...)
	HEADLESS_SERVICE_TEMPLATE_STR = "splunk-%s-%s-headless" // identifier, instanceType
	SERVICE_TEMPLATE_STR          = "splunk-%s-%s-service"  // identifier, instanceType
	SECRETS_TEMPLATE_STR          = "splunk-%s-secrets"     // identifier
	DEFAULTS_TEMPLATE_STR         = "splunk-%s-defaults"    // identifier

	SPLUNK_IMAGE = "splunk/splunk" // default docker image used for Splunk instances

	SPLUNK_SECRET_LEN = 24
)

// GetSplunkDeploymentName uses a template to name a Kubernetes Deployment for Splunk instances.
func GetSplunkDeploymentName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(DEPLOYMENT_TEMPLATE_STR, identifier, instanceType)
}

// GetSplunkStatefulsetName uses a template to name a Kubernetes StatefulSet for Splunk instances.
func GetSplunkStatefulsetName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(STATEFULSET_TEMPLATE_STR, identifier, instanceType)
}

// GetSplunkStatefulsetPodName uses a template to name a specific pod within a Kubernetes StatefulSet for Splunk instances.
func GetSplunkStatefulsetPodName(instanceType SplunkInstanceType, identifier string, index int) string {
	return fmt.Sprintf(STATEFULSET_POD_TEMPLATE_STR, identifier, instanceType, index)
}

// GetSplunkHeadlessServiceName uses a template to name a headless Kubernetes Service of Splunk instances.
func GetSplunkHeadlessServiceName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR, identifier, instanceType)
}

// GetSplunkServiceName uses a template to name a Kubernetes Service for Splunk instances.
func GetSplunkServiceName(instanceType SplunkInstanceType, identifier string) string {
	return fmt.Sprintf(SERVICE_TEMPLATE_STR, identifier, instanceType)
}

// GetSplunkSecretsName uses a template to name a Kubernetes Secret for a SplunkEnterprise resource.
func GetSplunkSecretsName(identifier string) string {
	return fmt.Sprintf(SECRETS_TEMPLATE_STR, identifier)
}

// GetSplunkDefaultsName uses a template to name a Kubernetes ConfigMap for a SplunkEnterprise resource.
func GetSplunkDefaultsName(identifier string) string {
	return fmt.Sprintf(DEFAULTS_TEMPLATE_STR, identifier)
}

// GetSplunkStatefulsetUrls returns a list of fully qualified domain names for all pods within a Splunk StatefulSet.
func GetSplunkStatefulsetUrls(namespace string, instanceType SplunkInstanceType, identifier string, replicas int, hostnameOnly bool) string {
	urls := make([]string, replicas)
	for i := 0; i < replicas; i++ {
		urls[i] = GetSplunkStatefulsetUrl(namespace, instanceType, identifier, i, hostnameOnly)
	}
	return strings.Join(urls, ",")
}

// GetSplunkStatefulsetUrl returns a fully qualified domain name for a specific pod within a Kubernetes StatefulSet Splunk instances.
func GetSplunkStatefulsetUrl(namespace string, instanceType SplunkInstanceType, identifier string, index int, hostnameOnly bool) string {
	podName := GetSplunkStatefulsetPodName(instanceType, identifier, index)

	if hostnameOnly {
		return podName
	}

	return resources.GetServiceFQDN(namespace,
		fmt.Sprintf(
			"%s.%s",
			podName,
			GetSplunkHeadlessServiceName(instanceType, identifier),
		))
}

// GetSplunkImage returns the docker image to use for Splunk instances.
func GetSplunkImage(cr *v1alpha1.SplunkEnterprise) string {
	splunkImage := SPLUNK_IMAGE
	if cr.Spec.SplunkImage != "" {
		splunkImage = cr.Spec.SplunkImage
	} else {
		splunkImage = os.Getenv("SPLUNK_IMAGE")
		if splunkImage == "" {
			splunkImage = SPLUNK_IMAGE
		}
	}
	return splunkImage
}
