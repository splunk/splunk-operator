package topology

import (
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newStandalone(name, namespace, splunkImage, serviceAccount, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) *enterpriseApi.Standalone {
	return &enterpriseApi.Standalone{
		TypeMeta: metav1.TypeMeta{Kind: "Standalone"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.StandaloneSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: string(corev1.PullIfNotPresent),
					Image:           splunkImage,
				},
				Volumes:        []corev1.Volume{},
				ServiceAccount: serviceAccount,
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: monitoringConsoleRef,
				},
			},
			Replicas: 1,
		},
	}
}

func newClusterManager(name, namespace, splunkImage, defaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) *enterpriseApi.ClusterManager {
	return &enterpriseApi.ClusterManager{
		TypeMeta: metav1.TypeMeta{Kind: "ClusterManager"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.ClusterManagerSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: string(corev1.PullIfNotPresent),
					Image:           splunkImage,
				},
				Volumes:  []corev1.Volume{},
				Defaults: defaults,
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: monitoringConsoleRef,
				},
			},
		},
	}
}

func newClusterMaster(name, namespace, splunkImage, defaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) *enterpriseApiV3.ClusterMaster {
	return &enterpriseApiV3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{Kind: "ClusterMaster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApiV3.ClusterMasterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: string(corev1.PullIfNotPresent),
					Image:           splunkImage,
				},
				Volumes:  []corev1.Volume{},
				Defaults: defaults,
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: monitoringConsoleRef,
				},
			},
		},
	}
}

func newIndexerCluster(name, namespace, clusterManagerRef, clusterMasterRef, splunkImage string, replicas int32, defaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) *enterpriseApi.IndexerCluster {
	return &enterpriseApi.IndexerCluster{
		TypeMeta: metav1.TypeMeta{Kind: "IndexerCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.IndexerClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: string(corev1.PullIfNotPresent),
					Image:           splunkImage,
				},
				Volumes: []corev1.Volume{},
				ClusterManagerRef: corev1.ObjectReference{
					Name: clusterManagerRef,
				},
				ClusterMasterRef: corev1.ObjectReference{
					Name: clusterMasterRef,
				},
				Defaults: defaults,
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: monitoringConsoleRef,
				},
			},
			Replicas: replicas,
		},
	}
}

func newSearchHeadCluster(name, namespace, clusterManagerRef, clusterMasterRef, splunkImage string, replicas int32, defaults, licenseManagerRef, licenseMasterRef, monitoringConsoleRef string) *enterpriseApi.SearchHeadCluster {
	return &enterpriseApi.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{Kind: "SearchHeadCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
		Spec: enterpriseApi.SearchHeadClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{
					ImagePullPolicy: string(corev1.PullIfNotPresent),
					Image:           splunkImage,
				},
				Volumes: []corev1.Volume{},
				ClusterManagerRef: corev1.ObjectReference{
					Name: clusterManagerRef,
				},
				ClusterMasterRef: corev1.ObjectReference{
					Name: clusterMasterRef,
				},
				Defaults: defaults,
				LicenseManagerRef: corev1.ObjectReference{
					Name: licenseManagerRef,
				},
				LicenseMasterRef: corev1.ObjectReference{
					Name: licenseMasterRef,
				},
				MonitoringConsoleRef: corev1.ObjectReference{
					Name: monitoringConsoleRef,
				},
			},
			Replicas: replicas,
		},
	}
}
