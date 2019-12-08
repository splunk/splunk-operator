// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
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
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// LaunchDeployment creates all Kubernetes resources necessary to represent the
// configuration state represented by the SplunkEnterprise CRD.
func LaunchDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	// launch a spark cluster if EnableDFS == true
	if cr.Spec.EnableDFS {
		// validation assertion: cr.Spec.Topology.SparkWorkers > 0
		if err := LaunchSparkCluster(cr, client); err != nil {
			return err
		}
	}

	// create splunk secrets
	secrets := enterprise.GetSplunkSecrets(cr)
	secrets.SetOwnerReferences(append(secrets.GetOwnerReferences(), resources.AsOwner(cr)))
	if err := ApplySecret(client, secrets); err != nil {
		return err
	}

	// create splunk defaults (for inline config)
	if cr.Spec.Defaults != "" {
		defaults := enterprise.GetSplunkDefaults(cr)
		defaults.SetOwnerReferences(append(defaults.GetOwnerReferences(), resources.AsOwner(cr)))
		if err := ApplyConfigMap(client, defaults); err != nil {
			return err
		}
	}

	// launch standalone instances when > 0
	if cr.Spec.Topology.Standalones > 0 {
		if err := LaunchStandalones(cr, client); err != nil {
			return err
		}
	}

	// launch a cluster when at least 1 search head and 1 indexer
	if cr.Spec.Topology.Indexers > 0 && cr.Spec.Topology.SearchHeads > 0 {
		if err := LaunchCluster(cr, client); err != nil {
			return err
		}
	}

	return nil
}

// LaunchStandalones creates a Kubernetes deployment for N standalone instances of Splunk Enterprise.
func LaunchStandalones(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	overrides := map[string]string{"SPLUNK_ROLE": "splunk_standalone"}
	if cr.Spec.LicenseURL != "" {
		overrides["SPLUNK_LICENSE_URI"] = cr.Spec.LicenseURL
	}
	if cr.Spec.EnableDFS {
		overrides = enterprise.AppendSplunkDfsOverrides(overrides, cr.GetIdentifier(), cr.Spec.Topology.SearchHeads)
	}

	err := ApplySplunkStatefulSet(
		cr,
		client,
		enterprise.SplunkStandalone,
		cr.Spec.Topology.Standalones,
		enterprise.GetSplunkConfiguration(overrides, cr.Spec.Defaults, cr.Spec.DefaultsURL),
	)
	if err != nil {
		return err
	}

	return nil
}

// LaunchCluster creates all Kubernetes resources necessary to represent a complete Splunk Enterprise cluster.
func LaunchCluster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := LaunchLicenseMaster(cr, client)
	if err != nil {
		return err
	}

	err = LaunchClusterMaster(cr, client)
	if err != nil {
		return err
	}

	if cr.Spec.Topology.SearchHeads > 1 {
		err = LaunchDeployer(cr, client)
		if err != nil {
			return err
		}
	}

	err = LaunchIndexers(cr, client)
	if err != nil {
		return nil
	}

	err = LaunchSearchHeads(cr, client)
	if err != nil {
		return nil
	}

	return nil
}

// LaunchLicenseMaster create a Kubernetes Deployment and service for the Splunk Enterprise license master.
func LaunchLicenseMaster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkLicenseMaster, false))
	if err != nil {
		return err
	}

	err = ApplySplunkStatefulSet(
		cr,
		client,
		enterprise.SplunkLicenseMaster,
		1,
		enterprise.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.Topology.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE":        "splunk_license_master",
				"SPLUNK_LICENSE_URI": cr.Spec.LicenseURL,
			},
		),
	)
	if err != nil {
		return err
	}

	return nil
}

// LaunchClusterMaster create a Kubernetes Deployment and service for the Splunk Enterprise cluster master (used to manage an indexer cluster).
func LaunchClusterMaster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkClusterMaster, false))
	if err != nil {
		return err
	}

	err = ApplySplunkStatefulSet(
		cr,
		client,
		enterprise.SplunkClusterMaster,
		1,
		enterprise.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.Topology.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_cluster_master",
			},
		),
	)
	if err != nil {
		return err
	}

	return err
}

// LaunchDeployer create a Kubernetes Deployment and service for the Splunk Enterprise deployer (used to push apps and config to search heads).
func LaunchDeployer(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkDeployer, false))
	if err != nil {
		return err
	}

	err = ApplySplunkStatefulSet(
		cr,
		client,
		enterprise.SplunkDeployer,
		1,
		enterprise.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.Topology.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_deployer",
			},
		),
	)
	if err != nil {
		return err
	}

	return err
}

// LaunchIndexers create a Kubernetes StatefulSet and services for a Splunk Enterprise indexer cluster.
func LaunchIndexers(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := ApplySplunkStatefulSet(
		cr,
		client,
		enterprise.SplunkIndexer,
		cr.Spec.Topology.Indexers,
		enterprise.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.Topology.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_indexer",
			},
		),
	)
	if err != nil {
		return err
	}

	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkIndexer, true))
	if err != nil {
		return err
	}

	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkIndexer, false))
	if err != nil {
		return err
	}

	return err
}

// LaunchSearchHeads create a Kubernetes StatefulSet and services for a Splunk Enterprise search head cluster.
func LaunchSearchHeads(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	overrides := map[string]string{"SPLUNK_ROLE": "splunk_search_head"}
	if cr.Spec.EnableDFS {
		overrides = enterprise.AppendSplunkDfsOverrides(overrides, cr.GetIdentifier(), cr.Spec.Topology.SearchHeads)
	}

	err := ApplySplunkStatefulSet(
		cr,
		client,
		enterprise.SplunkSearchHead,
		cr.Spec.Topology.SearchHeads,
		enterprise.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.Topology.SearchHeads > 1,
			overrides,
		),
	)
	if err != nil {
		return err
	}

	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkSearchHead, true))
	if err != nil {
		return err
	}

	err = ApplyService(client, enterprise.GetSplunkService(cr, enterprise.SplunkSearchHead, false))
	if err != nil {
		return err
	}

	return err
}

// LaunchSparkCluster create Kubernetes Deployment (master), StatefulSet (workers) and services Spark cluster.
func LaunchSparkCluster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := ApplyService(client, spark.GetSparkService(cr, spark.SparkMaster, false, spark.GetSparkMasterServicePorts()))
	if err != nil {
		return err
	}

	err = ApplySparkDeployment(cr, client, spark.SparkMaster, 1, spark.GetSparkMasterConfiguration(), spark.GetSparkMasterContainerPorts())
	if err != nil {
		return err
	}

	err = ApplySparkStatefulSet(cr,
		client,
		spark.SparkWorker,
		cr.Spec.Topology.SparkWorkers,
		spark.GetSparkWorkerConfiguration(cr.GetIdentifier()),
		spark.GetSparkWorkerContainerPorts())
	if err != nil {
		return err
	}

	err = ApplyService(client, spark.GetSparkService(cr, spark.SparkWorker, true, spark.GetSparkWorkerServicePorts()))
	if err != nil {
		return err
	}

	return nil
}
