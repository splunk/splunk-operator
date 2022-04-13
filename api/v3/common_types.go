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

package v3

import (
	corev1 "k8s.io/api/core/v1"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

const (

	// APIVersion is a string representation of this API
	APIVersion = "enterprise.splunk.com/v3"

	// TotalWorker concurrent workers to reconcile
	TotalWorker int = 15
)

// default all fields to being optional
// +kubebuilder:validation:Optional

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
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

// CommonSplunkSpec defines the desired state of parameters that are common across all Splunk Enterprise CRD types
type CommonSplunkSpec struct {
	splcommon.Spec `json:",inline"`

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

	// ObsoleteLicenseManagerRef refers to a Splunk Enterprise license manager managed by the operator within Kubernetes
	ObsoleteLicenseManagerRef corev1.ObjectReference `json:"licenseMasterRef"`

	// LicenseManagerRef refers to a Splunk Enterprise license manager managed by the operator within Kubernetes
	LicenseManagerRef corev1.ObjectReference `json:"licenseManagerRef"`

	// ClusterMasterRef refers to a Splunk Enterprise indexer cluster managed by the operator within Kubernetes
	ClusterMasterRef corev1.ObjectReference `json:"clusterMasterRef"`

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
	ReadinessInitialDelaySeconds int32 `json:"readinessInitialDelaySeconds"`

	// LivenessInitialDelaySeconds defines initialDelaySeconds(See https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-a-liveness-command) for the Liveness probe
	// Note: If needed, Operator overrides with a higher value
	LivenessInitialDelaySeconds int32 `json:"livenessInitialDelaySeconds"`
}

// StorageClassSpec defines storage class configuration
type StorageClassSpec struct {
	// Name of StorageClass to use for persistent volume claims
	StorageClassName string `json:"storageClassName"`

	// Storage capacity to request persistent volume claims (default=”10Gi” for etc and "100Gi" for var)
	StorageCapacity string `json:"storageCapacity"`

	// If true, ephemeral (emptyDir) storage will be used
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

	// Remote Storage type. Supported values: s3
	Type string `json:"storageType"`

	// App Package Remote Store provider. Supported values: aws, minio
	Provider string `json:"provider"`
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
	VolName string `json:"volumeName,omitempty"`

	// Scope of the App deployment: cluster, clusterWithPreConfig, local. Scope determines whether the App(s) is/are installed locally or cluster-wide
	Scope string `json:"scope,omitempty"`
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
	//    1. If no value or 0 is specified then it will be defaulted to 1 hour.
	//    2. If anything less than min is specified then we set it to 1 min.
	//    3. If anything more than the max value is specified then we set it to 1 day.
	AppsRepoPollInterval int64 `json:"appsRepoPollIntervalSeconds,omitempty"`

	// List of remote storage volumes
	VolList []VolumeSpec `json:"volumes,omitempty"`

	// List of App sources on remote storage
	AppSources []AppSourceSpec `json:"appSources,omitempty"`
}

// AppDeploymentInfo represents a single App deployment information
type AppDeploymentInfo struct {
	AppName          string              `json:"appName"`
	LastModifiedTime string              `json:"lastModifiedTime,omitempty"`
	ObjectHash       string              `json:"objectHash"`
	Size             uint64              `json:"Size,omitempty"`
	RepoState        AppRepoState        `json:"repoState"`
	DeployStatus     AppDeploymentStatus `json:"deployStatus"`
}

// AppSrcDeployInfo represents deployment info for list of Apps
type AppSrcDeployInfo struct {
	AppDeploymentInfoList []AppDeploymentInfo `json:"appDeploymentInfo,omitempty"`
}

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
}
