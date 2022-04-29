package testutils

import (
	corev1 "k8s.io/api/core/v1"

	//"k8s.io/apimachinery/pkg/api/resource"
	enterpriseApi "github.com/splunk/splunk-operator/api/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var pullPolicy = corev1.PullIfNotPresent

// NewStandalone returns new Standalone instance with its config hash
func NewStandalone(name, ns, image string) *enterpriseApi.Standalone {

	c := &enterpriseApi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterpriseApi.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v3",
			Kind:       "Standalone",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
	}

	ad.Spec = enterpriseApi.StandaloneSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}
