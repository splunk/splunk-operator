package deploy

import (
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/enterprise"
	"git.splunk.com/splunk-operator/pkg/splunk/resources"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


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
	resources.AddOwnerRefToObject(secrets, resources.AsOwner(cr))
	if err := resources.CreateResource(client, secrets); err != nil {
		return err
	}

	// create splunk defaults (for inline config)
	if (cr.Spec.Defaults != "") {
		defaults := enterprise.GetSplunkDefaults(cr)
		resources.AddOwnerRefToObject(defaults, resources.AsOwner(cr))
		if err := resources.CreateResource(client, defaults); err != nil {
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


func LaunchStandalones(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	overrides := map[string]string{"SPLUNK_ROLE": "splunk_standalone"}
	if cr.Spec.LicenseUrl != "" {
		overrides["SPLUNK_LICENSE_URI"] = cr.Spec.LicenseUrl
	}
	enterprise.AppendSplunkDfsOverrides(cr, overrides)

	err := resources.CreateSplunkDeployment(cr, client, enterprise.SPLUNK_STANDALONE, enterprise.GetIdentifier(cr), cr.Spec.Topology.Standalones, enterprise.GetSplunkConfiguration(cr, overrides))
	if err != nil {
		return err
	}

	return nil
}


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


func LaunchLicenseMaster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := resources.CreateService(cr, client, enterprise.SPLUNK_LICENSE_MASTER, enterprise.GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = resources.CreateSplunkDeployment(
		cr,
		client,
		enterprise.SPLUNK_LICENSE_MASTER,
		enterprise.GetIdentifier(cr),
		1,
		enterprise.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.Topology.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_license_master",
				"SPLUNK_LICENSE_URI": cr.Spec.LicenseUrl,
			},
		),
	)
	if err != nil {
		return err
	}

	return nil
}


func LaunchClusterMaster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := resources.CreateService(cr, client, enterprise.SPLUNK_CLUSTER_MASTER, enterprise.GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = resources.CreateSplunkDeployment(
		cr,
		client,
		enterprise.SPLUNK_CLUSTER_MASTER,
		enterprise.GetIdentifier(cr),
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


func LaunchDeployer(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := resources.CreateService(cr, client, enterprise.SPLUNK_DEPLOYER, enterprise.GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = resources.CreateSplunkDeployment(
		cr,
		client,
		enterprise.SPLUNK_DEPLOYER,
		enterprise.GetIdentifier(cr),
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


func LaunchIndexers(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := resources.CreateSplunkStatefulSet(
		cr,
		client,
		enterprise.SPLUNK_INDEXER,
		enterprise.GetIdentifier(cr),
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

	return err
}


func LaunchSearchHeads(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := resources.CreateService(cr, client, enterprise.SPLUNK_SEARCH_HEAD, enterprise.GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	overrides := map[string]string{"SPLUNK_ROLE": "splunk_search_head"}
	enterprise.AppendSplunkDfsOverrides(cr, overrides)

	err = resources.CreateSplunkStatefulSet(
		cr,
		client,
		enterprise.SPLUNK_SEARCH_HEAD,
		enterprise.GetIdentifier(cr),
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

	return err
}


func LaunchSparkCluster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := resources.CreateSparkService(cr, client, spark.SPARK_MASTER, enterprise.GetIdentifier(cr), false, spark.GetSparkMasterServicePorts())
	if err != nil {
		return err
	}

	err = resources.CreateSparkDeployment(cr, client, spark.SPARK_MASTER, enterprise.GetIdentifier(cr), 1, spark.GetSparkMasterConfiguration(), spark.GetSparkMasterContainerPorts())
	if err != nil {
		return err
	}

	err = resources.CreateSparkStatefulSet(cr, client, spark.SPARK_WORKER, enterprise.GetIdentifier(cr), cr.Spec.Topology.SparkWorkers, spark.GetSparkWorkerConfiguration(enterprise.GetIdentifier(cr)), spark.GetSparkWorkerContainerPorts(), spark.GetSparkWorkerServicePorts())
	if err != nil {
		return err
	}

	return nil
}