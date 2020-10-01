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
	"reflect"
	"time"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	appsv1 "k8s.io/api/apps/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	corev1 "k8s.io/api/core/v1"
)

// ApplyClusterMaster reconciles the state of a Splunk Enterprise cluster master.
func ApplyClusterMaster(client splcommon.ControllerClient, cr *enterprisev1.ClusterMaster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	scopedLog := log.WithName("ApplyClusterMaster").WithValues("name", cr.GetName(), "namespace", cr.GetNamespace())

	// validate and updates defaults for CR
	err := validateClusterMasterSpec(cr)
	if err != nil {
		// To do: sgontla: later delete these listings. (for now just to test CSPL-320)
		LogSmartStoreVolumes(cr.Status.SmartStore.VolList)
		LogSmartStoreIndexes(cr.Status.SmartStore.IndexList)
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-cluster-master", cr.GetName())
	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		_, err := CreateSmartStoreConfigMap(client, cr, &cr.Spec.SmartStore)
		if err != nil {
			return result, err
		}

		// To do: sgontla: Do we need to update the status in K8 etcd immediately?
		// Consider the case, where the Cluster master validates the config, and
		// generates config map, but fails later in the flow to complete its
		// stateful set. Meanwhile, all the indexer cluster sites notices new
		// config map, and keeps resetting the indexers. This is not problem,
		// as long as:
		// 1. ApplyConfigMap avoids a CRUD update if there is no change to data,
		// which is already happening today, so, this scenario shouldn't happen.
		// 2. To Do: Is there anything additional to be done on indexer site?
		cr.Status.SmartStore = cr.Spec.SmartStore
	}

	defer func() {
		err = client.Status().Update(context.TODO(), cr)
		if err != nil {
			scopedLog.Error(err, "Status update failed")
		}
	}()

	// create or update general config resources
	_, err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkIndexer)
	if err != nil {
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		err = ApplyMonitoringConsole(client, cr, cr.Spec.CommonSplunkSpec, getClusterMasterExtraEnv(cr, &cr.Spec.CommonSplunkSpec))
		if err != nil {
			return result, err
		}
		terminating, err := splctrl.CheckForDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update a regular service for indexer cluster (ingestion)
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkIndexer, false))
	if err != nil {
		return result, err
	}

	// create or update a regular service for the cluster master
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkClusterMaster, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the cluster master
	statefulSet, err := getClusterMasterStatefulSet(client, cr)
	if err != nil {
		return result, err
	}
	clusterMasterManager := splctrl.DefaultStatefulSetPodManager{}
	phase, err := clusterMasterManager.Update(client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		err = ApplyMonitoringConsole(client, cr, cr.Spec.CommonSplunkSpec, getClusterMasterExtraEnv(cr, &cr.Spec.CommonSplunkSpec))
		if err != nil {
			return result, err
		}
		result.Requeue = false
	}
	return result, nil
}

// validateClusterMasterSpec checks validity and makes default updates to a ClusterMasterSpec, and returns error if something is wrong.
func validateClusterMasterSpec(cr *enterprisev1.ClusterMaster) error {
	err := ValidateSplunkSmartstoreSpec(&cr.Spec.SmartStore)
	if err != nil {
		return err
	}

	return validateCommonSplunkSpec(&cr.Spec.CommonSplunkSpec)
}

// getClusterMasterStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license master.
func getClusterMasterStatefulSet(client splcommon.ControllerClient, cr *enterprisev1.ClusterMaster) (*appsv1.StatefulSet, error) {
	var extraEnvVar []corev1.EnvVar
	return getSplunkStatefulSet(client, cr, &cr.Spec.CommonSplunkSpec, SplunkClusterMaster, 1, extraEnvVar)
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
