package spark

import (
	"fmt"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"os"
)

const (
	DEPLOYMENT_TEMPLATE_STR = "splunk-%s-%s" // identifier, instance type (ex: spark-worker, spark-master)
	STATEFULSET_TEMPLATE_STR = "splunk-%s-%s" // identifier, instance type (ex: spark-worker, spark-master)
	SERVICE_TEMPLATE_STR = "splunk-%s-%s-service" // identifier, instance type (ex: spark-worker, spark-master)
	HEADLESS_SERVICE_TEMPLATE_STR = "splunk-%s-%s-headless" // identifier, instance type (ex: spark-worker, spark-master)

	SPLUNK_SPARK_IMAGE = "splunk/spark"
)


func GetSparkStatefulsetName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(STATEFULSET_TEMPLATE_STR, identifier, instanceType)
}


func GetSparkDeploymentName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(DEPLOYMENT_TEMPLATE_STR, identifier, instanceType)
}


func GetSparkServiceName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(SERVICE_TEMPLATE_STR, identifier, instanceType)
}


func GetSparkHeadlessServiceName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR, identifier, instanceType)
}


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