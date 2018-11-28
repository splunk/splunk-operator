package resources

import (
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
)


func CreateResource(object sdk.Object) error {
	err := sdk.Create(object)
	if err != nil && !errors.IsAlreadyExists(err) {
		logrus.Errorf("Failed to create object : %v", err)
		return err
	}
	return nil
}


func AddOwnerRefToObject(object metav1.Object, reference metav1.OwnerReference) {
	object.SetOwnerReferences(append(object.GetOwnerReferences(), reference))
}


func AsOwner(instance *v1alpha1.SplunkInstance) metav1.OwnerReference {
	trueVar := true
	return metav1.OwnerReference{
		APIVersion: instance.APIVersion,
		Kind:       instance.Kind,
		Name:       instance.Name,
		UID:        instance.UID,
		Controller: &trueVar,
	}
}
