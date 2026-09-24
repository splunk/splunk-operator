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
	"context"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
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

	IngestorClusterGVR = schema.GroupVersionResource{
		Group:    "enterprise.splunk.com",
		Version:  "v4",
		Resource: "ingestorclusters",
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

	IngestorClusterGVR: &GenericValidator[*enterpriseApi.IngestorCluster]{
		ValidateCreateFunc:            ValidateIngestorClusterCreate,
		ValidateUpdateFunc:            ValidateIngestorClusterUpdate,
		ValidateCreateWithContextFunc: ValidateIngestorClusterCreateWithContext,
		ValidateUpdateWithContextFunc: ValidateIngestorClusterUpdateWithContext,
		WarningsOnCreateFunc:          GetIngestorClusterWarningsOnCreate,
		WarningsOnUpdateFunc:          GetIngestorClusterWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "IngestorCluster",
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
		ValidateCreateFunc: func(obj *enterpriseApi.PostgresCluster) field.ErrorList {
			return pgclusterwebhook.ValidatePostgresClusterCreate(context.Background(), obj, nil)
		},
		ValidateUpdateFunc: func(obj, oldObj *enterpriseApi.PostgresCluster) field.ErrorList {
			return pgclusterwebhook.ValidatePostgresClusterUpdate(context.Background(), obj, oldObj, nil)
		},
		ValidateCreateWithContextFunc: func(obj *enterpriseApi.PostgresCluster, vc *ValidationContext) field.ErrorList {
			return pgclusterwebhook.ValidatePostgresClusterCreate(vc.Ctx, obj, vc.Client)
		},
		ValidateUpdateWithContextFunc: func(obj *enterpriseApi.PostgresCluster, oldObj *enterpriseApi.PostgresCluster, vc *ValidationContext) field.ErrorList {
			return pgclusterwebhook.ValidatePostgresClusterUpdate(vc.Ctx, obj, oldObj, vc.Client)
		},
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
		ValidateCreateFunc: func(obj *enterpriseApi.PostgresDatabase) field.ErrorList {
			return pgdbwebhook.ValidatePostgresDatabaseCreate(context.Background(), obj, nil)
		},
		ValidateUpdateFunc: func(obj, oldObj *enterpriseApi.PostgresDatabase) field.ErrorList {
			return pgdbwebhook.ValidatePostgresDatabaseUpdate(context.Background(), obj, oldObj, nil)
		},
		ValidateCreateWithContextFunc: func(obj *enterpriseApi.PostgresDatabase, vc *ValidationContext) field.ErrorList {
			return pgdbwebhook.ValidatePostgresDatabaseCreate(vc.Ctx, obj, vc.Client)
		},
		ValidateUpdateWithContextFunc: func(obj, oldObj *enterpriseApi.PostgresDatabase, vc *ValidationContext) field.ErrorList {
			return pgdbwebhook.ValidatePostgresDatabaseUpdate(vc.Ctx, obj, oldObj, vc.Client)
		},
		WarningsOnCreateFunc: pgdbwebhook.GetPostgresDatabaseWarningsOnCreate,
		WarningsOnUpdateFunc: pgdbwebhook.GetPostgresDatabaseWarningsOnUpdate,
		GroupKind: schema.GroupKind{
			Group: "enterprise.splunk.com",
			Kind:  "PostgresDatabase",
		},
	},
}
