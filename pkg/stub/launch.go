package stub

import (
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
	"operator/splunk-operator/pkg/stub/resources"
	"operator/splunk-operator/pkg/stub/splunk"
)


func LaunchDeployment(cr *v1alpha1.SplunkInstance) error {

	if cr.Spec.Standalones > 0 {
		LaunchStandalones(cr)
	}

	if cr.Spec.Indexers > 0 && cr.Spec.SearchHeads > 0 {
		LaunchCluster(cr)
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


func LaunchCluster(cr *v1alpha1.SplunkInstance) error {

	err := LaunchLicenseMaster(cr)
	if err != nil {
		return err
	}

	err = LaunchClusterMaster(cr)
	if err != nil {
		return err
	}

	err = LaunchDeployer(cr)
	if err != nil {
		return err
	}

	err = LaunchIndexers(cr)
	if err != nil {
		return nil
	}

	err = LaunchSearchHeads(cr)
	if err != nil {
		return nil
	}

	//getClusterConf := func(role string) map[string]string {
	//	return map[string]string{
	//		"SPLUNK_CLUSTER_MASTER_URL": GetClusterComponetUrls(cr, GetIdentifier(cr), SPLUNK_MASTER, 1),
	//		"SPLUNK_INDEXER_URL": GetClusterComponetUrls(cr, GetIdentifier(cr), SPLUNK_INDEXER, cr.Spec.Indexers),
	//		"SPLUNK_SEARCH_HEAD_URL": GetClusterComponetUrls(cr, GetIdentifier(cr), SPLUNK_SEARCH_HEAD, cr.Spec.SearchHeads),
	//		"SPLUNK_ROLE": role,
	//	}
	//}
	//
	//numMasters := 0
	//if cr.Spec.SearchHeads > 0 && cr.Spec.Indexers > 0 {
	//	numMasters = 1
	//}
	//err := CreateSplunkStatefulSet(cr, GetIdentifier(cr), SPLUNK_MASTER, numMasters, GetSplunkConfiguration(getClusterConf("splunk_cluster_master")))
	//if err != nil {
	//	return err
	//}
	//
	//err = CreateSplunkStatefulSet(cr, GetIdentifier(cr), SPLUNK_INDEXER, cr.Spec.Indexers, GetSplunkConfiguration(getClusterConf("splunk_indexer")))
	//if err != nil {
	//	return err
	//}
	//
	//err = CreateSplunkStatefulSet(cr, GetIdentifier(cr), SPLUNK_SEARCH_HEAD, cr.Spec.SearchHeads, GetSplunkConfiguration(getClusterConf("splunk_search_head")))
	//if err != nil {
	//	return err
	//}
	//
	//if numMasters == 1 {
	//	err = CreateExposeService(cr, GetIdentifier(cr), SPLUNK_MASTER, EXPOSE_MASTER_SERVICE,0)
	//	if err != nil {
	//		return err
	//	}
	//}

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
			true,
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
			true,
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
			true,
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
			true,
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


func LaunchSearchHeads(cr *v1alpha1.SplunkInstance) error {
	err := resources.CreateSplunkStatefulSet(
		cr,
		splunk.SPLUNK_SEARCH_HEAD,
		splunk.GetIdentifier(cr),
		cr.Spec.SearchHeads,
		splunk.GetSplunkClusterConfiguration(
			cr,
			true,
			map[string]string{
				"SPLUNK_ROLE": "splunk_search_head",
			},
		),
		splunk.GetSplunkDNSConfiguration(cr),
	)
	if err != nil {
		return err
	}

	return err
}