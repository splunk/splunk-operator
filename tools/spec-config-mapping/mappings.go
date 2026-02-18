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

package main

// ConfigTarget describes how a CRD spec field maps to a Splunk configuration.
type ConfigTarget struct {
	// Which Splunk conf file this maps to: "indexes.conf", "server.conf",
	// "env" for environment variables, or "" for kubernetes-only fields.
	ConfFile string `json:"confFile"`

	// The stanza in the conf file (e.g. "cachemanager", "volume:<name>").
	Stanza string `json:"stanza,omitempty"`

	// The key within the stanza (e.g. "eviction_policy").
	Key string `json:"key,omitempty"`

	// For env var mappings, the environment variable name.
	EnvVar string `json:"envVar,omitempty"`

	// Human-readable description of how this field is consumed.
	Description string `json:"description"`
}


// fieldMappings maps each CRD spec field JSON path to its Splunk config target.
// Maintainers: update this table when CRD spec fields are added or removed.
// The drift detection test in mappings_test.go will fail if a field is missing.
var fieldMappings = map[string]ConfigTarget{
	// ── CommonSplunkSpec fields (shared by all CRDs) ──────────────────

	// Container / image
	"image":         {Description: "Docker image for Splunk pod containers"},
	"imagePullPolicy": {Description: "Image pull policy (Always or IfNotPresent)"},

	// Scheduling
	"schedulerName":              {Description: "Kubernetes scheduler name for pod placement"},
	"affinity":                   {Description: "Kubernetes affinity rules for pod scheduling"},
	"tolerations":                {Description: "Kubernetes tolerations for node taints"},
	"topologySpreadConstraints":  {Description: "Kubernetes topology spread constraints"},

	// Resources
	"resources":       {Description: "CPU and memory requests/limits for pod containers"},
	"serviceTemplate": {Description: "Template for Kubernetes Service customization (ports, LB config)"},

	// Storage — etcVolumeStorageConfig (StorageClassSpec sub-fields)
	"etcVolumeStorageConfig.storageClassName": {Description: "StorageClass name for /opt/splunk/etc PVC"},
	"etcVolumeStorageConfig.storageCapacity":  {Description: "Storage capacity for /opt/splunk/etc PVC (default: 10Gi)"},
	"etcVolumeStorageConfig.ephemeralStorage": {Description: "Use emptyDir instead of PVC for /opt/splunk/etc"},

	// Storage — varVolumeStorageConfig (StorageClassSpec sub-fields)
	"varVolumeStorageConfig.storageClassName": {Description: "StorageClass name for /opt/splunk/var PVC"},
	"varVolumeStorageConfig.storageCapacity":  {Description: "Storage capacity for /opt/splunk/var PVC (default: 100Gi)"},
	"varVolumeStorageConfig.ephemeralStorage": {Description: "Use emptyDir instead of PVC for /opt/splunk/var"},

	// Volumes
	"volumes": {Description: "Additional Kubernetes volumes mounted at /mnt/<name>"},

	// Splunk defaults (Ansible)
	"defaults": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_DEFAULTS_URL",
		Description: "Inline default.yml overrides; mounted at /mnt/splunk-defaults/default.yml and referenced via SPLUNK_DEFAULTS_URL",
	},
	"defaultsUrl": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_DEFAULTS_URL",
		Description: "External URL(s) for default.yml files; appended to SPLUNK_DEFAULTS_URL",
	},
	"defaultsUrlApps": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_DEFAULTS_URL",
		Description: "App-specific defaults URL(s); prepended to SPLUNK_DEFAULTS_URL (not applied to IDXC/SHC members)",
	},

	// License
	"licenseUrl": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_LICENSE_URI",
		Description: "URL to Splunk Enterprise license file",
	},
	"licenseMasterRef": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_LICENSE_MASTER_URL",
		Description: "(Deprecated v3 field) Reference to LicenseMaster; resolved to service FQDN",
	},
	"licenseManagerRef": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_LICENSE_MASTER_URL",
		Description: "Reference to LicenseManager; resolved to service FQDN",
	},

	// Cluster manager
	"clusterMasterRef": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_CLUSTER_MASTER_URL",
		Description: "(Deprecated v3 field) Reference to ClusterMaster; resolved to service FQDN or 'localhost' for CM",
	},
	"clusterManagerRef": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_CLUSTER_MASTER_URL",
		Description: "Reference to ClusterManager; resolved to service FQDN or 'localhost' for CM",
	},

	// Monitoring console
	"monitoringConsoleRef": {
		ConfFile:    "env",
		EnvVar:      "SPLUNK_MONITORING_CONSOLE_REF",
		Description: "Reference to MonitoringConsole; passed as CR name to register instance",
	},

	// Service account
	"serviceAccount": {Description: "Kubernetes ServiceAccount for pod identity"},

	// Extra environment
	"extraEnv": {
		ConfFile:    "env",
		Description: "Additional environment variables passed to Splunk containers; may override operator-set vars",
	},

	// Probes — top-level delay overrides
	"readinessInitialDelaySeconds": {Description: "Initial delay for readiness probe"},
	"livenessInitialDelaySeconds":  {Description: "Initial delay for liveness probe"},

	// Probes — livenessProbe (Probe sub-fields)
	"livenessProbe.initialDelaySeconds": {Description: "Liveness probe initial delay seconds"},
	"livenessProbe.timeoutSeconds":      {Description: "Liveness probe timeout seconds"},
	"livenessProbe.periodSeconds":       {Description: "Liveness probe period seconds"},
	"livenessProbe.failureThreshold":    {Description: "Liveness probe failure threshold"},

	// Probes — readinessProbe (Probe sub-fields)
	"readinessProbe.initialDelaySeconds": {Description: "Readiness probe initial delay seconds"},
	"readinessProbe.timeoutSeconds":      {Description: "Readiness probe timeout seconds"},
	"readinessProbe.periodSeconds":       {Description: "Readiness probe period seconds"},
	"readinessProbe.failureThreshold":    {Description: "Readiness probe failure threshold"},

	// Probes — startupProbe (Probe sub-fields)
	"startupProbe.initialDelaySeconds": {Description: "Startup probe initial delay seconds"},
	"startupProbe.timeoutSeconds":      {Description: "Startup probe timeout seconds"},
	"startupProbe.periodSeconds":       {Description: "Startup probe period seconds"},
	"startupProbe.failureThreshold":    {Description: "Startup probe failure threshold"},

	// Image pull secrets
	"imagePullSecrets": {Description: "Secrets for pulling images from private registries"},

	// Mock (internal)
	"Mock": {Description: "Internal flag to differentiate unit tests from actual reconciliation"},

	// ── Standalone-specific fields ────────────────────────────────────

	"replicas": {Description: "Number of pods (StatefulSet replica count)"},

	// ── SearchHeadCluster-specific fields ─────────────────────────────

	"deployerResourceSpec": {Description: "Resource requirements for the SHC Deployer pod"},
	"deployerNodeAffinity": {Description: "Node affinity rules for the SHC Deployer pod"},

	// ── SmartStore fields (Standalone, ClusterManager) ────────────────

	// smartstore.volumes[]
	"smartstore.volumes.name": {
		ConfFile:    "indexes.conf",
		Stanza:      "volume:<name>",
		Description: "Remote storage volume name; becomes stanza name in indexes.conf",
	},
	"smartstore.volumes.endpoint": {
		ConfFile:    "indexes.conf",
		Stanza:      "volume:<name>",
		Key:         "remote.s3.endpoint",
		Description: "Remote storage endpoint URL",
	},
	"smartstore.volumes.path": {
		ConfFile:    "indexes.conf",
		Stanza:      "volume:<name>",
		Key:         "path",
		Description: "Remote volume path; rendered as s3://<path>",
	},
	"smartstore.volumes.secretRef": {
		ConfFile:    "indexes.conf",
		Stanza:      "volume:<name>",
		Key:         "remote.s3.access_key, remote.s3.secret_key",
		Description: "Kubernetes Secret containing remote storage credentials",
	},
	"smartstore.volumes.storageType": {
		ConfFile:    "indexes.conf",
		Stanza:      "volume:<name>",
		Key:         "storageType",
		Description: "Remote storage type (s3, blob, gcs); always rendered as 'remote'",
	},
	"smartstore.volumes.provider": {
		ConfFile:    "indexes.conf",
		Stanza:      "volume:<name>",
		Description: "Storage provider (aws, minio, azure, gcp); used for credential handling",
	},
	"smartstore.volumes.region": {
		ConfFile:    "indexes.conf",
		Stanza:      "volume:<name>",
		Key:         "remote.s3.auth_region",
		Description: "AWS region for remote storage volume",
	},

	// smartstore.indexes[]
	"smartstore.indexes.name": {
		ConfFile:    "indexes.conf",
		Stanza:      "<index_name>",
		Description: "Splunk index name; becomes stanza name in indexes.conf",
	},
	"smartstore.indexes.remotePath": {
		ConfFile:    "indexes.conf",
		Stanza:      "<index_name>",
		Key:         "remotePath",
		Description: "Index location relative to volume path; rendered as volume:<vol>/<path>",
	},
	"smartstore.indexes.volumeName": {
		ConfFile:    "indexes.conf",
		Stanza:      "<index_name>",
		Key:         "remotePath",
		Description: "Volume name for index remote path; combined with remotePath",
	},
	"smartstore.indexes.hotlistRecencySecs": {
		ConfFile:    "indexes.conf",
		Stanza:      "<index_name>",
		Key:         "hotlist_recency_secs",
		Description: "Time period (seconds) during which bucket is protected from cache eviction",
	},
	"smartstore.indexes.hotlistBloomFilterRecencyHours": {
		ConfFile:    "indexes.conf",
		Stanza:      "<index_name>",
		Key:         "hotlist_bloom_filter_recency_hours",
		Description: "Time period (hours) during which bloom filter is protected from cache eviction",
	},
	"smartstore.indexes.maxGlobalDataSizeMB": {
		ConfFile:    "indexes.conf",
		Stanza:      "<index_name>",
		Key:         "maxGlobalDataSizeMB",
		Description: "Maximum space for warm and cold buckets of an index",
	},
	"smartstore.indexes.maxGlobalRawDataSizeMB": {
		ConfFile:    "indexes.conf",
		Stanza:      "<index_name>",
		Key:         "maxGlobalRawDataSizeMB",
		Description: "Maximum cumulative space for warm and cold buckets of an index",
	},

	// smartstore.defaults
	"smartstore.defaults.volumeName": {
		ConfFile:    "indexes.conf",
		Stanza:      "default",
		Key:         "remotePath",
		Description: "Default volume for all indexes; rendered as remotePath = volume:<vol>/$_index_name",
	},
	"smartstore.defaults.maxGlobalDataSizeMB": {
		ConfFile:    "indexes.conf",
		Stanza:      "default",
		Key:         "maxGlobalDataSizeMB",
		Description: "Default maximum space for warm and cold buckets",
	},
	"smartstore.defaults.maxGlobalRawDataSizeMB": {
		ConfFile:    "indexes.conf",
		Stanza:      "default",
		Key:         "maxGlobalRawDataSizeMB",
		Description: "Default maximum cumulative space for warm and cold buckets",
	},

	// smartstore.cacheManager
	"smartstore.cacheManager.hotlistRecencySecs": {
		ConfFile:    "server.conf",
		Stanza:      "cachemanager",
		Key:         "hotlist_recency_secs",
		Description: "Global cache: time period (seconds) bucket is protected from eviction",
	},
	"smartstore.cacheManager.hotlistBloomFilterRecencyHours": {
		ConfFile:    "server.conf",
		Stanza:      "cachemanager",
		Key:         "hotlist_bloom_filter_recency_hours",
		Description: "Global cache: time period (hours) bloom filter is protected from eviction",
	},
	"smartstore.cacheManager.evictionPolicy": {
		ConfFile:    "server.conf",
		Stanza:      "cachemanager",
		Key:         "eviction_policy",
		Description: "Cache eviction policy (e.g. lru)",
	},
	"smartstore.cacheManager.maxCacheSize": {
		ConfFile:    "server.conf",
		Stanza:      "cachemanager",
		Key:         "max_cache_size",
		Description: "Maximum cache size in MB",
	},
	"smartstore.cacheManager.evictionPadding": {
		ConfFile:    "server.conf",
		Stanza:      "cachemanager",
		Key:         "eviction_padding",
		Description: "Additional size beyond minFreeSize before eviction kicks in",
	},
	"smartstore.cacheManager.maxConcurrentDownloads": {
		ConfFile:    "server.conf",
		Stanza:      "cachemanager",
		Key:         "max_concurrent_downloads",
		Description: "Maximum parallel bucket downloads from remote storage",
	},
	"smartstore.cacheManager.maxConcurrentUploads": {
		ConfFile:    "server.conf",
		Stanza:      "cachemanager",
		Key:         "max_concurrent_uploads",
		Description: "Maximum parallel bucket uploads to remote storage",
	},

	// ── App Framework fields ──────────────────────────────────────────

	// appRepo.defaults
	"appRepo.defaults.volumeName": {
		Description: "Default remote storage volume name for app sources",
	},
	"appRepo.defaults.scope": {
		Description: "Default app deployment scope: local, cluster, clusterWithPreConfig, or premiumApps",
	},
	"appRepo.defaults.premiumAppsProps.type": {
		Description: "Premium app type (e.g. enterpriseSecurity)",
	},
	"appRepo.defaults.premiumAppsProps.esDefaults.sslEnablement": {
		ConfFile:    "web.conf",
		Stanza:      "settings",
		Key:         "enableSplunkWebSSL",
		Description: "SSL enablement mode for Enterprise Security app (strict, auto, ignore)",
	},

	// appRepo.volumes[]
	"appRepo.volumes.name": {
		Description: "App repository remote volume name",
	},
	"appRepo.volumes.endpoint": {
		Description: "App repository remote storage endpoint URL",
	},
	"appRepo.volumes.path": {
		Description: "App repository remote volume path (S3 bucket / Azure container)",
	},
	"appRepo.volumes.secretRef": {
		Description: "Kubernetes Secret for app repository remote storage credentials",
	},
	"appRepo.volumes.storageType": {
		Description: "App repository storage type (s3, blob, gcs)",
	},
	"appRepo.volumes.provider": {
		Description: "App repository storage provider (aws, minio, azure, gcp)",
	},
	"appRepo.volumes.region": {
		Description: "AWS region for app repository remote storage",
	},

	// appRepo.appSources[]
	"appRepo.appSources.name": {
		Description: "Logical name for a set of apps at a remote location",
	},
	"appRepo.appSources.location": {
		Description: "Path relative to volume where app packages reside",
	},
	"appRepo.appSources.volumeName": {
		Description: "Remote storage volume name for this app source",
	},
	"appRepo.appSources.scope": {
		Description: "App deployment scope: local (per-pod), cluster (bundle push), clusterWithPreConfig, premiumApps",
	},
	"appRepo.appSources.premiumAppsProps.type": {
		Description: "Premium app type for this app source",
	},
	"appRepo.appSources.premiumAppsProps.esDefaults.sslEnablement": {
		ConfFile:    "web.conf",
		Stanza:      "settings",
		Key:         "enableSplunkWebSSL",
		Description: "SSL enablement for Enterprise Security in this app source",
	},

	// appRepo top-level settings
	"appRepo.appsRepoPollIntervalSeconds": {
		Description: "Interval in seconds to poll remote storage for app changes (0 = disabled)",
	},
	"appRepo.appInstallPeriodSeconds": {
		Description: "Time window in seconds within a reconcile cycle to install apps",
	},
	"appRepo.installMaxRetries": {
		Description: "Maximum retry attempts for failed app installations",
	},
	"appRepo.maxConcurrentAppDownloads": {
		Description: "Maximum number of apps downloaded in parallel",
	},
}

