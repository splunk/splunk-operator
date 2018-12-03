package enterprise

import (
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"
	"git.splunk.com/splunk-operator/pkg/splunk/resources"
	"sigs.k8s.io/controller-runtime/pkg/client"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)


func CreateService(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType SplunkInstanceType, identifier string, isHeadless bool) error {

	serviceName := GetSplunkServiceName(instanceType, identifier)
	if isHeadless {
		serviceName = GetSplunkHeadlessServiceName(instanceType, identifier)
	}

	serviceType := SERVICE
	if isHeadless {
		serviceType = HEADLESS_SERVICE
	}

	serviceTypeLabels := GetSplunkAppLabels(identifier, serviceType.ToString())
	selectLabels := GetSplunkAppLabels(identifier, instanceType.ToString())

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
			Namespace: cr.Namespace,
			Labels: serviceTypeLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectLabels,
			Ports: GetSplunkServicePorts(),
		},
	}

	if isHeadless {
		service.Spec.ClusterIP = corev1.ClusterIPNone
	}

	resources.AddOwnerRefToObject(service, resources.AsOwner(cr))

	err := resources.CreateResource(client, service)
	if err != nil {
		return err
	}

	return nil
}


func CreateSparkService(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType spark.SparkInstanceType, identifier string, isHeadless bool, ports []corev1.ServicePort) error {

	serviceName := spark.GetSparkServiceName(instanceType, identifier)
	if isHeadless {
		serviceName = spark.GetSparkHeadlessServiceName(instanceType, identifier)
	}

	serviceType := SERVICE
	if isHeadless {
		serviceType = HEADLESS_SERVICE
	}

	serviceTypeLabels := spark.GetSparkAppLabels(identifier, serviceType.ToString())
	selectLabels := spark.GetSparkAppLabels(identifier, instanceType.ToString())

	service := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind: "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: serviceName,
			Namespace: cr.Namespace,
			Labels: serviceTypeLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selectLabels,
			Ports: ports,
		},
	}

	if isHeadless {
		service.Spec.ClusterIP = corev1.ClusterIPNone
	}

	resources.AddOwnerRefToObject(service, resources.AsOwner(cr))

	err := resources.CreateResource(client, service)
	if err != nil {
		return err
	}

	return nil
}