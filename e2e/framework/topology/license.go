package topology

import (
	"context"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	"github.com/splunk/splunk-operator/e2e/framework/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeployLicenseManager creates a license manager CR.
func DeployLicenseManager(ctx context.Context, kube *k8s.Client, namespace, name, splunkImage, licenseConfigMap string) (*enterpriseApi.LicenseManager, error) {
	lm := &enterpriseApi.LicenseManager{
		TypeMeta: metav1.TypeMeta{Kind: "LicenseManager"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.LicenseManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: "licenses",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: licenseConfigMap,
								},
							},
						},
					},
				},
				LicenseURL: "/mnt/licenses/enterprise.lic",
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: string(corev1.PullIfNotPresent),
					Image:           splunkImage,
				},
			},
		},
	}
	if err := kube.Client.Create(ctx, lm); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
	}
	return lm, nil
}

// DeployLicenseMaster creates a license master CR (v3).
func DeployLicenseMaster(ctx context.Context, kube *k8s.Client, namespace, name, splunkImage, licenseConfigMap string) (*enterpriseApiV3.LicenseMaster, error) {
	lm := &enterpriseApiV3.LicenseMaster{
		TypeMeta: metav1.TypeMeta{Kind: "LicenseMaster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApiV3.LicenseMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Volumes: []corev1.Volume{
					{
						Name: "licenses",
						VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{
									Name: licenseConfigMap,
								},
							},
						},
					},
				},
				LicenseURL: "/mnt/licenses/enterprise.lic",
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: string(corev1.PullIfNotPresent),
					Image:           splunkImage,
				},
			},
		},
	}
	if err := kube.Client.Create(ctx, lm); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
	}
	return lm, nil
}

// DeployMonitoringConsole creates a monitoring console CR.
func DeployMonitoringConsole(ctx context.Context, kube *k8s.Client, namespace, name, splunkImage, licenseManagerRef, licenseMasterRef string) (*enterpriseApi.MonitoringConsole, error) {
	mc := &enterpriseApi.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{Kind: "MonitoringConsole"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.MonitoringConsoleSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: string(corev1.PullIfNotPresent),
					Image:           splunkImage,
				},
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				Volumes: []corev1.Volume{},
			},
		},
	}
	if err := kube.Client.Create(ctx, mc); err != nil {
		if !apierrors.IsAlreadyExists(err) {
			return nil, err
		}
	}
	return mc, nil
}
