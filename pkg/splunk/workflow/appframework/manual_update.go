// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package appframework

import (
	"context"
	"fmt"
	"reflect"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// ApplyManualAppUpdateConfigMap applies the namespace-scoped manual app update ConfigMap.
func ApplyManualAppUpdateConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, crKindMap map[string]string) (*corev1.ConfigMap, error) {
	logger := logging.FromContext(ctx).With("func", "ApplyManualAppUpdateConfigMap")

	configMapName := splutil.GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	var configMap *corev1.ConfigMap
	var err error
	var newConfigMap bool
	configMap, err = k8sops.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		configMap = k8sops.PrepareConfigMap(configMapName, cr.GetNamespace(), crKindMap)
		newConfigMap = true
	}

	configMap.Data = crKindMap
	configMap.SetOwnerReferences(append(configMap.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	if newConfigMap {
		logger.InfoContext(ctx, "creating manual app update configMap")
		err = splutil.CreateResource(ctx, client, configMap)
	} else {
		logger.InfoContext(ctx, "updating manual app update configMap")
		err = splutil.UpdateResource(ctx, client, configMap)
	}
	if err != nil {
		logger.ErrorContext(ctx, "unable to apply the configMap", "name", configMapName, "error", err)
		return configMap, err
	}

	return configMap, nil
}

func getManualUpdateStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) string {
	logger := logging.FromContext(ctx).With("func", "getManualUpdateStatus")

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := k8sops.GetConfigMap(ctx, client, namespacedName)
	if err == nil {
		statusRegex := ".*status: (?P<status>.*).*"
		data := configMap.Data[cr.GetObjectKind().GroupVersionKind().Kind]
		result := extractFieldFromConfigMapData(statusRegex, data)
		if result == "on" {
			logger.InfoContext(ctx, "namespace configMap value is set to", "name", configMapName, "data", result)
			return result
		}
	} else {
		logger.ErrorContext(ctx, "unable to get namespace specific configMap", "name", configMapName, "error", err)
	}

	return "off"
}

func getManualUpdatePerCrStatus(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) string {
	logger := logging.FromContext(ctx).With("func", "getManualUpdatePerCrStatus")

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkPerCRConfigMapName(splcommon.KindToInstanceString(cr.GroupVersionKind().Kind), cr.GetName())}
	crConfigMap, err := k8sops.GetConfigMap(ctx, client, namespacedName)
	if err == nil {
		logger.InfoContext(ctx, "custom configMap value is set to", "name", configMapName, "data", crConfigMap.Data)
		return crConfigMap.Data["manualUpdate"]
	}

	logger.ErrorContext(ctx, "unable to get custom specific configMap", "name", configMapName, "error", err)
	return "off"
}

func getManualUpdateRefCount(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject, configMapName string) int {
	logger := logging.FromContext(ctx).With("func", "getManualUpdateRefCount")

	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}
	configMap, err := k8sops.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		logger.ErrorContext(ctx, "unable to get the configMap", "name", configMapName, "error", err)
		return 0
	}

	refCountRegex := ".*refCount: (?P<refCount>.*).*"
	data := configMap.Data[cr.GetObjectKind().GroupVersionKind().Kind]

	refCount, _ := strconv.Atoi(extractFieldFromConfigMapData(refCountRegex, data))
	return refCount
}

func createOrUpdateAppUpdateConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject) (*corev1.ConfigMap, error) {
	logger := logging.FromContext(ctx).With("func", "createOrUpdateAppUpdateConfigMap", "name", cr.GetName(), "namespace", cr.GetNamespace())

	var crKindMap map[string]string
	var configMapData, status string
	var configMap *corev1.ConfigMap
	var err error
	var numOfObjects int

	kind := cr.GetObjectKind().GroupVersionKind().Kind
	configMapName := splutil.GetSplunkManualAppUpdateConfigMapName(cr.GetNamespace())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	mux := getResourceMutex(configMapName)
	mux.Lock()
	defer mux.Unlock()
	configMap, err = k8sops.GetConfigMap(ctx, client, namespacedName)
	if err == nil {
		currentOwnerRef := configMap.GetOwnerReferences()
		for i := 0; i < len(currentOwnerRef); i++ {
			if reflect.DeepEqual(currentOwnerRef[i], splcommon.AsOwner(cr, false)) {
				return configMap, nil
			}
		}

		logger.InfoContext(ctx, "existing configMap data", "data", configMap.Data)
		crKindMap = configMap.Data
		numOfObjects = getNumOfOwnerRefsKind(configMap, kind)
	} else if !k8serrors.IsNotFound(err) {
		logger.ErrorContext(ctx, "unable to get manual app update configMap", "name", configMapName, "error", err)
		return configMap, err
	}

	if crKindMap == nil {
		crKindMap = make(map[string]string)
	}
	if _, ok := crKindMap[kind]; !ok {
		status = "off"
	} else {
		status = getManualUpdateStatus(ctx, client, cr, configMapName)
	}

	configMapData = fmt.Sprintf(`status: %s
refCount: %d`, status, numOfObjects+1)
	crKindMap[kind] = configMapData

	configMap, err = ApplyManualAppUpdateConfigMap(ctx, client, cr, crKindMap)
	if err != nil {
		logger.ErrorContext(ctx, "create/update configMap for app update failed", "error", err)
		return configMap, err
	}

	return configMap, nil
}

// ReconcileCRSpecificConfigMap reconciles the per-CR manual-update ConfigMap.
func ReconcileCRSpecificConfigMap(ctx context.Context, client splcommon.ControllerClient, cr splcommon.MetaObject) error {
	logger := logging.FromContext(ctx).With("func", "ReconcileCRSpecificConfigMap", "name", cr.GetName(), "namespace", cr.GetNamespace())

	instanceName := splcommon.KindToInstanceString(cr.GetObjectKind().GroupVersionKind().Kind)
	configMapName := splutil.GetSplunkPerCRConfigMapName(instanceName, cr.GetName())
	namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: configMapName}

	configMap, err := k8sops.GetConfigMap(ctx, client, namespacedName)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			configMap = &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMapName,
					Namespace: cr.GetNamespace(),
				},
				Data: map[string]string{
					"manualUpdate": "off",
				},
			}
			configMap.SetOwnerReferences(append(configMap.GetOwnerReferences(), splcommon.AsOwner(cr, true)))
			if err = client.Create(ctx, configMap); err != nil {
				logger.ErrorContext(ctx, "failed to create config map", "error", err)
				return err
			}
			logger.InfoContext(ctx, "created new config map with manualUpdate set to off")
			return nil
		}
		logger.ErrorContext(ctx, "failed to get config map", "error", err)
		return err
	}

	if _, exists := configMap.Data["manualUpdate"]; !exists {
		configMap.Data["manualUpdate"] = "off"
		if err = client.Update(ctx, configMap); err != nil {
			logger.ErrorContext(ctx, "failed to update config map with manualUpdate field", "error", err)
			return err
		}
		logger.InfoContext(ctx, "updated config map with manualUpdate set to off")
	}

	return nil
}
