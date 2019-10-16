// Copyright (c) 2018-2019 Splunk Inc. All rights reserved.
// Use of this source code is governed by an Apache 2 style
// license that can be found in the LICENSE file.

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
	if err := CreateResource(client, secrets); err != nil {
		return err
	}

	// create splunk defaults (for inline config)
	if cr.Spec.Defaults != "" {
		defaults := enterprise.GetSplunkDefaults(cr)
		defaults.SetOwnerReferences(append(defaults.GetOwnerReferences(), resources.AsOwner(cr)))
		if err := CreateResource(client, defaults); err != nil {
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
	if cr.Spec.LicenseUrl != "" {
		overrides["SPLUNK_LICENSE_URI"] = cr.Spec.LicenseUrl
	}
	if cr.Spec.EnableDFS {
		overrides = enterprise.AppendSplunkDfsOverrides(overrides, cr.GetIdentifier(), cr.Spec.Topology.SearchHeads)
	}

	err := CreateSplunkStatefulSet(
		cr,
		client,
		enterprise.SPLUNK_STANDALONE,
		cr.Spec.Topology.Standalones,
		enterprise.GetSplunkConfiguration(overrides, cr.Spec.Defaults, cr.Spec.DefaultsUrl),
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

	err := CreateResource(client, enterprise.GetSplunkService(cr, enterprise.SPLUNK_LICENSE_MASTER, false))
	if err != nil {
		return err
	}

	err = CreateSplunkStatefulSet(
		cr,
		client,
		enterprise.SPLUNK_LICENSE_MASTER,
		1,
		enterprise.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.Topology.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE":        "splunk_license_master",
				"SPLUNK_LICENSE_URI": cr.Spec.LicenseUrl,
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

	err := CreateResource(client, enterprise.GetSplunkService(cr, enterprise.SPLUNK_CLUSTER_MASTER, false))
	if err != nil {
		return err
	}

	err = CreateSplunkStatefulSet(
		cr,
		client,
		enterprise.SPLUNK_CLUSTER_MASTER,
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

	err := CreateResource(client, enterprise.GetSplunkService(cr, enterprise.SPLUNK_DEPLOYER, false))
	if err != nil {
		return err
	}

	err = CreateSplunkStatefulSet(
		cr,
		client,
		enterprise.SPLUNK_DEPLOYER,
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

	err := CreateSplunkStatefulSet(
		cr,
		client,
		enterprise.SPLUNK_INDEXER,
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

	err = CreateResource(client, enterprise.GetSplunkService(cr, enterprise.SPLUNK_INDEXER, true))
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

	err := CreateSplunkStatefulSet(
		cr,
		client,
		enterprise.SPLUNK_SEARCH_HEAD,
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

	err = CreateResource(client, enterprise.GetSplunkService(cr, enterprise.SPLUNK_SEARCH_HEAD, true))
	if err != nil {
		return err
	}

	err = CreateResource(client, enterprise.GetSplunkService(cr, enterprise.SPLUNK_SEARCH_HEAD, false))
	if err != nil {
		return err
	}

	return err
}

// LaunchSparkCluster create Kubernetes Deployment (master), StatefulSet (workers) and services Spark cluster.
func LaunchSparkCluster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := CreateResource(client, spark.GetSparkService(cr, spark.SPARK_MASTER, false, spark.GetSparkMasterServicePorts()))
	if err != nil {
		return err
	}

	err = CreateSparkDeployment(cr, client, spark.SPARK_MASTER, 1, spark.GetSparkMasterConfiguration(), spark.GetSparkMasterContainerPorts())
	if err != nil {
		return err
	}

	err = CreateSparkStatefulSet(cr,
		client,
		spark.SPARK_WORKER,
		cr.Spec.Topology.SparkWorkers,
		spark.GetSparkWorkerConfiguration(cr.GetIdentifier()),
		spark.GetSparkWorkerContainerPorts())
	if err != nil {
		return err
	}

	err = CreateResource(client, spark.GetSparkService(cr, spark.SPARK_WORKER, true, spark.GetSparkWorkerServicePorts()))
	if err != nil {
		return err
	}

	return nil
}
