package deploy

import (
	"fmt"
	"git.splunk.com/splunk-operator/pkg/apis/enterprise/v1alpha1"
	"git.splunk.com/splunk-operator/pkg/splunk/enterprise"
	"git.splunk.com/splunk-operator/pkg/splunk/resources"
	"git.splunk.com/splunk-operator/pkg/splunk/spark"
	"sigs.k8s.io/controller-runtime/pkg/client"
)


func LaunchDeployment(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	if cr.Spec.Topology.Standalones > 0 {
		LaunchStandalones(cr, client)
	}

	if cr.Spec.Topology.Indexers > 0 && cr.Spec.Topology.SearchHeads > 0 {
		if cr.Spec.Topology.SparkWorkers > 0 && cr.Spec.Config.EnableDFS {
			err := LaunchCluster(cr, client, true)
			if err != nil {
				return err
			}

			err = LaunchSparkCluster(cr, client)
			if err != nil {
				return err
			}
		} else {
			err := LaunchCluster(cr, client, false)
			if err != nil {
				return err
			}
		}
	}

	return nil
}


func LaunchStandalones(cr *v1alpha1.SplunkEnterprise, client client.Client) error {

	overrides := map[string]string{"SPLUNK_ROLE": "splunk_standalone"}
	if cr.Spec.Config.SplunkLicense.LicensePath != "" {
		overrides["SPLUNK_LICENSE_URI"] = fmt.Sprintf("%s%s", enterprise.LICENSE_MOUNT_LOCATION, cr.Spec.Config.SplunkLicense.LicensePath)
	}

	err := resources.CreateSplunkDeployment(cr, client, enterprise.SPLUNK_STANDALONE, enterprise.GetIdentifier(cr), cr.Spec.Topology.Standalones, enterprise.GetSplunkConfiguration(cr, overrides), nil)
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

	err = LaunchSearchHeads(cr, client, enableDFS)
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
				"SPLUNK_LICENSE_URI": fmt.Sprintf("%s%s", enterprise.LICENSE_MOUNT_LOCATION, cr.Spec.Config.SplunkLicense.LicensePath),
			},
		),
		enterprise.GetSplunkDNSConfiguration(cr),
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
		enterprise.GetSplunkDNSConfiguration(cr),
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
		enterprise.GetSplunkDNSConfiguration(cr),
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
		enterprise.GetSplunkDNSConfiguration(cr),
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
		overrides["SPARK_MASTER_HOSTNAME"] = spark.GetSparkServiceName(spark.SPARK_MASTER, enterprise.GetIdentifier(cr))
		overrides["SPARK_MASTER_WEBUI_PORT"] = "8009"
		overrides["DFS_EXECUTOR_STARTING_PORT"] = "17500"
		if cr.Spec.Topology.SearchHeads > 1 {
			overrides["DFW_NUM_SLOTS_ENABLED"] = "true"
		} else {
			overrides["DFW_NUM_SLOTS_ENABLED"] = "false"
		}
	}

	err := resources.CreateSplunkStatefulSet(
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
		enterprise.GetSplunkDNSConfiguration(cr),
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

	err = resources.CreateSparkStatefulSet(cr, client, spark.SPARK_WORKER, enterprise.GetIdentifier(cr), cr.Spec.Topology.SparkWorkers, spark.GetSparkWorkerConfiguration(enterprise.GetIdentifier(cr)), enterprise.GetSplunkDNSConfiguration(cr), spark.GetSparkWorkerContainerPorts(), spark.GetSparkWorkerServicePorts())
	if err != nil {
		return err
	}

	return nil
}