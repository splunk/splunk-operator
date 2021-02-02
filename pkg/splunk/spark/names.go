// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

package spark

import (
	"fmt"
	"os"
)

const (
	deploymentTemplateStr  = "splunk-%s-%s"    // identifier, instance type (ex: spark-worker, spark-master)
	statefulSetTemplateStr = "splunk-%s-%s"    // identifier, instance type (ex: spark-worker, spark-master)
	serviceTemplateStr     = "splunk-%s-%s-%s" // identifier, instance type (ex: spark-worker, spark-master), "headless" or "service"
	defaultSparkImage      = "splunk/spark"    // default docker image used for Spark instances
)

// GetSparkStatefulsetName uses a template to name a Kubernetes StatefulSet for Spark instances.
func GetSparkStatefulsetName(instanceType InstanceType, identifier string) string {
	return fmt.Sprintf(statefulSetTemplateStr, identifier, instanceType)
}

// GetSparkDeploymentName uses a template to name a Kubernetes Deployment for Spark instances.
func GetSparkDeploymentName(instanceType InstanceType, identifier string) string {
	return fmt.Sprintf(deploymentTemplateStr, identifier, instanceType)
}

// GetSparkServiceName uses a template to name a Kubernetes Service for Spark instances.
func GetSparkServiceName(instanceType InstanceType, identifier string, isHeadless bool) string {
	var result string

	if isHeadless {
		result = fmt.Sprintf(serviceTemplateStr, identifier, instanceType, "headless")
	} else {
		result = fmt.Sprintf(serviceTemplateStr, identifier, instanceType, "service")
	}

	return result
}

// GetSparkImage returns the docker image to use for Spark instances.
func GetSparkImage(specImage string) string {
	var name string

	if specImage != "" {
		name = specImage
	} else {
		name = os.Getenv("RELATED_IMAGE_SPLUNK_SPARK")
		if name == "" {
			name = defaultSparkImage
		}
	}

	return name
}
