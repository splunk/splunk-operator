// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"fmt"
	"os"
	"strings"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

const (
	// identifier, instanceType (ex: standalone, indexers, etc...)
	deploymentTemplateStr = "splunk-%s-%s"

	// identifier, instanceType (ex: standalone, indexers, etc...)
	statefulSetTemplateStr = "splunk-%s-%s"

	// identifier, instanceType, index (ex: 0, 1, 2, ...)
	statefulSetPodTemplateStr = "splunk-%s-%s-%d"

	// identifier, instanceType, "headless" or "service"
	serviceTemplateStr = "splunk-%s-%s-%s"

	// identifier
	secretsTemplateStr = "splunk-%s-secrets"

	// identifier
	defaultsTemplateStr = "splunk-%s-defaults"

	// default docker image used for Splunk instances
	defaultSplunkImage = "splunk/splunk"

	// bytes used to generate random hexidecimal strings (e.g. HEC tokens)
	hexBytes = "ABCDEF01234567890"

	// bytes used to generate Splunk secrets
	secretBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
)

// GetSplunkDeploymentName uses a template to name a Kubernetes Deployment for Splunk instances.
func GetSplunkDeploymentName(instanceType InstanceType, identifier string) string {
	return fmt.Sprintf(deploymentTemplateStr, identifier, instanceType)
}

// GetSplunkStatefulsetName uses a template to name a Kubernetes StatefulSet for Splunk instances.
func GetSplunkStatefulsetName(instanceType InstanceType, identifier string) string {
	return fmt.Sprintf(statefulSetTemplateStr, identifier, instanceType)
}

// GetSplunkStatefulsetPodName uses a template to name a specific pod within a Kubernetes StatefulSet for Splunk instances.
func GetSplunkStatefulsetPodName(instanceType InstanceType, identifier string, index int) string {
	return fmt.Sprintf(statefulSetPodTemplateStr, identifier, instanceType, index)
}

// GetSplunkServiceName uses a template to name a Kubernetes Service for Splunk instances.
func GetSplunkServiceName(instanceType InstanceType, identifier string, isHeadless bool) string {
	var result string

	if isHeadless {
		result = fmt.Sprintf(serviceTemplateStr, identifier, instanceType, "headless")
	} else {
		result = fmt.Sprintf(serviceTemplateStr, identifier, instanceType, "service")
	}

	return result
}

// GetSplunkSecretsName uses a template to name a Kubernetes Secret for a SplunkEnterprise resource.
func GetSplunkSecretsName(identifier string) string {
	return fmt.Sprintf(secretsTemplateStr, identifier)
}

// GetSplunkDefaultsName uses a template to name a Kubernetes ConfigMap for a SplunkEnterprise resource.
func GetSplunkDefaultsName(identifier string) string {
	return fmt.Sprintf(defaultsTemplateStr, identifier)
}

// GetSplunkStatefulsetUrls returns a list of fully qualified domain names for all pods within a Splunk StatefulSet.
func GetSplunkStatefulsetUrls(namespace string, instanceType InstanceType, identifier string, replicas int, hostnameOnly bool) string {
	urls := make([]string, replicas)
	for i := 0; i < replicas; i++ {
		urls[i] = GetSplunkStatefulsetURL(namespace, instanceType, identifier, i, hostnameOnly)
	}
	return strings.Join(urls, ",")
}

// GetSplunkStatefulsetURL returns a fully qualified domain name for a specific pod within a Kubernetes StatefulSet Splunk instances.
func GetSplunkStatefulsetURL(namespace string, instanceType InstanceType, identifier string, index int, hostnameOnly bool) string {
	podName := GetSplunkStatefulsetPodName(instanceType, identifier, index)

	if hostnameOnly {
		return podName
	}

	return resources.GetServiceFQDN(namespace,
		fmt.Sprintf(
			"%s.%s",
			podName,
			GetSplunkServiceName(instanceType, identifier, true),
		))
}

// GetSplunkImage returns the docker image to use for Splunk instances.
func GetSplunkImage(cr *v1alpha2.SplunkEnterprise) string {
	var name string

	if cr.Spec.SplunkImage != "" {
		name = cr.Spec.SplunkImage
	} else {
		name = os.Getenv("SPLUNK_IMAGE")
		if name == "" {
			name = defaultSplunkImage
		}
	}

	return name
}
