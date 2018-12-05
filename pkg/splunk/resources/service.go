package resources

import (
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/enterprise"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


func CreateService(cr *v1alpha1.SplunkEnterprise, client client.Client, instanceType enterprise.SplunkInstanceType, identifier string, isHeadless bool) error {

	serviceName := enterprise.GetSplunkServiceName(instanceType, identifier)
	if isHeadless {
		serviceName = enterprise.GetSplunkHeadlessServiceName(instanceType, identifier)
	}

	serviceType := enterprise.SERVICE
	if isHeadless {
		serviceType = enterprise.HEADLESS_SERVICE
	}

	serviceTypeLabels := enterprise.GetSplunkAppLabels(identifier, serviceType.ToString())
	selectLabels := enterprise.GetSplunkAppLabels(identifier, instanceType.ToString())

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
			Ports: enterprise.GetSplunkServicePorts(),
		},
	}

	if isHeadless {
		service.Spec.ClusterIP = corev1.ClusterIPNone
	}

	AddOwnerRefToObject(service, AsOwner(cr))

	err := CreateResource(client, service)
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

	serviceType := enterprise.SERVICE
	if isHeadless {
		serviceType = enterprise.HEADLESS_SERVICE
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

	AddOwnerRefToObject(service, AsOwner(cr))

	err := CreateResource(client, service)
	if err != nil {
		return err
	}

	return nil
}