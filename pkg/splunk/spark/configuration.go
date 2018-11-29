package spark

import "k8s.io/api/core/v1"

func GetSparkAppLabels(identifier string, typeLabel string) map[string]string {
	labels := map[string]string{
		"app": "spark",
		"for": identifier,
		"type": typeLabel,
	}

	return labels
}


func GetSparkMasterPorts() map[string]int {
	return map[string]int{
		"sparkmaster": 7777,
		"sparkwebui": 8009,
	}
}


func GetSparkMasterContainerPorts() []v1.ContainerPort {
	l := []v1.ContainerPort{}
	for key, value := range GetSparkMasterPorts() {
		l = append(l, v1.ContainerPort{
			Name: key,
			ContainerPort: int32(value),
		})
	}
	return l
}


func GetSparkMasterServicePorts() []v1.ServicePort {
	l := []v1.ServicePort{}
	for key, value := range GetSparkMasterPorts() {
		l = append(l, v1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}


func GetSparkMasterConfiguration() []v1.EnvVar {
	return []v1.EnvVar{
		{
			Name: "SPLUNK_ROLE",
			Value: "splunk_spark_master",
		},{
			Name: "SPARK_MASTER_WEBUI_PORT",
			Value: "8009",
		},{
			Name: "SPARK_MASTER_PORT",
			Value: "7777",
		},
	}
}


func GetSparkWorkerPorts() map[string]int {
	return map[string]int{
		"dfwreceivedata": 17500,
		"workerwebui": 7000,
	}
}


func GetSparkWorkerContainerPorts() []v1.ContainerPort {
	l := []v1.ContainerPort{}
	for key, value := range GetSparkWorkerPorts() {
		l = append(l, v1.ContainerPort{
			Name: key,
			ContainerPort: int32(value),
		})
	}
	return l
}


func GetSparkWorkerServicePorts() []v1.ServicePort {
	l := []v1.ServicePort{}
	for key, value := range GetSparkWorkerPorts() {
		l = append(l, v1.ServicePort{
			Name: key,
			Port: int32(value),
		})
	}
	return l
}


func GetSparkWorkerConfiguration(identifier string) []v1.EnvVar {
	return []v1.EnvVar{
		{
			Name: "SPLUNK_ROLE",
			Value: "splunk_spark_worker",
		},{
			Name: "SPARK_MASTER_WEBUI_PORT",
			Value: "8009",
		},{
			Name: "SPARK_MASTER_PORT",
			Value: "7777",
		},{
			Name: "SPARK_WORKER_WEBUI_PORT",
			Value: "7000",
		},{
			Name: "SPARK_MASTER_HOSTNAME",
			Value: GetSparkServiceName(SPARK_MASTER, identifier),
		},
	}
}