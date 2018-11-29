package stub

import (
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
	"operator/splunk-operator/pkg/stub/resources"
	"operator/splunk-operator/pkg/stub/spark"
	"operator/splunk-operator/pkg/stub/splunk"
)


func LaunchDeployment(cr *v1alpha1.SplunkInstance) error {

	if cr.Spec.Standalones > 0 {
		LaunchStandalones(cr)
	}

	if cr.Spec.Indexers > 0 && cr.Spec.SearchHeads > 0 {
		if cr.Spec.SparkWorkers > 0 && cr.Spec.EnableDFS {
			LaunchCluster(cr, true)
			LaunchSparkCluster(cr)
		} else {
			LaunchCluster(cr, false)
		}
	}

	return nil
}


func LaunchStandalones(cr *v1alpha1.SplunkInstance) error {

	err := resources.CreateSplunkDeployment(cr, splunk.SPLUNK_STANDALONE, splunk.GetIdentifier(cr), cr.Spec.Standalones, splunk.GetSplunkConfiguration(nil), nil)
	if err != nil {
		return err
	}

	return nil
}


func LaunchCluster(cr *v1alpha1.SplunkInstance, enableDFS bool) error {

	err := LaunchLicenseMaster(cr)
	if err != nil {
		return err
	}

	err = LaunchClusterMaster(cr)
	if err != nil {
		return err
	}

	if cr.Spec.SearchHeads > 1 {
		err = LaunchDeployer(cr)
		if err != nil {
			return err
		}
	}

	err = LaunchIndexers(cr)
	if err != nil {
		return nil
	}

	err = LaunchSearchHeads(cr, enableDFS)
	if err != nil {
		return nil
	}

	return nil
}


func LaunchLicenseMaster(cr *v1alpha1.SplunkInstance) error {
	err := resources.CreateService(cr, splunk.SPLUNK_LICENSE_MASTER, splunk.GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = resources.CreateSplunkDeployment(
		cr,
		splunk.SPLUNK_LICENSE_MASTER,
		splunk.GetIdentifier(cr),
		1,
		splunk.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_license_master",
				"SPLUNK_LICENSE_URI": "/license.lic",
			},
		),
		splunk.GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return nil
}


func LaunchClusterMaster(cr *v1alpha1.SplunkInstance) error {
	err := resources.CreateService(cr, splunk.SPLUNK_CLUSTER_MASTER, splunk.GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = resources.CreateSplunkDeployment(
		cr,
		splunk.SPLUNK_CLUSTER_MASTER,
		splunk.GetIdentifier(cr),
		1,
		splunk.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_cluster_master",
			},
		),
		splunk.GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}


func LaunchDeployer(cr *v1alpha1.SplunkInstance) error {
	err := resources.CreateService(cr, splunk.SPLUNK_DEPLOYER, splunk.GetIdentifier(cr), false)
	if err != nil {
		return err
	}

	err = resources.CreateSplunkDeployment(
		cr,
		splunk.SPLUNK_DEPLOYER,
		splunk.GetIdentifier(cr),
		1,
		splunk.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_deployer",
			},
		),
		splunk.GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}


func LaunchIndexers(cr *v1alpha1.SplunkInstance) error {
	err := resources.CreateSplunkStatefulSet(
		cr,
		splunk.SPLUNK_INDEXER,
		splunk.GetIdentifier(cr),
		cr.Spec.Indexers,
		splunk.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			map[string]string{
				"SPLUNK_ROLE": "splunk_indexer",
			},
		),
		splunk.GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}


func LaunchSearchHeads(cr *v1alpha1.SplunkInstance, enableDFS bool) error {

	overrides := map[string]string{
		"SPLUNK_ROLE": "splunk_search_head",
	}

	if enableDFS {
		overrides["ENABLE_DFS"] = "true"
		overrides["DFS_MASTER_PORT"] = "9000"
		overrides["SPARK_MASTER_HOSTNAME"] = spark.GetSparkServiceName(spark.SPARK_MASTER, splunk.GetIdentifier(cr))
		overrides["SPARK_MASTER_WEBUI_PORT"] = "8009"
		overrides["DFS_EXECUTOR_STARTING_PORT"] = "17500"
		if cr.Spec.SearchHeads > 1 {
			overrides["DFW_NUM_SLOTS_ENABLED"] = "true"
		} else {
			overrides["DFW_NUM_SLOTS_ENABLED"] = "false"
		}
	}

	err := resources.CreateSplunkStatefulSet(
		cr,
		splunk.SPLUNK_SEARCH_HEAD,
		splunk.GetIdentifier(cr),
		cr.Spec.SearchHeads,
		splunk.GetSplunkClusterConfiguration(
			cr,
			cr.Spec.SearchHeads > 1,
			overrides,
		),
		splunk.GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}


func LaunchSparkCluster(cr *v1alpha1.SplunkInstance) error {

	err := resources.CreateSparkService(cr, spark.SPARK_MASTER, splunk.GetIdentifier(cr), false, spark.GetSparkMasterServicePorts())
	if err != nil {
		return err
	}

	err = resources.CreateSparkDeployment(cr, spark.SPARK_MASTER, splunk.GetIdentifier(cr), 1, spark.GetSparkMasterConfiguration(), spark.GetSparkMasterContainerPorts())
	if err != nil {
		return err
	}


	err = resources.CreateSparkStatefulSet(cr, spark.SPARK_WORKER, splunk.GetIdentifier(cr), cr.Spec.SparkWorkers, spark.GetSparkWorkerConfiguration(splunk.GetIdentifier(cr)), splunk.GetSplunkDNSConfiguration(cr), spark.GetSparkWorkerContainerPorts(), spark.GetSparkWorkerServicePorts())
	if err != nil {
		return err
	}

	return nil
}