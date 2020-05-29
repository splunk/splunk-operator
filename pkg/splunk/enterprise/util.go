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
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	//stdlog "log"
	//"github.com/go-logr/stdr"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
)

// kubernetes logger used by splunk.enterprise package
var log = logf.Log.WithName("splunk.enterprise")

// ApplySplunkConfig reconciles the state of Kubernetes Secrets, ConfigMaps and other general settings for Splunk Enterprise instances.
func ApplySplunkConfig(client splcommon.ControllerClient, cr splcommon.MetaObject, spec enterprisev1.CommonSplunkSpec, instanceType InstanceType) (*corev1.Secret, error) {
	var err error

	// if reference to indexer cluster, extract and re-use idxc.secret
	// IndexerRef is not relevant for Indexer, and Indexer will use value from LicenseMaster to prevent cyclical dependency
	var idxcSecret []byte
	if instanceType.ToKind() != "indexer" && instanceType.ToKind() != "license-master" && spec.IndexerClusterRef.Name != "" {
		idxcSecret, err = GetSplunkSecret(client, cr, spec.IndexerClusterRef, SplunkIndexer, "idxc_secret")
		if err != nil {
			return nil, err
		}
	}

	// if reference to license master, extract and re-use pass4SymmKey
	var pass4SymmKey []byte
	if instanceType.ToKind() != "license-master" && spec.LicenseMasterRef.Name != "" {
		pass4SymmKey, err = GetSplunkSecret(client, cr, spec.LicenseMasterRef, SplunkLicenseMaster, "pass4SymmKey")
		if err != nil {
			return nil, err
		}
		if instanceType.ToKind() == "indexer" {
			// get pass4SymmKey from LicenseMaster to avoid cyclical dependency
			idxcSecret, err = GetSplunkSecret(client, cr, spec.LicenseMasterRef, SplunkLicenseMaster, "idxc_secret")
			if err != nil {
				return nil, err
			}
		}
	}

	// create or retrieve splunk secrets
	secrets := getSplunkSecrets(cr, instanceType, idxcSecret, pass4SymmKey)
	secrets.SetOwnerReferences(append(secrets.GetOwnerReferences(), splcommon.AsOwner(cr)))
	if secrets, err = splctrl.ApplySecret(client, secrets); err != nil {
		return nil, err
	}

	// create splunk defaults (for inline config)
	if spec.Defaults != "" {
		defaultsMap := getSplunkDefaults(cr.GetName(), cr.GetNamespace(), instanceType, spec.Defaults)
		defaultsMap.SetOwnerReferences(append(defaultsMap.GetOwnerReferences(), splcommon.AsOwner(cr)))
		if err = splctrl.ApplyConfigMap(client, defaultsMap); err != nil {
			return nil, err
		}
	}

	return secrets, nil
}

// GetSplunkSecret is used to retrieve a secret from another custom resource.
func GetSplunkSecret(client splcommon.ControllerClient, cr splcommon.MetaObject, ref corev1.ObjectReference, instanceType InstanceType, secretName string) ([]byte, error) {
	namespace := ref.Namespace
	if namespace == "" {
		namespace = cr.GetNamespace()
	}
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      GetSplunkSecretsName(ref.Name, instanceType),
	}

	scopedLog := log.WithName("GetSplunkSecret").WithValues("kind", cr.GetObjectKind().GroupVersionKind().Kind,
		"name", namespacedName.Name, "namespace", namespacedName.Namespace, "secretName", secretName)

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
