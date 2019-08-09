// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

package spark

import (
	"fmt"
	"os"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
)

const (
	DEPLOYMENT_TEMPLATE_STR = "splunk-%s-%s" // identifier, instance type (ex: spark-worker, spark-master)
	STATEFULSET_TEMPLATE_STR = "splunk-%s-%s" // identifier, instance type (ex: spark-worker, spark-master)
	SERVICE_TEMPLATE_STR = "splunk-%s-%s-service" // identifier, instance type (ex: spark-worker, spark-master)
	HEADLESS_SERVICE_TEMPLATE_STR = "splunk-%s-%s-headless" // identifier, instance type (ex: spark-worker, spark-master)

	SPLUNK_SPARK_IMAGE = "splunk/spark"	// default docker image used for Spark instances
)


// GetSparkStatefulsetName uses a template to name a Kubernetes StatefulSet for Spark instances.
func GetSparkStatefulsetName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(STATEFULSET_TEMPLATE_STR, identifier, instanceType)
}


// GetSparkDeploymentName uses a template to name a Kubernetes Deployment for Spark instances.
func GetSparkDeploymentName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(DEPLOYMENT_TEMPLATE_STR, identifier, instanceType)
}


// GetSparkServiceName uses a template to name a Kubernetes Service for Spark instances.
func GetSparkServiceName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(SERVICE_TEMPLATE_STR, identifier, instanceType)
}


// GetSparkHeadlessServiceName uses a template to name a headless Kubernetes Service for Spark instances.
func GetSparkHeadlessServiceName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR, identifier, instanceType)
}


// GetSparkImage returns the docker image to use for Spark instances.
func GetSparkImage(cr *v1alpha1.SplunkEnterprise) string {
	sparkImage := SPLUNK_SPARK_IMAGE
	if (cr.Spec.SparkImage != "") {
		sparkImage = cr.Spec.SparkImage
	} else {
		sparkImage = os.Getenv("SPARK_IMAGE")
		if (sparkImage == "") {
			sparkImage = SPLUNK_SPARK_IMAGE
		}
	}
	return sparkImage
}