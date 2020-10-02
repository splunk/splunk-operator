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

package enterprise

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	//"github.com/go-logr/stdr"
	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// kubernetes logger used by splunk.enterprise package
var log = logf.Log.WithName("splunk.enterprise")

// ApplySplunkConfig reconciles the state of Kubernetes Secrets, ConfigMaps and other general settings for Splunk Enterprise instances.
func ApplySplunkConfig(client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterprisev1.CommonSplunkSpec, instanceType InstanceType) (*corev1.Secret, error) {
	var err error

	// Creates/updates the namespace scoped "splunk-secrets" K8S secret object
	namespaceScopedSecret, err := splutil.ApplyNamespaceScopedSecretObject(client, cr.GetNamespace())
	if err != nil {
		return nil, err
	}

	// Set secret owner references
	err = splutil.SetSecretOwnerRef(client, namespaceScopedSecret, cr)
	if err != nil {
		return nil, err
	}

	// create splunk defaults (for inline config)
	if spec.Defaults != "" {
		defaultsMap := getSplunkDefaults(cr.GetName(), cr.GetNamespace(), instanceType, spec.Defaults)
		defaultsMap.SetOwnerReferences(append(defaultsMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
		if err = splctrl.ApplyConfigMap(client, defaultsMap); err != nil {
			return nil, err
		}
	}

	return namespaceScopedSecret, nil
}

// getIndexerExtraEnv returns extra environment variables used by indexer clusters
func getIndexerExtraEnv(cr splcommon.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_INDEXER_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkIndexer, cr.GetName(), replicas, false),
		},
	}
}

// getClusterMasterExtraEnv returns extra environment variables used by indexer clusters
func getClusterMasterExtraEnv(cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec) []corev1.EnvVar {
	if spec.ClusterMasterRef.Name != "" {
		clusterMasterURL := GetSplunkServiceName(SplunkClusterMaster, spec.ClusterMasterRef.Name, false)
		if spec.ClusterMasterRef.Namespace != "" {
			clusterMasterURL = splcommon.GetServiceFQDN(spec.ClusterMasterRef.Namespace, clusterMasterURL)
		}
		return []corev1.EnvVar{
			{
				Name:  "SPLUNK_CLUSTER_MASTER_URL",
				Value: clusterMasterURL,
			},
		}
	}
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_CLUSTER_MASTER_URL",
			Value: GetSplunkServiceName(SplunkClusterMaster, cr.GetName(), false),
		},
	}
}

// getStandaloneExtraEnv returns extra environment variables used by monitoring console
func getStandaloneExtraEnv(cr splcommon.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_STANDALONE_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkStandalone, cr.GetName(), replicas, false),
		},
	}
}

// getLicenseMasterURL returns URL of license master
func getLicenseMasterURL(cr splcommon.MetaObject, spec *enterprisev1.CommonSplunkSpec) []corev1.EnvVar {
	if spec.LicenseMasterRef.Name != "" {
		licenseMasterURL := GetSplunkServiceName(SplunkLicenseMaster, spec.LicenseMasterRef.Name, false)
		if spec.LicenseMasterRef.Namespace != "" {
			licenseMasterURL = splcommon.GetServiceFQDN(spec.LicenseMasterRef.Namespace, licenseMasterURL)
		}
		return []corev1.EnvVar{
			{
				Name:  "SPLUNK_LICENSE_MASTER_URL",
				Value: licenseMasterURL,
			},
		}
	}
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_LICENSE_MASTER_URL",
			Value: GetSplunkServiceName(SplunkLicenseMaster, cr.GetName(), false),
		},
	}
}

// getSearchHeadExtraEnv returns extra environment variables used by search head clusters
func getSearchHeadEnv(cr *enterprisev1.SearchHeadCluster) []corev1.EnvVar {

	// get search head env variables with deployer
	env := getSearchHeadExtraEnv(cr, cr.Spec.Replicas)
	env = append(env, corev1.EnvVar{
		Name:  "SPLUNK_DEPLOYER_URL",
		Value: GetSplunkServiceName(SplunkDeployer, cr.GetName(), false),
	})

	return env
}

// getSearchHeadExtraEnv returns extra environment variables used by search head clusters
func getSearchHeadExtraEnv(cr splcommon.MetaObject, replicas int32) []corev1.EnvVar {
	return []corev1.EnvVar{
		{
			Name:  "SPLUNK_SEARCH_HEAD_URL",
			Value: GetSplunkStatefulsetUrls(cr.GetNamespace(), SplunkSearchHead, cr.GetName(), replicas, false),
		}, {
			Name:  "SPLUNK_SEARCH_HEAD_CAPTAIN_URL",
			Value: GetSplunkStatefulsetURL(cr.GetNamespace(), SplunkSearchHead, cr.GetName(), 0, false),
		},
	}
}

// GetSmartstoreSecrets is used to retrieve S3 access key and secrete keys.
// To do: sgontla:
// 1. Support multiple secret objects
// 2. default secret object
// volume name other than default, look for the specific secret object, else fetch from defaults
func GetSmartstoreSecrets(client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterprisev1.SmartStoreSpec) (string, string, error) {
	namespaceScopedSecret, err := splutil.GetNamespaceScopedSecret(client, cr.GetNamespace())
	if err != nil {
		return "", "", err
	}

	accessKey := string(namespaceScopedSecret.Data[s3AccessKey])
	secretKey := string(namespaceScopedSecret.Data[s3SecretKey])

	if accessKey == "" {
		return "", "", fmt.Errorf("S3 Access Key is missing")
	} else if secretKey == "" {
		return "", "", fmt.Errorf("S3 Secret Key is missing")
	}

	return accessKey, secretKey, nil
}

// CreateSmartStoreConfigMap creates the configMap with Smartstore config in INI format
func CreateSmartStoreConfigMap(client splcommon.ControllerClient, cr splcommon.MetaObject,
	smartstore *enterprisev1.SmartStoreSpec) (*corev1.ConfigMap, error) {

	var crKind string
	crKind = cr.GetObjectKind().GroupVersionKind().Kind

	scopedLog := log.WithName("CreateSmartStoreConfigMap").WithValues("kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	if !isSmartstoreConfigured(smartstore) {
		return nil, fmt.Errorf("Smartstore is not configured")
	}

	// Get the list of volumes in INI format
	volumesConfIni, err := GetSmartstoreVolumesConfig(client, cr, smartstore)
	if err != nil {
		return nil, err
	} else if volumesConfIni == "" {
		return nil, fmt.Errorf("Volume stanza list is empty")
	}

	// Get the list of indexes in INI format
	indexesConfIni := GetSmartstoreIndexesConfig(smartstore.IndexList)

	// To do: sgontla: Do we need to error out, if indexes config is missing?
	// Indexes without volume is a No, but volumes without indexes should be fine?
	if indexesConfIni == "" {
		scopedLog.Info("Index stanza list is empty")
	}

	iniSmartstoreConf := fmt.Sprintf(`%s %s`, volumesConfIni, indexesConfIni)

	// Create smartstore config consisting indexes.conf
	smartstoreConfigMap := getSplunkSmartstoreConfigMap(cr.GetName(), cr.GetNamespace(), crKind, iniSmartstoreConf)
	smartstoreConfigMap.SetOwnerReferences(append(smartstoreConfigMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
	if err := splctrl.ApplyConfigMap(client, smartstoreConfigMap); err != nil {
		return nil, err
	}

	return smartstoreConfigMap, nil
}
