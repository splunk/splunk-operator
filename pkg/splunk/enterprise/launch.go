package enterprise

import (
	"git.splunk.com/splunk-operator/pkg/splunk/spark"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


func LaunchDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	if cr.Spec.Standalones > 0 {
		LaunchStandalones(cr, client)
	}

	if cr.Spec.Indexers > 0 && cr.Spec.SearchHeads > 0 {
		if cr.Spec.SparkWorkers > 0 && cr.Spec.EnableDFS {
			LaunchCluster(cr, client, true)
			LaunchSparkCluster(cr, client)
		} else {
			LaunchCluster(cr, client, false)
		}
	}

	return nil
}


func LaunchStandalones(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := CreateSplunkDeployment(cr, client, SPLUNK_STANDALONE, GetIdentifier(cr), cr.Spec.Standalones, GetSplunkConfiguration(nil), nil)
	if err != nil {
		return err
	}

	return nil
}


func LaunchCluster(cr *v1alpha1.SplunkEnterprise, client client.Client, enableDFS bool) error {

	err := LaunchLicenseMaster(cr, client)
	if err != nil {
		return err
	}

	err = LaunchClusterMaster(cr, client)
	if err != nil {
		return err
	}

	if cr.Spec.SearchHeads > 1 {
		err = LaunchDeployer(cr, client)
		if err != nil {
			return err
		}
	}

	err = LaunchIndexers(cr, client)
	if err != nil {
		return nil
	}

	err = LaunchSearchHeads(cr, client, enableDFS)
	if err != nil {
		return nil
	}

	return nil
}


func LaunchLicenseMaster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := CreateService(cr, client, SPLUNK_LICENSE_MASTER, GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = CreateSplunkDeployment(
		cr,
		client,
		SPLUNK_LICENSE_MASTER,
		GetIdentifier(cr),
		1,
		GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_license_master",
				"SPLUNK_LICENSE_URI": "/license.lic",
			},
		),
		GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return nil
}


func LaunchClusterMaster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := CreateService(cr, client, SPLUNK_CLUSTER_MASTER, GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = CreateSplunkDeployment(
		cr,
		client,
		SPLUNK_CLUSTER_MASTER,
		GetIdentifier(cr),
		1,
		GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_cluster_master",
			},
		),
		GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}


func LaunchDeployer(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := CreateService(cr, client, SPLUNK_DEPLOYER, GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = CreateSplunkDeployment(
		cr,
		client,
		SPLUNK_DEPLOYER,
		GetIdentifier(cr),
		1,
		GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_deployer",
			},
		),
		GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}


func LaunchIndexers(cr *v1alpha1.SplunkEnterprise, client client.Client) error {
	err := CreateSplunkStatefulSet(
		cr,
		client,
		SPLUNK_INDEXER,
		GetIdentifier(cr),
		cr.Spec.Indexers,
		GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_indexer",
			},
		),
		GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}


func LaunchSearchHeads(cr *v1alpha1.SplunkEnterprise, client client.Client, enableDFS bool) error {

	overrides := map[string]string{
		"SPLUNK_ROLE": "splunk_search_head",
	}

	if enableDFS {
		overrides["ENABLE_DFS"] = "true"
		overrides["DFS_MASTER_PORT"] = "9000"
		overrides["SPARK_MASTER_HOSTNAME"] = spark.GetSparkServiceName(spark.SPARK_MASTER, GetIdentifier(cr))
		overrides["SPARK_MASTER_WEBUI_PORT"] = "8009"
		overrides["DFS_EXECUTOR_STARTING_PORT"] = "17500"
		if cr.Spec.SearchHeads > 1 {
			overrides["DFW_NUM_SLOTS_ENABLED"] = "true"
		} else {
			overrides["DFW_NUM_SLOTS_ENABLED"] = "false"
		}
	}

	err := CreateSplunkStatefulSet(
		cr,
		client,
		SPLUNK_SEARCH_HEAD,
		GetIdentifier(cr),
		cr.Spec.SearchHeads,
		GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			overrides,
		),
		GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}


func LaunchSparkCluster(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	err := CreateSparkService(cr, client, spark.SPARK_MASTER, GetIdentifier(cr), false, spark.GetSparkMasterServicePorts())
	if err != nil {
		return err
	}

	err = CreateSparkDeployment(cr, client, spark.SPARK_MASTER, GetIdentifier(cr), 1, spark.GetSparkMasterConfiguration(), spark.GetSparkMasterContainerPorts())
	if err != nil {
		return err
	}


	err = CreateSparkStatefulSet(cr, client, spark.SPARK_WORKER, GetIdentifier(cr), cr.Spec.SparkWorkers, spark.GetSparkWorkerConfiguration(GetIdentifier(cr)), GetSplunkDNSConfiguration(cr), spark.GetSparkWorkerContainerPorts(), spark.GetSparkWorkerServicePorts())
	if err != nil {
		return err
	}

	return nil
}