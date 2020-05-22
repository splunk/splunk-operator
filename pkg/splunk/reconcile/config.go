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

package reconcile

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// ApplySplunkConfig reconciles the state of Kubernetes Secrets, ConfigMaps and other general settings for Splunk Enterprise instances.
func ApplySplunkConfig(client ControllerClient, cr enterprisev1.MetaObject, spec enterprisev1.CommonSplunkSpec, instanceType enterprise.InstanceType) (*corev1.Secret, error) {
	var err error

	// if reference to indexer cluster, extract and re-use idxc.secret
	// IndexerRef is not relevant for Indexer, and Indexer will use value from LicenseMaster to prevent cyclical dependency
	var idxcSecret []byte
	if instanceType.ToKind() != "indexer" && instanceType.ToKind() != "license-master" && spec.IndexerClusterRef.Name != "" {
		idxcSecret, err = GetSplunkSecret(client, cr, spec.IndexerClusterRef, enterprise.SplunkIndexer, "idxc_secret")
		if err != nil {
			return nil, err
		}
	}

	// if reference to license master, extract and re-use pass4SymmKey
	var pass4SymmKey []byte
	if instanceType.ToKind() != "license-master" && spec.LicenseMasterRef.Name != "" {
		pass4SymmKey, err = GetSplunkSecret(client, cr, spec.LicenseMasterRef, enterprise.SplunkLicenseMaster, "pass4SymmKey")
		if err != nil {
			return nil, err
		}
		if instanceType.ToKind() == "indexer" {
			// get pass4SymmKey from LicenseMaster to avoid cyclical dependency
			idxcSecret, err = GetSplunkSecret(client, cr, spec.LicenseMasterRef, enterprise.SplunkLicenseMaster, "idxc_secret")
			if err != nil {
				return nil, err
			}
		}
	}

	// create or retrieve splunk secrets
	secrets := enterprise.GetSplunkSecrets(cr, instanceType, idxcSecret, pass4SymmKey)
	secrets.SetOwnerReferences(append(secrets.GetOwnerReferences(), resources.AsOwner(cr)))
	if secrets, err = ApplySecret(client, secrets); err != nil {
		return nil, err
	}

	// create splunk defaults (for inline config)
	if spec.Defaults != "" {
		defaultsMap := enterprise.GetSplunkDefaults(cr.GetIdentifier(), cr.GetNamespace(), instanceType, spec.Defaults)
		defaultsMap.SetOwnerReferences(append(defaultsMap.GetOwnerReferences(), resources.AsOwner(cr)))
		if err = ApplyConfigMap(client, defaultsMap); err != nil {
			return nil, err
		}
	}

	return secrets, nil
}

// ApplyConfigMap creates or updates a Kubernetes ConfigMap
func ApplyConfigMap(client ControllerClient, configMap *corev1.ConfigMap) error {
	scopedLog := log.WithName("ApplyConfigMap").WithValues(
		"name", configMap.GetObjectMeta().GetName(),
		"namespace", configMap.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: configMap.GetNamespace(), Name: configMap.GetName()}
	var current corev1.ConfigMap

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		if !reflect.DeepEqual(configMap.Data, current.Data) {
			scopedLog.Info("Updating existing ConfigMap")
			current.Data = configMap.Data
			err = UpdateResource(client, &current)
		} else {
			scopedLog.Info("No changes for ConfigMap")
		}
	} else {
		err = CreateResource(client, configMap)
	}

	return err
}

// ApplySecret creates or updates a Kubernetes Secret, and returns active secrets if successful
func ApplySecret(client ControllerClient, secret *corev1.Secret) (*corev1.Secret, error) {
	scopedLog := log.WithName("ApplySecret").WithValues(
		"name", secret.GetObjectMeta().GetName(),
		"namespace", secret.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: secret.GetNamespace(), Name: secret.GetName()}
	var current corev1.Secret
	result := &current

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing Secret: do nothing
		scopedLog.Info("Found existing Secret")
	} else {
		err = CreateResource(client, secret)
		result = secret
	}

	return result, err
}

// GetSplunkSecret is used to retrieve a secret from another custom resource.
func GetSplunkSecret(client ControllerClient, cr enterprisev1.MetaObject, ref corev1.ObjectReference, instanceType enterprise.InstanceType, secretName string) ([]byte, error) {
	namespace := ref.Namespace
	if namespace == "" {
		namespace = cr.GetNamespace()
	}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      enterprise.GetSplunkSecretsName(ref.Name, instanceType),
	}

	scopedLog := log.WithName("GetSplunkSecret").WithValues(
		"kind", cr.GetTypeMeta().Kind, "name", namespacedName.Name, "namespace", namespacedName.Namespace, "secretName", secretName)

	var secret corev1.Secret
	err := client.Get(context.TODO(), namespacedName, &secret)
	if err != nil {
		return nil, fmt.Errorf("Unable to get secret: %v", err)
	}

	result := secret.Data[secretName]
	if len(result) == 0 {
		return nil, fmt.Errorf("Secret is empty")
	}

	scopedLog.Info("Re-using secret")
	return result, nil
}
