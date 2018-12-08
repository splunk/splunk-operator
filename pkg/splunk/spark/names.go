package spark

import (
	"fmt"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"os"
)

const (
	DEPLOYMENT_TEMPLATE_STR = "spark-instance-%s-%s" // instance type (ex: standalone, indexers, etc...), identifier
	SERVICE_TEMPLATE_STR = "spark-service-%s-%s" // instanceType, identifier
	STATEFULSET_TEMPLATE_STR = "spark-instance-%s-%s" // instance type (ex: standalone, indexers, etc...), identifier
	HEADLESS_SERVICE_TEMPLATE_STR = "spark-headless-%s-%s" // instanceType, identifier

	SPLUNK_SPARK_IMAGE = "splunk-spark"
)


func GetSparkStatefulsetName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(STATEFULSET_TEMPLATE_STR, instanceType, identifier)
}


func GetSparkDeploymentName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(DEPLOYMENT_TEMPLATE_STR, instanceType, identifier)
}


func GetSparkServiceName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(SERVICE_TEMPLATE_STR, instanceType, identifier)
}


func GetSparkHeadlessServiceName(instanceType SparkInstanceType, identifier string) string {
	return fmt.Sprintf(HEADLESS_SERVICE_TEMPLATE_STR, instanceType, identifier)
}


func GetSparkImage(cr *v1alpha1.SplunkEnterprise) string {
	sparkImage := SPLUNK_SPARK_IMAGE
	if (cr.Spec.Config.SparkImage != "") {
		sparkImage = cr.Spec.Config.SparkImage
	} else {
		sparkImage = os.Getenv("SPLUNK_SPARK_IMAGE")
		if (sparkImage == "") {
			sparkImage = SPLUNK_SPARK_IMAGE
		}
	}
	return sparkImage
}