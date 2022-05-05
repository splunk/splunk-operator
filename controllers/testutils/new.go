package testutils

import (
	commonapi "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"

	//"k8s.io/apimachinery/pkg/api/resource"
	enterprisev3 "github.com/splunk/splunk-operator/api/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var pullPolicy = corev1.PullIfNotPresent

// NewStandalone returns new Standalone instance with its config hash
func NewStandalone(name, ns, image string) *enterprisev3.Standalone {

	c := &commonapi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterprisev3.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterprisev3.Standalone{
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

	ad.Spec = enterprisev3.StandaloneSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewSearchHeadCluster returns new serach head cluster instance with its config hash
func NewSearchHeadCluster(name, ns, image string) *enterprisev3.SearchHeadCluster {

	c := &commonapi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterprisev3.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterprisev3.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v3",
			Kind:       "SearchHeadCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
	}

	ad.Spec = enterprisev3.SearchHeadClusterSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewMonitoringConsole returns new serach head cluster instance with its config hash
func NewMonitoringConsole(name, ns, image string) *enterprisev3.MonitoringConsole {

	c := &commonapi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterprisev3.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterprisev3.MonitoringConsole{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v3",
			Kind:       "MonitoringConsole",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
	}

	ad.Spec = enterprisev3.MonitoringConsoleSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewClusterMaster returns new serach head cluster instance with its config hash
func NewClusterMaster(name, ns, image string) *enterprisev3.ClusterMaster {

	c := &commonapi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterprisev3.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterprisev3.ClusterMaster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v3",
			Kind:       "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
	}

	ad.Spec = enterprisev3.ClusterMasterSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewClusterManager returns new serach head cluster instance with its config hash
func NewClusterManager(name, ns, image string) *enterprisev3.ClusterManager {

	c := &commonapi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterprisev3.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterprisev3.ClusterManager{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v3",
			Kind:       "ClusterMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
	}

	ad.Spec = enterprisev3.ClusterManagerSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewLicenseManager returns new serach head cluster instance with its config hash
func NewLicenseManager(name, ns, image string) *enterprisev3.LicenseManager {

	c := &commonapi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterprisev3.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterprisev3.LicenseManager{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v3",
			Kind:       "LicenseManager",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
	}

	ad.Spec = enterprisev3.LicenseManagerSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewLicenseMaster returns new serach head cluster instance with its config hash
func NewLicenseMaster(name, ns, image string) *enterprisev3.LicenseMaster {

	c := &commonapi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterprisev3.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterprisev3.LicenseMaster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v3",
			Kind:       "LicenseMaster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
	}

	ad.Spec = enterprisev3.LicenseMasterSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewIndexerCluster returns new serach head cluster instance with its config hash
func NewIndexerCluster(name, ns, image string) *enterprisev3.IndexerCluster {

	c := &commonapi.Spec{
		ImagePullPolicy: string(pullPolicy),
	}

	cs := &enterprisev3.CommonSplunkSpec{
		Mock:    true,
		Spec:    *c,
		Volumes: []corev1.Volume{},
		MonitoringConsoleRef: corev1.ObjectReference{
			Name: "mcName",
		},
	}

	ad := &enterprisev3.IndexerCluster{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "enterprise.splunk.com/v3",
			Kind:       "IndexerCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  ns,
			Finalizers: []string{"enterprise.splunk.com/delete-pvc"},
		},
	}

	ad.Spec = enterprisev3.IndexerClusterSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}
