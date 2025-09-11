package testutils

import (
	enterpriseApiV3 "github.com/splunk/splunk-operator/api/v3"
	corev1 "k8s.io/api/core/v1"

	//"k8s.io/apimachinery/pkg/api/resource"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
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

// NewIngestorCluster returns new IngestorCluster instance with its config hash
func NewIngestorCluster(name, ns, image string) *enterpriseApi.IngestorCluster {
	return &enterpriseApi.IngestorCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: enterpriseApi.IngestorClusterSpec{
			CommonSplunkSpec: enterpriseApi.CommonSplunkSpec{
				Spec: enterpriseApi.Spec{ImagePullPolicy: string(pullPolicy)},
			},
			Replicas: 3,
			PushBus: enterpriseApi.PushBusSpec{
				Type: "sqs_smartbus",
				SQS: enterpriseApi.SQSSpec{
					QueueName:                 "test-queue",
					AuthRegion:                "us-west-2",
					Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
					LargeMessageStorePath:     "s3://ingestion/smartbus-test",
					LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
					DeadLetterQueueName:       "sqs-dlq-test",
					MaxRetriesPerPart:         4,
					RetryPolicy:               "max_count",
					SendInterval:              "5s",
					EncodingFormat:            "s2s",
				},
			},
			PipelineConfig: enterpriseApi.PipelineConfigSpec{
				RemoteQueueRuleset: false,
				RuleSet:            true,
				RemoteQueueTyping:  false,
				RemoteQueueOutput:  false,
				Typing:             true,
				IndexerPipe:        true,
			},
		},
	}
}

// NewSearchHeadCluster returns new serach head cluster instance with its config hash
func NewSearchHeadCluster(name, ns, image string) *enterpriseApi.SearchHeadCluster {

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

	ad := &enterpriseApi.SearchHeadCluster{
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

	ad.Spec = enterpriseApi.SearchHeadClusterSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewMonitoringConsole returns new serach head cluster instance with its config hash
func NewMonitoringConsole(name, ns, image string) *enterpriseApi.MonitoringConsole {

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

	ad := &enterpriseApi.MonitoringConsole{
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

	ad.Spec = enterpriseApi.MonitoringConsoleSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewClusterMaster returns new serach head cluster instance with its config hash
func NewClusterMaster(name, ns, image string) *enterpriseApiV3.ClusterMaster {

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

	ad := &enterpriseApiV3.ClusterMaster{
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

	ad.Spec = enterpriseApiV3.ClusterMasterSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewClusterManager returns new serach head cluster instance with its config hash
func NewClusterManager(name, ns, image string) *enterpriseApi.ClusterManager {

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

	ad := &enterpriseApi.ClusterManager{
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

	ad.Spec = enterpriseApi.ClusterManagerSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewLicenseManager returns new serach head cluster instance with its config hash
func NewLicenseManager(name, ns, image string) *enterpriseApi.LicenseManager {

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

	ad := &enterpriseApi.LicenseManager{
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

	ad.Spec = enterpriseApi.LicenseManagerSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewLicenseMaster returns new serach head cluster instance with its config hash
func NewLicenseMaster(name, ns, image string) *enterpriseApiV3.LicenseMaster {

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

	ad := &enterpriseApiV3.LicenseMaster{
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

	ad.Spec = enterpriseApiV3.LicenseMasterSpec{
		CommonSplunkSpec: *cs,
	}
	return ad
}

// NewIndexerCluster returns new serach head cluster instance with its config hash
func NewIndexerCluster(name, ns, image string) *enterpriseApi.IndexerCluster {

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

	ad := &enterpriseApi.IndexerCluster{
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

	ad.Spec = enterpriseApi.IndexerClusterSpec{
		CommonSplunkSpec: *cs,
		PipelineConfig: enterpriseApi.PipelineConfigSpec{
			RemoteQueueRuleset: false,
			RuleSet:            true,
			RemoteQueueTyping:  false,
			RemoteQueueOutput:  false,
			Typing:             true,
			IndexerPipe:        true,
		},
		PullBus: enterpriseApi.PushBusSpec{
			Type: "sqs_smartbus",
			SQS: enterpriseApi.SQSSpec{
				QueueName:                 "test-queue",
				AuthRegion:                "us-west-2",
				Endpoint:                  "https://sqs.us-west-2.amazonaws.com",
				LargeMessageStorePath:     "s3://ingestion/smartbus-test",
				LargeMessageStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
				DeadLetterQueueName:       "sqs-dlq-test",
				MaxRetriesPerPart:         4,
				RetryPolicy:               "max_count",
				SendInterval:              "5s",
				EncodingFormat:            "s2s",
			},
		},
	}
	return ad
}
