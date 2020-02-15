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

package deploy

import (
	"context"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha2"
	"github.com/splunk/splunk-operator/pkg/splunk/enterprise"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
)

// ReconcileSplunkConfig reconciles the state of Kubernetes Secrets, ConfigMaps and other general settings for Splunk Enterprise instances.
func ReconcileSplunkConfig(client ControllerClient, cr enterprisev1.MetaObject, spec enterprisev1.CommonSplunkSpec, instanceType enterprise.InstanceType) error {
	scopedLog := log.WithName("ReconcileSplunkConfig").WithValues("name", cr.GetIdentifier(), "namespace", cr.GetNamespace())

	// if reference to indexer cluster, extract and re-use idxc.secret
	var idxcSecret []byte
	indexerNamespace := spec.IndexerRef.Namespace
	if indexerNamespace == "" {
		indexerNamespace = cr.GetNamespace()
	}
	if spec.IndexerRef.Name != "" {
		namespacedName := types.NamespacedName{
			Namespace: indexerNamespace,
			Name:      enterprise.GetSplunkSecretsName(spec.IndexerRef.Name, enterprise.SplunkIndexer),
		}
		var indexerSecret corev1.Secret
		err := client.Get(context.TODO(), namespacedName, &indexerSecret)
		if err == nil {
			scopedLog.Info("Re-using idxc_secret from referenced Indexer")
			idxcSecret = indexerSecret.Data["idxc_secret"]
		}
	}

	// create splunk secrets
	secrets := enterprise.GetSplunkSecrets(cr, instanceType, idxcSecret)
	secrets.SetOwnerReferences(append(secrets.GetOwnerReferences(), resources.AsOwner(cr)))
	if err := ApplySecret(client, secrets); err != nil {
		return err
	}

	// create splunk defaults (for inline config)
	if spec.Defaults != "" {
		defaultsMap := enterprise.GetSplunkDefaults(cr.GetIdentifier(), cr.GetNamespace(), instanceType, spec.Defaults)
		defaultsMap.SetOwnerReferences(append(defaultsMap.GetOwnerReferences(), resources.AsOwner(cr)))
		if err := ApplyConfigMap(client, defaultsMap); err != nil {
			return err
		}
	}

	return nil
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

// ApplySecret creates or updates a Kubernetes Secret
func ApplySecret(client ControllerClient, secret *corev1.Secret) error {
	scopedLog := log.WithName("ApplySecret").WithValues(
		"name", secret.GetObjectMeta().GetName(),
		"namespace", secret.GetObjectMeta().GetNamespace())

	namespacedName := types.NamespacedName{Namespace: secret.GetNamespace(), Name: secret.GetName()}
	var current corev1.Secret

	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// found existing Secret: do nothing
		scopedLog.Info("Found existing Secret")
	} else {
		err = CreateResource(client, secret)
	}

	return err
}
