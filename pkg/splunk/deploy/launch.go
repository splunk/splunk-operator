// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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

package deploy

import (
	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// ApplySplunkEnterprise creates all Kubernetes resources necessary to represent the
// configuration state represented by the SplunkEnterprise CRD.
func ApplySplunkEnterprise(cr *enterprisev1.SplunkEnterprise, client ControllerClient) error {

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		_, err := CheckSplunkDeletion(cr, client)
		return err
	}

	// validate and updates defaults for CR
	err := enterprise.ValidateSplunkEnterpriseSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// create a spark cluster if EnableDFS == true
	if cr.Spec.EnableDFS {
		sparkCR, err := enterprise.GetSparkResource(cr)
		if err != nil {
			return err
		}
		if err = ApplySparkCluster(sparkCR, client); err != nil {
			return err
		}
	}

	// create standalone instances when > 0
	if cr.Spec.Topology.Standalones > 0 {
		standaloneCR, err := enterprise.GetStandaloneResource(cr)
		if err != nil {
			return err
		}
		if err = ApplyStandalones(standaloneCR, client); err != nil {
			return err
		}
	}

	// create a cluster when at least 1 search head and 1 indexer
	if cr.Spec.Topology.Indexers > 0 && cr.Spec.Topology.SearchHeads > 0 {
		// create or update license master if we have a licenseURL
		if cr.Spec.LicenseURL != "" {
			licenseMasterCR, err := enterprise.GetLicenseMasterResource(cr)
			if err != nil {
				return err
			}
			if err = ApplyLicenseMaster(licenseMasterCR, client); err != nil {
				return err
			}
		}

		// create or update cluster amster
		clusterMasterCR, err := enterprise.GetClusterMasterResource(cr)
		if err != nil {
			return err
		}
		if err = ApplyClusterMaster(clusterMasterCR, client); err != nil {
			return err
		}

		// create or update deployer if we have > 1 search head
		if cr.Spec.Topology.SearchHeads > 1 {
			deployerCR, err := enterprise.GetDeployerResource(cr)
			if err != nil {
				return err
			}
			if err = ApplyDeployer(deployerCR, client); err != nil {
				return err
			}
		}

		// create or update indexers
		indexerCR, err := enterprise.GetIndexerResource(cr)
		if err != nil {
			return nil
		}
		if err = ApplyIndexers(indexerCR, client); err != nil {
			return nil
		}

		// create or update search heads
		searchHeadCR, err := enterprise.GetSearchHeadResource(cr)
		if err != nil {
			return nil
		}
		if err = ApplySearchHeads(searchHeadCR, client); err != nil {
			return nil
		}
	}

	return nil
}

// ApplySplunkConfig create or updates Kubernetes Secrets. ConfigMaps and other general settings for Splunk Enterprise instances.
func ApplySplunkConfig(client ControllerClient, cr enterprisev1.MetaObject, spec enterprisev1.CommonSplunkSpec) error {
	// create splunk secrets
	secrets := enterprise.GetSplunkSecrets(cr)
	secrets.SetOwnerReferences(append(secrets.GetOwnerReferences(), resources.AsOwner(cr)))
	if err := ApplySecret(client, secrets); err != nil {
		return err
	}

	// create splunk defaults (for inline config)
	if spec.Defaults != "" {
		defaultsMap := enterprise.GetSplunkDefaults(cr.GetIdentifier(), cr.GetNamespace(), spec.Defaults)
		defaultsMap.SetOwnerReferences(append(defaultsMap.GetOwnerReferences(), resources.AsOwner(cr)))
		if err := ApplyConfigMap(client, defaultsMap); err != nil {
			return err
		}
	}

	return nil
}

// ApplyStandalones creates a Kubernetes deployment for N standalone instances of Splunk Enterprise.
func ApplyStandalones(cr *enterprisev1.Standalone, client ControllerClient) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateStandaloneSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// create or update general config resources
	err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec)
	if err != nil {
		return err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetStandaloneStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}

// ApplyLicenseMaster create a Kubernetes Deployment and service for the Splunk Enterprise license master.
func ApplyLicenseMaster(cr *enterprisev1.LicenseMaster, client ControllerClient) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateLicenseMasterSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// create or update general config resources
	err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec)
	if err != nil {
		return err
	}

	// create or update a service
	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkLicenseMaster, false))
	if err != nil {
		return err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetLicenseMasterStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}

// ApplyClusterMaster create a Kubernetes Deployment and service for the Splunk Enterprise cluster master (used to manage an indexer cluster).
func ApplyClusterMaster(cr *enterprisev1.ClusterMaster, client ControllerClient) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateClusterMasterSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// create or update general config resources
	err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec)
	if err != nil {
		return err
	}

	// create or update a service
	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkClusterMaster, false))
	if err != nil {
		return err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetClusterMasterStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}

// ApplyDeployer create a Kubernetes Deployment and service for the Splunk Enterprise deployer (used to push apps and config to search heads).
func ApplyDeployer(cr *enterprisev1.Deployer, client ControllerClient) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateDeployerSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// create or update general config resources
	err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec)
	if err != nil {
		return err
	}

	// create or update a service
	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkDeployer, false))
	if err != nil {
		return err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetDeployerStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}

// ApplyIndexers create a Kubernetes StatefulSet and services for a Splunk Enterprise indexer cluster.
func ApplyIndexers(cr *enterprisev1.Indexer, client ControllerClient) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateIndexerSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// create or update general config resources
	err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec)
	if err != nil {
		return err
	}

	// create or update both a regular and headless service
	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkIndexer, true))
	if err != nil {
		return err
	}
	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkIndexer, false))
	if err != nil {
		return err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetIndexerStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}

// ApplySearchHeads create a Kubernetes StatefulSet and services for a Splunk Enterprise search head cluster.
func ApplySearchHeads(cr *enterprisev1.SearchHead, client ControllerClient) error {

	// validate and updates defaults for CR
	err := enterprise.ValidateSearchHeadSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// create or update general config resources
	err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec)
	if err != nil {
		return err
	}

	// create or update both a regular and headless service
	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkSearchHead, true))
	if err != nil {
		return err
	}
	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkSearchHead, false))
	if err != nil {
		return err
	}

	// create or update statefulset
	statefulSet, err := enterprise.GetSearchHeadStatefulSet(cr)
	if err != nil {
		return err
	}
	return ApplyStatefulSet(client, statefulSet)
}

// ApplySparkCluster create Kubernetes Deployment (master), StatefulSet (workers) and services Spark cluster.
func ApplySparkCluster(cr *enterprisev1.Spark, client ControllerClient) error {

	// validate and updates defaults for CR
	err := spark.ValidateSparkSpec(&cr.Spec)
	if err != nil {
		return err
	}

	// create or update a service for spark master
	err = ApplyService(client, spark.GetSparkService(cr, spark.SparkMaster, false))
	if err != nil {
		return err
	}

	// create or update a headless service for spark workers
	err = ApplyService(client, spark.GetSparkService(cr, spark.SparkWorker, true))
	if err != nil {
		return err
	}

	// create or update deployment for spark master
	deployment, err := spark.GetSparkDeployment(cr, spark.SparkMaster)
	if err != nil {
		return err
	}
	err = ApplyDeployment(client, deployment)
	if err != nil {
		return err
	}

	// create or update deployment for spark worker
	deployment, err = spark.GetSparkDeployment(cr, spark.SparkWorker)
	if err != nil {
		return err
	}
	err = ApplyDeployment(client, deployment)
	if err != nil {
		return err
	}

	return nil
}
