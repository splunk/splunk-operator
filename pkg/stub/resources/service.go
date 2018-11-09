package resources

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
	"operator/splunk-operator/pkg/stub/splunk"
)


func CreateService(cr *v1alpha1.SplunkInstance, instanceType splunk.SplunkInstanceType, identifier string, isHeadless bool) error {

	serviceName := splunk.GetSplunkServiceName(instanceType, identifier)
	if isHeadless {
		serviceName = splunk.GetSplunkHeadlessServiceName(instanceType, identifier)
	}

	serviceType := splunk.SERVICE
	if isHeadless {
		serviceType = splunk.HEADLESS_SERVICE
	}

	serviceTypeLabels := splunk.GetSplunkAppLabels(identifier, serviceType.ToString())
	selectLabels := splunk.GetSplunkAppLabels(identifier, instanceType.ToString())

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
			Ports: splunk.GetSplunkPortsForService(),
		},
	}

	if isHeadless {
		service.Spec.ClusterIP = corev1.ClusterIPNone
	}

	AddOwnerRefToObject(service, AsOwner(cr))

	err := CreateResource(service)
	if err != nil {
		return err
	}

	return nil
}