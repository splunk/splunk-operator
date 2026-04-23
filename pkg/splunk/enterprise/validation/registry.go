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
	pgclusterwebhook "github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/webhook"
	pgdbwebhook "github.com/splunk/splunk-operator/pkg/postgresql/database/adapter/webhook"
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

	PostgresClusterGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "postgresclusters",
	}

	PostgresClusterClassGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "postgresclusterclasses",
	}

	PostgresDatabaseGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "postgresdatabases",
	}
)

// DefaultValidators is the registry of validators for all Splunk Enterprise CRDs
var DefaultValidators = map[schema.GroupVersionResource]Validator{
	StandaloneGVR: &GenericValidator[*enterpriseApi.Standalone]{
		ValidateCreateFunc:            ValidateStandaloneCreate,
		ValidateUpdateFunc:            ValidateStandaloneUpdate,
		ValidateCreateWithContextFunc: ValidateStandaloneCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateStandaloneUpdateWithContext,
		WarningsOnCreateFunc:          GetStandaloneWarningsOnCreate,
		WarningsOnUpdateFunc:          GetStandaloneWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "Standalone",
		},
	},

	IndexerClusterGVR: &GenericValidator[*enterpriseApi.IndexerCluster]{
		ValidateCreateFunc:            ValidateIndexerClusterCreate,
		ValidateUpdateFunc:            ValidateIndexerClusterUpdate,
		ValidateCreateWithContextFunc: ValidateIndexerClusterCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateIndexerClusterUpdateWithContext,
		WarningsOnCreateFunc:          GetIndexerClusterWarningsOnCreate,
		WarningsOnUpdateFunc:          GetIndexerClusterWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "IndexerCluster",
		},
	},

	SearchHeadClusterGVR: &GenericValidator[*enterpriseApi.SearchHeadCluster]{
		ValidateCreateFunc:            ValidateSearchHeadClusterCreate,
		ValidateUpdateFunc:            ValidateSearchHeadClusterUpdate,
		ValidateCreateWithContextFunc: ValidateSearchHeadClusterCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateSearchHeadClusterUpdateWithContext,
		WarningsOnCreateFunc:          GetSearchHeadClusterWarningsOnCreate,
		WarningsOnUpdateFunc:          GetSearchHeadClusterWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "SearchHeadCluster",
		},
	},

	ClusterManagerGVR: &GenericValidator[*enterpriseApi.ClusterManager]{
		ValidateCreateFunc:            ValidateClusterManagerCreate,
		ValidateUpdateFunc:            ValidateClusterManagerUpdate,
		ValidateCreateWithContextFunc: ValidateClusterManagerCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateClusterManagerUpdateWithContext,
		WarningsOnCreateFunc:          GetClusterManagerWarningsOnCreate,
		WarningsOnUpdateFunc:          GetClusterManagerWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "ClusterManager",
		},
	},

	// ClusterMaster is an alias for ClusterManager (deprecated)
	ClusterMasterGVR: &GenericValidator[*enterpriseApi.ClusterManager]{
		ValidateCreateFunc:            ValidateClusterManagerCreate,
		ValidateUpdateFunc:            ValidateClusterManagerUpdate,
		ValidateCreateWithContextFunc: ValidateClusterManagerCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateClusterManagerUpdateWithContext,
		WarningsOnCreateFunc:          GetClusterManagerWarningsOnCreate,
		WarningsOnUpdateFunc:          GetClusterManagerWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "ClusterManager",
		},
	},

	LicenseManagerGVR: &GenericValidator[*enterpriseApi.LicenseManager]{
		ValidateCreateFunc:            ValidateLicenseManagerCreate,
		ValidateUpdateFunc:            ValidateLicenseManagerUpdate,
		ValidateCreateWithContextFunc: ValidateLicenseManagerCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateLicenseManagerUpdateWithContext,
		WarningsOnCreateFunc:          GetLicenseManagerWarningsOnCreate,
		WarningsOnUpdateFunc:          GetLicenseManagerWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "LicenseManager",
		},
	},

	// LicenseMaster is an alias for LicenseManager (deprecated)
	LicenseMasterGVR: &GenericValidator[*enterpriseApi.LicenseManager]{
		ValidateCreateFunc:            ValidateLicenseManagerCreate,
		ValidateUpdateFunc:            ValidateLicenseManagerUpdate,
		ValidateCreateWithContextFunc: ValidateLicenseManagerCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateLicenseManagerUpdateWithContext,
		WarningsOnCreateFunc:          GetLicenseManagerWarningsOnCreate,
		WarningsOnUpdateFunc:          GetLicenseManagerWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "LicenseManager",
		},
	},

	MonitoringConsoleGVR: &GenericValidator[*enterpriseApi.MonitoringConsole]{
		ValidateCreateFunc:            ValidateMonitoringConsoleCreate,
		ValidateUpdateFunc:            ValidateMonitoringConsoleUpdate,
		ValidateCreateWithContextFunc: ValidateMonitoringConsoleCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateMonitoringConsoleUpdateWithContext,
		WarningsOnCreateFunc:          GetMonitoringConsoleWarningsOnCreate,
		WarningsOnUpdateFunc:          GetMonitoringConsoleWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "MonitoringConsole",
		},
	},

	PostgresClusterGVR: &GenericValidator[*enterpriseApi.PostgresCluster]{
		ValidateCreateFunc:   pgclusterwebhook.ValidatePostgresClusterCreate,
		ValidateUpdateFunc:   pgclusterwebhook.ValidatePostgresClusterUpdate,
		WarningsOnCreateFunc: pgclusterwebhook.GetPostgresClusterWarningsOnCreate,
		WarningsOnUpdateFunc: pgclusterwebhook.GetPostgresClusterWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "PostgresCluster",
		},
	},

	PostgresClusterClassGVR: &GenericValidator[*enterpriseApi.PostgresClusterClass]{
		ValidateCreateFunc:   pgclusterwebhook.ValidatePostgresClusterClassCreate,
		ValidateUpdateFunc:   pgclusterwebhook.ValidatePostgresClusterClassUpdate,
		WarningsOnCreateFunc: pgclusterwebhook.GetPostgresClusterClassWarningsOnCreate,
		WarningsOnUpdateFunc: pgclusterwebhook.GetPostgresClusterClassWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "PostgresClusterClass",
		},
	},
	PostgresDatabaseGVR: &GenericValidator[*enterpriseApi.PostgresDatabase]{
		ValidateCreateFunc:   pgdbwebhook.ValidatePostgresDatabaseCreate,
		ValidateUpdateFunc:   pgdbwebhook.ValidatePostgresDatabaseUpdate,
		WarningsOnCreateFunc: pgdbwebhook.GetPostgresDatabaseWarningsOnCreate,
		WarningsOnUpdateFunc: pgdbwebhook.GetPostgresDatabaseWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "PostgresDatabase",
		},
	},
}
