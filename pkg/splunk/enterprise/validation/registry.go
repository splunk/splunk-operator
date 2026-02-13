/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package validation

import (
	"k8s.io/apimachinery/pkg/runtime/schema"

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
)

// GVR constants for all Splunk Enterprise CRDs
var (
	StandaloneGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "standalones",
	}

	IndexerClusterGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "indexerclusters",
	}

	SearchHeadClusterGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "searchheadclusters",
	}

	ClusterManagerGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "clustermanagers",
	}

	ClusterMasterGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "clustermasters",
	}

	LicenseManagerGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "licensemanagers",
	}

	LicenseMasterGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "licensemasters",
	}

	MonitoringConsoleGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "monitoringconsoles",
	}
)

// DefaultValidators is the registry of validators for all Splunk Enterprise CRDs
var DefaultValidators = map[schema.GroupVersionResource]Validator{
	StandaloneGVR: &GenericValidator[*enterpriseApi.Standalone]{
		ValidateCreateFunc:   ValidateStandaloneCreate,
		ValidateUpdateFunc:   ValidateStandaloneUpdate,
		WarningsOnCreateFunc: GetStandaloneWarningsOnCreate,
		WarningsOnUpdateFunc: GetStandaloneWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "Standalone",
		},
	},

	IndexerClusterGVR: &GenericValidator[*enterpriseApi.IndexerCluster]{
		ValidateCreateFunc:   ValidateIndexerClusterCreate,
		ValidateUpdateFunc:   ValidateIndexerClusterUpdate,
		WarningsOnCreateFunc: GetIndexerClusterWarningsOnCreate,
		WarningsOnUpdateFunc: GetIndexerClusterWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "IndexerCluster",
		},
	},

	SearchHeadClusterGVR: &GenericValidator[*enterpriseApi.SearchHeadCluster]{
		ValidateCreateFunc:   ValidateSearchHeadClusterCreate,
		ValidateUpdateFunc:   ValidateSearchHeadClusterUpdate,
		WarningsOnCreateFunc: GetSearchHeadClusterWarningsOnCreate,
		WarningsOnUpdateFunc: GetSearchHeadClusterWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "SearchHeadCluster",
		},
	},

	ClusterManagerGVR: &GenericValidator[*enterpriseApi.ClusterManager]{
		ValidateCreateFunc:   ValidateClusterManagerCreate,
		ValidateUpdateFunc:   ValidateClusterManagerUpdate,
		WarningsOnCreateFunc: GetClusterManagerWarningsOnCreate,
		WarningsOnUpdateFunc: GetClusterManagerWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "ClusterManager",
		},
	},

	// ClusterMaster is an alias for ClusterManager (deprecated)
	ClusterMasterGVR: &GenericValidator[*enterpriseApi.ClusterManager]{
		ValidateCreateFunc:   ValidateClusterManagerCreate,
		ValidateUpdateFunc:   ValidateClusterManagerUpdate,
		WarningsOnCreateFunc: GetClusterManagerWarningsOnCreate,
		WarningsOnUpdateFunc: GetClusterManagerWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "ClusterManager",
		},
	},

	LicenseManagerGVR: &GenericValidator[*enterpriseApi.LicenseManager]{
		ValidateCreateFunc:   ValidateLicenseManagerCreate,
		ValidateUpdateFunc:   ValidateLicenseManagerUpdate,
		WarningsOnCreateFunc: GetLicenseManagerWarningsOnCreate,
		WarningsOnUpdateFunc: GetLicenseManagerWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "LicenseManager",
		},
	},

	// LicenseMaster is an alias for LicenseManager (deprecated)
	LicenseMasterGVR: &GenericValidator[*enterpriseApi.LicenseManager]{
		ValidateCreateFunc:   ValidateLicenseManagerCreate,
		ValidateUpdateFunc:   ValidateLicenseManagerUpdate,
		WarningsOnCreateFunc: GetLicenseManagerWarningsOnCreate,
		WarningsOnUpdateFunc: GetLicenseManagerWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "LicenseManager",
		},
	},

	MonitoringConsoleGVR: &GenericValidator[*enterpriseApi.MonitoringConsole]{
		ValidateCreateFunc:   ValidateMonitoringConsoleCreate,
		ValidateUpdateFunc:   ValidateMonitoringConsoleUpdate,
		WarningsOnCreateFunc: GetMonitoringConsoleWarningsOnCreate,
		WarningsOnUpdateFunc: GetMonitoringConsoleWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "MonitoringConsole",
		},
	},
}
