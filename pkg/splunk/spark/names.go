package spark

import (
	"fmt"
)

const (
	DEPLOYMENT_TEMPLATE_STR = "spark-instance-%s-%s" // instance type (ex: standalone, indexers, etc...), identifier
	SERVICE_TEMPLATE_STR = "spark-service-%s-%s" // instanceType, identifier
	STATEFULSET_TEMPLATE_STR = "spark-instance-%s-%s" // instance type (ex: standalone, indexers, etc...), identifier
	HEADLESS_SERVICE_TEMPLATE_STR = "spark-headless-%s-%s" // instanceType, identifier

	SPARK_IMAGE = "tmaliksplunk/spark"
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

