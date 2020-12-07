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

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
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
	defaultsTemplateStr = "splunk-%s-%s-defaults"

	// identifier
	smartstoreTemplateStr = "splunk-%s-%s-smartstore"

	// default docker image used for Splunk instances
	defaultSplunkImage = "splunk/splunk"

	// identifier used for S3 access key
	s3AccessKey = "s3_access_key"

	// identifier used for S3 secret key
	s3SecretKey = "s3_secret_key"

	//identifier for monitoring console configMap revision
	monitoringConsoleConfigRev = "monitoringConsoleConfigRev"

	// identifier to track the smartstore config rev. on Pod
	smartStoreConfigRev = "SmartStoreConfigRev"

	// command merger
	commandMerger = " && "

	// command for the init container with DFC
	commandForDfc = "cp -r /opt/jdk /mnt && cp -r /opt/spark /mnt"

	// command for init container on a standalone without DFC
	commandForStandaloneSmartstore = "mkdir -p /opt/splk/etc/apps/splunk-operator/local && ln -sfn  /mnt/splunk-operator/local/indexes.conf /opt/splk/etc/apps/splunk-operator/local/indexes.conf && ln -sfn  /mnt/splunk-operator/local/server.conf /opt/splk/etc/apps/splunk-operator/local/server.conf"

	// command for init container with Smartstore enabled and DFC enabled
	commandForDfcAndSmartstore = commandForDfc + commandMerger + commandForStandaloneSmartstore

	// command for init container on a CM
	commandForCMSmartstore = "mkdir -p /opt/splk/etc/master-apps/splunk-operator/local && ln -sfn  /mnt/splunk-operator/local/indexes.conf /opt/splk/etc/master-apps/splunk-operator/local/indexes.conf && ln -sfn  /mnt/splunk-operator/local/server.conf /opt/splk/etc/master-apps/splunk-operator/local/server.conf"

	//smartstoreconfigToken used to track if the config is reflecting on Pod or not
	configToken = "conftoken"
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
func GetSplunkStatefulsetPodName(instanceType InstanceType, identifier string, index int32) string {
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

// GetSplunkDefaultsName uses a template to name a Kubernetes ConfigMap for a SplunkEnterprise resource.
func GetSplunkDefaultsName(identifier string, instanceType InstanceType) string {
	return fmt.Sprintf(defaultsTemplateStr, identifier, instanceType.ToKind())
}

// GetSplunkMonitoringconsoleConfigMapName uses a template to name a Kubernetes ConfigMap for a SplunkEnterprise resource.
func GetSplunkMonitoringconsoleConfigMapName(identifier string, instanceType InstanceType) string {
	return fmt.Sprintf(statefulSetTemplateStr, identifier, instanceType.ToKind())
}

// GetSplunkSmartstoreConfigMapName uses a template to name a Kubernetes ConfigMap for a SplunkEnterprise resource.
func GetSplunkSmartstoreConfigMapName(identifier string, crKind string) string {
	return fmt.Sprintf(smartstoreTemplateStr, identifier, strings.ToLower(crKind))
}

// GetSplunkStatefulsetUrls returns a list of fully qualified domain names for all pods within a Splunk StatefulSet.
func GetSplunkStatefulsetUrls(namespace string, instanceType InstanceType, identifier string, replicas int32, hostnameOnly bool) string {
	urls := make([]string, replicas)
	for i := int32(0); i < replicas; i++ {
		urls[i] = GetSplunkStatefulsetURL(namespace, instanceType, identifier, i, hostnameOnly)
	}
	return strings.Join(urls, ",")
}

// GetSplunkStatefulsetURL returns a fully qualified domain name for a specific pod within a Kubernetes StatefulSet Splunk instances.
func GetSplunkStatefulsetURL(namespace string, instanceType InstanceType, identifier string, index int32, hostnameOnly bool) string {
	podName := GetSplunkStatefulsetPodName(instanceType, identifier, index)

	if hostnameOnly {
		return podName
	}

	return splcommon.GetServiceFQDN(namespace,
		fmt.Sprintf(
			"%s.%s",
			podName,
			GetSplunkServiceName(instanceType, identifier, true),
		))
}

// GetSplunkImage returns the docker image to use for Splunk instances.
func GetSplunkImage(specImage string) string {
	var name string

	if specImage != "" {
		name = specImage
	} else {
		name = os.Getenv("RELATED_IMAGE_SPLUNK_ENTERPRISE")
		if name == "" {
			name = defaultSplunkImage
		}
	}

	return name
}
