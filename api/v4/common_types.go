// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package v4

import (
	corev1 "k8s.io/api/core/v1"
)

const (

	// APIVersion is a string representation of this API
	APIVersion = "enterprise.splunk.com/v4"

	// TotalWorker concurrent workers to reconcile
	TotalWorker int = 15
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Add custom validation using kubebuilder tags: https://book-v1.book.kubebuilder.io/beyond_basics/generating_crd.html
// see also https://book.kubebuilder.io/reference/markers/crd.html

//CAUTION: Do not change json field tags, otherwise the configuration will not be backward compatible with the existing CRs

// AppRepoState represent the App state on remote store
type AppRepoState uint8

// Values to represent the App Repo status
const (
	RepoStateActive AppRepoState = iota + 1
	RepoStateDeleted
	RepoStatePassive
)

// Values to represent the App Source scope
const (
	ScopeLocal                = "local"
	ScopeCluster              = "cluster"
	ScopeClusterWithPreConfig = "clusterWithPreConfig"
	ScopePremiumApps          = "premiumApps"
)

// Values to represent the properties for the scope premiumApps
const (
	PremiumAppsTypeEs = "enterpriseSecurity"
)

// Values to represent the defaults of enterprise security app
const (
	SslEnablementStrict = "strict"
	SslEnablementAuto   = "auto"
	SslEnablementIgnore = "ignore"
)

// AppDeploymentStatus represents the status of an App on the Pod
type AppDeploymentStatus uint8

// Values to represent the Pod App deployment status
const (
	// Indicates there is a change on remote store, but yet to start propagating that to the Pod
	DeployStatusPending AppDeploymentStatus = iota + 1

	// App update on the Pod is in progress
	//ToDo: Mostly transient state for Phase-2, more of Phase-3 status
	DeployStatusInProgress

	// App is update is complete on the Pod
	DeployStatusComplete

	// Failed to update the App on the Pod
	DeployStatusError
)

// Spec defines a subset of the desired state of parameters that are common across all CRD types
type Spec struct {
	// Image to use for Splunk pod containers (overrides RELATED_IMAGE_SPLUNK_ENTERPRISE environment variables)
	Image string `json:"image"`

	// Sets pull policy for all images (either “Always” or the default: “IfNotPresent”)
	// +kubebuilder:validation:Enum=Always;IfNotPresent
	ImagePullPolicy string `json:"imagePullPolicy"`

	// Name of Scheduler to use for pod placement (defaults to “default-scheduler”)
	SchedulerName string `json:"schedulerName"`

	// Kubernetes Affinity rules that control how pods are assigned to particular nodes.
	Affinity corev1.Affinity `json:"affinity"`

	// Pod's tolerations for Kubernetes node's taint
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// resource requirements for the pod containers
	Resources corev1.ResourceRequirements `json:"resources"`

	// ServiceTemplate is a template used to create Kubernetes services
	ServiceTemplate corev1.Service `json:"serviceTemplate"`
}

// Phase is used to represent the current phase of a custom resource
// +kubebuilder:validation:Enum=Pending;Ready;Updating;ScalingUp;ScalingDown;Terminating;Error
type Phase string

const (
	// PhasePending means a custom resource has just been created and is not yet ready
	PhasePending Phase = "Pending"

	// PhaseReady means a custom resource is ready and up to date
	PhaseReady Phase = "Ready"

	// PhaseUpdating means a custom resource is in the process of updating to a new desired state (spec)
	PhaseUpdating Phase = "Updating"

	// PhaseScalingUp means a customer resource is in the process of scaling up
	PhaseScalingUp Phase = "ScalingUp"

	// PhaseScalingDown means a customer resource is in the process of scaling down
	PhaseScalingDown Phase = "ScalingDown"

	// PhaseTerminating means a customer resource is in the process of being removed
	PhaseTerminating Phase = "Terminating"

	// PhaseError means an error occured with custom resource management
	PhaseError Phase = "Error"
)

// Probe defines set of configurable values for Startup, Readiness, and Liveness probes
type Probe struct {
	// Number of seconds after the container has started before liveness probes are initiated.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +optional
	InitialDelaySeconds int32 `json:"initialDelaySeconds,omitempty"`
	// Number of seconds after which the probe times out.
	// More info: https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle#container-probes
	// +optional
	TimeoutSeconds int32 `json:"timeoutSeconds,omitempty"`
	// How often (in seconds) to perform the probe.
	// +optional
	PeriodSeconds int32 `json:"periodSeconds,omitempty"`
	// Minimum consecutive failures for the probe to be considered failed after having succeeded.
	// +optional
	FailureThreshold int32 `json:"failureThreshold,omitempty"`
}

// CommonSplunkSpec defines the desired state of parameters that are common across all Splunk Enterprise CRD types
type CommonSplunkSpec struct {
	Spec `json:",inline"`

	// Storage configuration for /opt/splunk/etc volume
	EtcVolumeStorageConfig StorageClassSpec `json:"etcVolumeStorageConfig"`

	// Storage configuration for /opt/splunk/var volume
	VarVolumeStorageConfig StorageClassSpec `json:"varVolumeStorageConfig"`

	// List of one or more Kubernetes volumes. These will be mounted in all pod containers as as /mnt/<name>
	Volumes []corev1.Volume `json:"volumes"`

	// Inline map of default.yml overrides used to initialize the environment
	Defaults string `json:"defaults"`

	// Full path or URL for one or more default.yml files, separated by commas
	DefaultsURL string `json:"defaultsUrl"`

	// Full path or URL for one or more defaults.yml files specific
	// to App install, separated by commas.  The defaults listed here
	// will be installed on the CM, standalone, search head deployer
	// or license manager instance.
	DefaultsURLApps string `json:"defaultsUrlApps"`

	// Full path or URL for a Splunk Enterprise license file
	LicenseURL string `json:"licenseUrl"`

	// LicenseMasterRef refers to a Splunk Enterprise license manager managed by the operator within Kubernetes
	// +optional
	LicenseMasterRef corev1.ObjectReference `json:"licenseMasterRef,omitempty"`

	// LicenseManagerRef refers to a Splunk Enterprise license manager managed by the operator within Kubernetes
	// +optional
	LicenseManagerRef corev1.ObjectReference `json:"licenseManagerRef,omitempty"`

	// ClusterMasterRef refers to a Splunk Enterprise indexer cluster managed by the operator within Kubernetes
	ClusterMasterRef corev1.ObjectReference `json:"clusterMasterRef"`

	// ClusterManagerRef refers to a Splunk Enterprise indexer cluster managed by the operator within Kubernetes
	ClusterManagerRef corev1.ObjectReference `json:"clusterManagerRef"`

	// MonitoringConsoleRef refers to a Splunk Enterprise monitoring console managed by the operator within Kubernetes
	MonitoringConsoleRef corev1.ObjectReference `json:"monitoringConsoleRef"`

	// Mock to differentiate between UTs and actual reconcile
	Mock bool `json:"Mock"`

	// ServiceAccount is the service account used by the pods deployed by the CRD.
	// If not specified uses the default serviceAccount for the namespace as per
	// https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#use-the-default-service-account-to-access-the-api-server
	ServiceAccount string `json:"serviceAccount"`

	// ExtraEnv refers to extra environment variables to be passed to the Splunk instance containers
	// WARNING: Setting environment variables used by Splunk or Ansible will affect Splunk installation and operation
	ExtraEnv []corev1.EnvVar `json:"extraEnv,omitempty"`

	// ReadinessInitialDelaySeconds defines initialDelaySeconds(See https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-readiness-probes) for Readiness probe
	// Note: If needed, Operator overrides with a higher value
	// +kubebuilder:validation:Minimum=0
	ReadinessInitialDelaySeconds int32 `json:"readinessInitialDelaySeconds"`

	// LivenessInitialDelaySeconds defines initialDelaySeconds(See https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-a-liveness-command) for the Liveness probe
	// Note: If needed, Operator overrides with a higher value
	// +kubebuilder:validation:Minimum=0
	LivenessInitialDelaySeconds int32 `json:"livenessInitialDelaySeconds"`

	// LivenessProbe as defined in https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-a-liveness-command
	LivenessProbe *Probe `json:"livenessProbe,omitempty"`

	// ReadinessProbe as defined in https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-readiness-probes
	ReadinessProbe *Probe `json:"readinessProbe,omitempty"`

	// StartupProbe as defined in https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-startup-probes
	StartupProbe *Probe `json:"startupProbe,omitempty"`

	// Sets imagePullSecrets if image is being pulled from a private registry.
	// See https://kubernetes.io/docs/tasks/configure-pod-container/pull-image-private-registry/
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`
}

// StorageClassSpec defines storage class configuration
type StorageClassSpec struct {
	// Name of StorageClass to use for persistent volume claims
	StorageClassName string `json:"storageClassName"`

	// Storage capacity to request persistent volume claims (default=”10Gi” for etc and "100Gi" for var)
	StorageCapacity string `json:"storageCapacity"`

	// If true, ephemeral (emptyDir) storage will be used
	// default false
	// +optional
	EphemeralStorage bool `json:"ephemeralStorage"`
}

// SmartStoreSpec defines Splunk indexes and remote storage volume configuration
type SmartStoreSpec struct {
	// List of remote storage volumes
	VolList []VolumeSpec `json:"volumes,omitempty"`

	// List of Splunk indexes
	IndexList []IndexSpec `json:"indexes,omitempty"`

	// Default configuration for indexes
	Defaults IndexConfDefaultsSpec `json:"defaults,omitempty"`

	// Defines Cache manager settings
	CacheManagerConf CacheManagerSpec `json:"cacheManager,omitempty"`
}

// CacheManagerSpec defines cachemanager specific configuration
type CacheManagerSpec struct {
	IndexAndCacheManagerCommonSpec `json:",inline"`

	// Eviction policy to use
	EvictionPolicy string `json:"evictionPolicy,omitempty"`

	// Max cache size per partition
	MaxCacheSizeMB uint `json:"maxCacheSize,omitempty"`

	// Additional size beyond 'minFreeSize' before eviction kicks in
	EvictionPaddingSizeMB uint `json:"evictionPadding,omitempty"`

	// Maximum number of buckets that can be downloaded from remote storage in parallel
	MaxConcurrentDownloads uint `json:"maxConcurrentDownloads,omitempty"`

	// Maximum number of buckets that can be uploaded to remote storage in parallel
	MaxConcurrentUploads uint `json:"maxConcurrentUploads,omitempty"`
}

// IndexConfDefaultsSpec defines Splunk indexes.conf global/defaults
type IndexConfDefaultsSpec struct {
	IndexAndGlobalCommonSpec `json:",inline"`
}

// VolumeSpec defines remote volume config
type VolumeSpec struct {
	// Remote volume name
	Name string `json:"name"`

	// Remote volume URI
	Endpoint string `json:"endpoint"`

	// Remote volume path
	Path string `json:"path"`

	// Secret object name
	SecretRef string `json:"secretRef"`

	// Remote Storage type. Supported values: s3, blob. s3 works with aws or minio providers, whereas blob works with azure provider.
	Type string `json:"storageType"`

	// App Package Remote Store provider. Supported values: aws, minio, azure.
	Provider string `json:"provider"`

	// Region of the remote storage volume where apps reside. Used for aws, if provided. Not used for minio and azure.
	Region string `json:"region"`
}

// VolumeAndTypeSpec used to add any custom varaibles for volume implementation
type VolumeAndTypeSpec struct {
	VolumeSpec `json:",inline"`
}

// IndexSpec defines Splunk index name and storage path
type IndexSpec struct {
	// Splunk index name
	Name string `json:"name"`

	// Index location relative to the remote volume path
	RemotePath string `json:"remotePath,omitempty"`

	IndexAndCacheManagerCommonSpec `json:",inline"`

	IndexAndGlobalCommonSpec `json:",inline"`
}

// IndexAndGlobalCommonSpec defines configurations that can be configured at index level or at global level
type IndexAndGlobalCommonSpec struct {

	// Remote Volume name
	VolName string `json:"volumeName,omitempty"`

	// MaxGlobalDataSizeMB defines the maximum amount of space for warm and cold buckets of an index
	MaxGlobalDataSizeMB uint `json:"maxGlobalDataSizeMB,omitempty"`

	// MaxGlobalDataSizeMB defines the maximum amount of cumulative space for warm and cold buckets of an index
	MaxGlobalRawDataSizeMB uint `json:"maxGlobalRawDataSizeMB,omitempty"`
}

// IndexAndCacheManagerCommonSpec defines configurations that can be configured at index level or at server level
type IndexAndCacheManagerCommonSpec struct {
	// Time period relative to the bucket's age, during which the bucket is protected from cache eviction
	HotlistRecencySecs uint `json:"hotlistRecencySecs,omitempty"`

	// Time period relative to the bucket's age, during which the bloom filter file is protected from cache eviction
	HotlistBloomFilterRecencyHours uint `json:"hotlistBloomFilterRecencyHours,omitempty"`
}

// AppSourceDefaultSpec defines config common for defaults and App Sources
type AppSourceDefaultSpec struct {
	// Remote Storage Volume name
	// +optional
	VolName string `json:"volumeName,omitempty"`

	// Scope of the App deployment: cluster, clusterWithPreConfig, local, premiumApps. Scope determines whether the App(s) is/are installed locally, cluster-wide or its a premium app
	// +optional
	Scope string `json:"scope,omitempty"`

	// Properties for premium apps, fill in when scope premiumApps is chosen
	// +optional
	PremiumAppsProps PremiumAppsProps `json:"premiumAppsProps,omitempty"`
}

// PremiumAppsProps represents properties for premium apps such as ES
type PremiumAppsProps struct {
	// Type: enterpriseSecurity for now, can accomodate itsi etc.. later
	// +optional
	Type string `json:"type,omitempty"`

	// Enterpreise Security App defaults
	// +optional
	EsDefaults EsDefaults `json:"esDefaults,omitempty"`
}

// EsDefaults captures defaults for the Enterprise Security App
type EsDefaults struct {
	// Sets the sslEnablement value for ES app installation
	//     strict: Ensure that SSL is enabled
	//             in the web.conf configuration file to use
	//             this mode. Otherwise, the installer exists
	//	     	   with an error.
	//     auto: Enables SSL in the etc/system/local/web.conf
	//           configuration file. This is the DEFAULT mode
	//           used by the operator if left empty.
	//     ignore: Ignores whether SSL is enabled or disabled.
	// +optional
	SslEnablement string `json:"sslEnablement,omitempty"`
}

// AppSourceSpec defines list of App package (*.spl, *.tgz) locations on remote volumes
type AppSourceSpec struct {
	// Logical name for the set of apps placed in this location. Logical name must be unique to the appRepo
	Name string `json:"name"`

	// Location relative to the volume path
	Location string `json:"location"`

	AppSourceDefaultSpec `json:",inline"`
}

// AppFrameworkSpec defines the application package remote store repository
type AppFrameworkSpec struct {
	// Defines the default configuration settings for App sources
	Defaults AppSourceDefaultSpec `json:"defaults,omitempty"`

	// Interval in seconds to check the Remote Storage for App changes.
	// The default value for this config is 1 hour(3600 sec),
	// minimum value is 1 minute(60sec) and maximum value is 1 day(86400 sec).
	// We assign the value based on following conditions -
	//    1. If no value or 0 is specified then it means periodic polling is disabled.
	//    2. If anything less than min is specified then we set it to 1 min.
	//    3. If anything more than the max value is specified then we set it to 1 day.
	AppsRepoPollInterval int64 `json:"appsRepoPollIntervalSeconds,omitempty"`

	// App installation period within a reconcile. Apps will be installed during this period before the next reconcile is attempted.
	// Note: Do not change this setting unless instructed to do so by Splunk Support
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum:=30
	// +kubebuilder:default:=90
	SchedulerYieldInterval uint64 `json:"appInstallPeriodSeconds,omitempty"`

	// Maximum number of retries to install Apps
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum:=0
	// +kubebuilder:default:=2
	PhaseMaxRetries uint32 `json:"installMaxRetries,omitempty"`

	// List of remote storage volumes
	VolList []VolumeSpec `json:"volumes,omitempty"`

	// List of App sources on remote storage
	AppSources []AppSourceSpec `json:"appSources,omitempty"`

	// Maximum number of apps that can be downloaded at same time
	MaxConcurrentAppDownloads uint64 `json:"maxConcurrentAppDownloads,omitempty"`
}

// AppDeploymentInfo represents a single App deployment information
type AppDeploymentInfo struct {
	AppName          string              `json:"appName"`
	LastModifiedTime string              `json:"lastModifiedTime,omitempty"`
	ObjectHash       string              `json:"objectHash"`
	IsUpdate         bool                `json:"isUpdate"`
	Size             uint64              `json:"Size,omitempty"`
	RepoState        AppRepoState        `json:"repoState"`
	DeployStatus     AppDeploymentStatus `json:"deployStatus"`

	// App phase info to track download, copy and install
	PhaseInfo PhaseInfo `json:"phaseInfo,omitempty"`

	// Used to track the copy and install status for each replica member.
	// Each Pod's phase info is mapped to its ordinal value.
	// Ignored, once the DeployStatus is marked as Complete
	AuxPhaseInfo []PhaseInfo `json:"auxPhaseInfo,omitempty"`
}

// AppSrcDeployInfo represents deployment info for list of Apps
type AppSrcDeployInfo struct {
	AppDeploymentInfoList []AppDeploymentInfo `json:"appDeploymentInfo,omitempty"`
}

//BundlePushStageType represents the bundle push status
type BundlePushStageType int

const (
	// BundlePushUninitialized indicates bundle push never happend
	BundlePushUninitialized BundlePushStageType = iota
	// BundlePushPending waiting for all the apps to be copied to the Pod
	BundlePushPending
	// BundlePushInProgress indicates bundle push in progress
	BundlePushInProgress
	// BundlePushComplete bundle push completed
	BundlePushComplete
)

// BundlePushTracker used to track the bundle push status
type BundlePushTracker struct {
	// Represents the current stage. Internal to the App framework
	BundlePushStage BundlePushStageType `json:"bundlePushStage,omitempty"`

	// defines the number of retries completed so far
	RetryCount int32 `json:"retryCount,omitempty"`
}

const (
	// AfwPhase2 represents Phase-2 app framework
	AfwPhase2 uint16 = iota

	// AfwPhase3 represents Phase-3 app framework
	AfwPhase3

	// LatestAfwVersion represents latest App framework version
	LatestAfwVersion = AfwPhase3
)

// AppDeploymentContext for storing the Apps deployment information
type AppDeploymentContext struct {
	// App Framework version info for future use
	Version uint16 `json:"version"`

	// IsDeploymentInProgress indicates if the Apps deployment is in progress
	IsDeploymentInProgress bool `json:"isDeploymentInProgress"`

	// List of App package (*.spl, *.tgz) locations on remote volume
	AppFrameworkConfig AppFrameworkSpec `json:"appRepo,omitempty"`

	// Represents the Apps deployment status
	AppsSrcDeployStatus map[string]AppSrcDeployInfo `json:"appSrcDeployStatus,omitempty"`

	// This is set to the time when we get the list of apps from remote storage.
	LastAppInfoCheckTime int64 `json:"lastAppInfoCheckTime"`

	// Interval in seconds to check the Remote Storage for App changes
	// This is introduced here so that we dont do spec validation in every reconcile just
	// because the spec and status are different.
	AppsRepoStatusPollInterval int64 `json:"appsRepoStatusPollIntervalSeconds,omitempty"`

	// Represents the Status field for maximum number of apps that can be downloaded at same time
	AppsStatusMaxConcurrentAppDownloads uint64 `json:"appsStatusMaxConcurrentAppDownloads,omitempty"`

	// Internal to the App framework. Used in case of CM(IDXC) and deployer(SHC)
	BundlePushStatus BundlePushTracker `json:"bundlePushStatus,omitempty"`
}

// AppPhaseStatusType defines the Phase status
type AppPhaseStatusType uint32

// AppPhaseType defines the App Phase
type AppPhaseType string

const (
	// PhaseDownload identifies download phase
	PhaseDownload AppPhaseType = "download"

	// PhasePodCopy identifies pod copy phase
	PhasePodCopy = "podCopy"

	// PhaseInstall identifies install phase for local scoped apps
	PhaseInstall = "install"
)

// PhaseInfo defines the status to track the App framework installation phase
type PhaseInfo struct {
	// Phase type
	Phase AppPhaseType `json:"phase,omitempty"`
	// Status of the phase
	Status AppPhaseStatusType `json:"status,omitempty"`
	// represents number of failures
	FailCount uint32 `json:"failCount,omitempty"`
}

const (
	// AppPkgDownloadPending indicates pending
	AppPkgDownloadPending AppPhaseStatusType = 101
	// AppPkgDownloadInProgress indicates in progress
	AppPkgDownloadInProgress = 102
	// AppPkgDownloadComplete indicates complete
	AppPkgDownloadComplete = 103
	// AppPkgDownloadError indicates error after retries
	AppPkgDownloadError = 199
)

const (
	// AppPkgPodCopyPending indicates pending
	AppPkgPodCopyPending AppPhaseStatusType = 201
	// AppPkgPodCopyInProgress indicates in progress
	AppPkgPodCopyInProgress = 202
	// AppPkgPodCopyComplete indicates complete
	AppPkgPodCopyComplete = 203
	// AppPkgMissingFromOperator indicates the downloaded app package is missing
	AppPkgMissingFromOperator = 298
	// AppPkgPodCopyError indicates error after retries
	AppPkgPodCopyError = 299
)

const (
	// AppPkgInstallPending indicates pending
	AppPkgInstallPending AppPhaseStatusType = 301
	// AppPkgInstallInProgress  indicates in progress
	AppPkgInstallInProgress = 302
	// AppPkgInstallComplete indicates complete
	AppPkgInstallComplete = 303
	// AppPkgMissingOnPodError indicates app pkg is not available on Pod for install
	AppPkgMissingOnPodError = 398
	// AppPkgInstallError indicates error after retries
	AppPkgInstallError = 399
)

// StatefulSetScalingType determines if the statefulset is scaling up/down
type StatefulSetScalingType uint32

const (
	// StatefulSetNotScaling indicates sts is not scaling
	StatefulSetNotScaling StatefulSetScalingType = iota
	// StatefulSetScalingUp indicates sts is scaling up
	StatefulSetScalingUp
	// StatefulSetScalingDown indicates sts is scaling down
	StatefulSetScalingDown
)
