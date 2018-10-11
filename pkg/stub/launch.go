package stub

import (
	"k8s.io/api/core/v1"
	"operator/splunk-operator/pkg/apis/splunk-instance/v1alpha1"
)


func GetIdentifier(cr *v1alpha1.SplunkInstance) string {
	return cr.GetObjectMeta().GetName()
}


func GetSplunkConfiguration(overrides map[string]string) []v1.EnvVar {
	conf := []v1.EnvVar{
		{
			Name: "SPLUNK_HOME",
			Value: "/opt/splunk",
		},{
			Name: "SPLUNK_PASSWORD",
			Value: "helloworld",
		},{
			Name: "SPLUNK_START_ARGS",
			Value: "--accept-license",
		},
	}

	if overrides != nil {
		for k, v := range overrides {
			conf = append(conf, v1.EnvVar{
				Name: k,
				Value: v,
			})
		}
	}

	return conf
}


func LaunchStandalones(cr *v1alpha1.SplunkInstance) error {

	err := CreateSplunkStatefulSet(cr, GetIdentifier(cr), SPLUNK_STANDALONE, cr.Spec.Standalones, GetSplunkConfiguration(nil))
	if err != nil {
		return err
	}

	err = ExposeStandaloneInstances(cr, GetIdentifier(cr))
	if err != nil {
		return err
	}

	return nil
}


func LaunchCluster(cr *v1alpha1.SplunkInstance) error {

	getClusterConf := func(role string) map[string]string {
		return map[string]string{
			"SPLUNK_CLUSTER_MASTER_URL": GetClusterComponetUrls(cr, GetIdentifier(cr), SPLUNK_MASTER, 1),
			"SPLUNK_INDEXER_URL": GetClusterComponetUrls(cr, GetIdentifier(cr), SPLUNK_INDEXER, cr.Spec.Indexers),
			"SPLUNK_SEARCH_HEAD_URL": GetClusterComponetUrls(cr, GetIdentifier(cr), SPLUNK_SEARCH_HEAD, cr.Spec.SearchHeads),
			"SPLUNK_ROLE": role,
		}
	}

	numMasters := 0
	if cr.Spec.SearchHeads > 0 && cr.Spec.Indexers > 0 {
		numMasters = 1
	}
	err := CreateSplunkStatefulSet(cr, GetIdentifier(cr), SPLUNK_MASTER, numMasters, GetSplunkConfiguration(getClusterConf("splunk_cluster_master")))
	if err != nil {
		return err
	}

	err = CreateSplunkStatefulSet(cr, GetIdentifier(cr), SPLUNK_INDEXER, cr.Spec.Indexers, GetSplunkConfiguration(getClusterConf("splunk_indexer")))
	if err != nil {
		return err
	}

	err = CreateSplunkStatefulSet(cr, GetIdentifier(cr), SPLUNK_SEARCH_HEAD, cr.Spec.SearchHeads, GetSplunkConfiguration(getClusterConf("splunk_search_head")))
	if err != nil {
		return err
	}

	if numMasters == 1 {
		err = CreateExposeService(cr, GetIdentifier(cr), SPLUNK_MASTER, EXPOSE_MASTER_SERVICE,0)
		if err != nil {
			return err
		}
	}

	return nil
}


func UpdateStandalones(cr *v1alpha1.SplunkInstance) error {

	err := UpdateSplunkInstance(cr, GetIdentifier(cr), SPLUNK_STANDALONE, cr.Spec.Standalones)
	if err != nil {
		return err
	}

	err = UpdateExposeServices(cr, GetIdentifier(cr), SPLUNK_STANDALONE, EXPOSE_STANDALONE_SERVICE, cr.Spec.Standalones)
	if err != nil {
		return err
	}

	return nil
}


func UpdateCluster(cr *v1alpha1.SplunkInstance) error {

	numMasters := 0
	if cr.Spec.SearchHeads > 0 && cr.Spec.Indexers > 0 {
		numMasters = 1
	}
	err := UpdateSplunkInstance(cr, GetIdentifier(cr), SPLUNK_MASTER, numMasters)
	if err != nil {
		return err
	}

	err = UpdateSplunkInstance(cr, GetIdentifier(cr), SPLUNK_SEARCH_HEAD, cr.Spec.SearchHeads)
	if err != nil {
		return err
	}

	err = UpdateSplunkInstance(cr, GetIdentifier(cr), SPLUNK_INDEXER, cr.Spec.Indexers)
	if err != nil {
		return err
	}

	err = UpdateExposeServices(cr, GetIdentifier(cr), SPLUNK_MASTER, EXPOSE_MASTER_SERVICE, numMasters)
	if err != nil {
		return err
	}

	return nil
}


func CleanupLaunch(cr *v1alpha1.SplunkInstance) error {
	err := DeletePersistentVolumeClaims(cr, GetIdentifier(cr))
	return err
}


func ExposeStandaloneInstances(cr *v1alpha1.SplunkInstance, identifier string) error {
	for i := 0; i < cr.Spec.Standalones; i++ {
		err := CreateExposeService(cr, identifier, SPLUNK_STANDALONE, EXPOSE_STANDALONE_SERVICE, int(i))
		if err != nil {
			return err
		}
	}
	return nil
}