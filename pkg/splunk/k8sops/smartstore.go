// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package k8sops

import (
	"context"
	"fmt"
	"reflect"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

const (
	s3AccessKey = "s3_access_key"
	s3SecretKey = "s3_secret_key"
)

func GetSmartstoreRemoteVolumeSecrets(ctx context.Context, volume enterpriseApi.VolumeSpec, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec) (string, string, string, error) {
	// Get event publisher from context
	eventPublisher := GetEventPublisher(ctx, cr)

	namespaceScopedSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), volume.SecretRef)
	if err != nil {
		// Emit event for missing secret
		if k8serrors.IsNotFound(err) {
			if eventPublisher != nil {
				eventPublisher.Warning(ctx, splcommon.EventReasonSecretMissing,
					fmt.Sprintf("Required secret '%s' not found in namespace '%s'. Create secret to proceed.", volume.SecretRef, cr.GetNamespace()))
			}
		}
		return "", "", "", err
	}

	accessKey := string(namespaceScopedSecret.Data[s3AccessKey])
	secretKey := string(namespaceScopedSecret.Data[s3SecretKey])

	splutil.SetSecretOwnerRef(ctx, client, volume.SecretRef, cr)

	if accessKey == "" {
		if eventPublisher != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonSecretInvalid,
				fmt.Sprintf("Secret '%s' missing required fields: %s. Update secret with required data.", namespaceScopedSecret.GetName(), "accessKey"))
		}
		return "", "", "", fmt.Errorf("s3 Access Key is missing")
	} else if secretKey == "" {
		if eventPublisher != nil {
			eventPublisher.Warning(ctx, splcommon.EventReasonSecretInvalid,
				fmt.Sprintf("Secret '%s' missing required fields: %s. Update secret with required data.", namespaceScopedSecret.GetName(), "s3SecretKey"))
		}
		return "", "", "", fmt.Errorf("s3 Secret Key is missing")
	}

	return accessKey, secretKey, namespaceScopedSecret.ResourceVersion, nil
}

func ApplySmartstoreConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject,
	smartstore *enterpriseApi.SmartStoreSpec) (*corev1.ConfigMap, bool, error) {

	var crKind string
	var configMapDataChanged bool
	crKind = cr.GetObjectKind().GroupVersionKind().Kind

	scopedLog := logging.FromContext(ctx).With("func", "ApplySmartStoreConfigMap", "kind", crKind, "name", cr.GetName(), "namespace", cr.GetNamespace())

	// 1. Prepare the indexes.conf entries
	mapSplunkConfDetails := make(map[string]string)

	// Get the list of volumes in INI format
	volumesConfIni, err := GetSmartstoreVolumesConfig(ctx, client, cr, smartstore, mapSplunkConfDetails)
	if err != nil {
		return nil, configMapDataChanged, err
	}

	if volumesConfIni == "" {
		scopedLog.InfoContext(ctx, "volume stanza list is empty")
	}

	// Get the list of indexes in INI format
	indexesConfIni := resources.GetSmartstoreIndexesConfig(smartstore.IndexList)

	if indexesConfIni == "" {
		scopedLog.InfoContext(ctx, "index stanza list is empty")
	} else if volumesConfIni == "" {
		return nil, configMapDataChanged, fmt.Errorf("indexes without Volume configuration is not allowed")
	}

	defaultsConfIni := resources.GetSmartstoreIndexesDefaults(smartstore.Defaults)

	// 2. Prepare server.conf entries
	iniServerConf := resources.GetServerConfigEntries(&smartstore.DeepCopy().CacheManagerConf)

	// Create smartstore config consisting indexes.conf
	configMapName := splutil.GetSplunkSmartstoreConfigMapName(cr.GetName(), crKind)
	SplunkOperatorAppConfigMap := resources.PrepareSmartstoreConfigMap(configMapName, cr.GetNamespace(), defaultsConfIni, volumesConfIni, indexesConfIni, iniServerConf)

	SplunkOperatorAppConfigMap.SetOwnerReferences(append(SplunkOperatorAppConfigMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))

	// if existing configmap contains key conftoken then add that back
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := GetConfigMap(ctx, client, namespacedName)
	if err == nil && configMap != nil && configMap.Data != nil && reflect.ValueOf(configMap.Data).Kind() == reflect.Map {
		if _, ok := configMap.Data[configToken]; ok {
			SplunkOperatorAppConfigMap.Data[configToken] = configMap.Data[configToken]
		}
	}

	configMapDataChanged, err = ApplyConfigMap(ctx, client, SplunkOperatorAppConfigMap)
	if err != nil {
		scopedLog.ErrorContext(ctx, "config map create/update failed", "error", err)
		return nil, configMapDataChanged, err
	} else if configMapDataChanged {
		// Create a token to check if the config is really populated to the pod
		SplunkOperatorAppConfigMap.Data[configToken] = fmt.Sprintf(`%d`, time.Now().Unix())

		// this is tricky call, I have seen update fail here  with error": "Operation cannot be fulfilled on configmaps
		// the object has been modified; please apply your changes to the latest version and try again"
		// now the problem here is if configmap data has changed we need to update configtoken, only way we can do that
		// is try at least few times before failing, I took random number of 10 times to try
		// ideally retryCnt should come from global const
		// Apply the configMap with a fresh token
		retryCnt := 10
		for i := 0; i < retryCnt; i++ {
			configMapDataChanged, err = ApplyConfigMap(ctx, client, SplunkOperatorAppConfigMap)
			if (err != nil && !k8serrors.IsConflict(err)) || err == nil {
				break
			}
		}
		if err != nil {
			scopedLog.ErrorContext(ctx, "config map update failed", "error", err)
			return nil, configMapDataChanged, err
		}
	}

	return SplunkOperatorAppConfigMap, configMapDataChanged, nil
}

func AreRemoteVolumeKeysChanged(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType splcommon.InstanceType, smartstore *enterpriseApi.SmartStoreSpec, ResourceRev map[string]string, retError *error) bool {
	// No need to proceed if the smartstore is not configured
	if !resources.IsSmartstoreConfigured(smartstore) {
		return false
	}

	logger := logging.FromContext(ctx).With("func", "AreRemoteVolumeKeysChanged")

	volList := smartstore.VolList
	for _, volume := range volList {
		if volume.SecretRef != "" {
			namespaceScopedSecret, err := splutil.GetSecretByName(ctx, client, cr.GetNamespace(), volume.SecretRef)
			// Ideally, this should have been detected in Spec validation time
			if err != nil {
				*retError = fmt.Errorf("not able to access secret object = %s, reason: %s", volume.SecretRef, err)
				return false
			}

			// Check if the secret version is already tracked, and if there is a change in it
			if existingSecretVersion, ok := ResourceRev[volume.SecretRef]; ok {
				if existingSecretVersion != namespaceScopedSecret.ResourceVersion {
					logger.InfoContext(ctx, "secret keys changed", "previousResourceVersion", existingSecretVersion, "currentVersion", namespaceScopedSecret.ResourceVersion)
					ResourceRev[volume.SecretRef] = namespaceScopedSecret.ResourceVersion
					return true
				}
				return false
			}

			// First time adding to track the secret resource version
			ResourceRev[volume.SecretRef] = namespaceScopedSecret.ResourceVersion
		} else {
			logger.DebugContext(ctx, "no valid SecretRef for volume. No secret to track", "volumeName", volume.Name)
		}
	}

	return false
}

// GetSmartstoreVolumesConfig returns the list of Volumes configuration in INI format
func GetSmartstoreVolumesConfig(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, smartstore *enterpriseApi.SmartStoreSpec, mapData map[string]string) (string, error) {
	var volumesConf string

	logger := logging.FromContext(ctx).With("func", "GetSmartstoreVolumesConfig")

	volumes := smartstore.VolList
	for i := 0; i < len(volumes); i++ {
		if volumes[i].SecretRef != "" {
			s3AccessKey, s3SecretKey, _, err := GetSmartstoreRemoteVolumeSecrets(ctx, volumes[i], client, cr, smartstore)
			if err != nil {
				return "", fmt.Errorf("unable to read the secrets for volume = %s. %s", volumes[i].Name, err)
			}

			volumesConf = fmt.Sprintf(`%s
[volume:%s]
storageType = remote
path = s3://%s
remote.s3.access_key = %s
remote.s3.secret_key = %s
remote.s3.endpoint = %s
remote.s3.auth_region = %s
`, volumesConf, volumes[i].Name, volumes[i].Path, s3AccessKey, s3SecretKey, volumes[i].Endpoint, volumes[i].Region)
		} else {
			logger.InfoContext(ctx, "no valid secretRef configured.  Configure volume without access/secret keys", "volumeName", volumes[i].Name)
			volumesConf = fmt.Sprintf(`%s
[volume:%s]
storageType = remote
path = s3://%s
remote.s3.endpoint = %s
remote.s3.auth_region = %s
`, volumesConf, volumes[i].Name, volumes[i].Path, volumes[i].Endpoint, volumes[i].Region)
		}
	}

	return volumesConf, nil
}

// GetSmartstoreConfigMap returns the smartstore configMap, if it exists and applicable for that instanceType
func GetSmartstoreConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, instanceType InstanceType) *corev1.ConfigMap {
	var configMap *corev1.ConfigMap

	if instanceType == SplunkStandalone || isCMDeployed(instanceType) {
		smartStoreConfigMapName := splutil.GetSplunkSmartstoreConfigMapName(cr.GetName(), cr.GetObjectKind().GroupVersionKind().Kind)
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: smartStoreConfigMapName}
		configMap, _ = GetConfigMap(ctx, client, namespacedName)
	}

	return configMap
}
