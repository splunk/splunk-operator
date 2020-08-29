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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	"github.com/splunk/splunk-operator/pkg/splunk/spark"
)

// ApplyStandalone reconciles the StatefulSet for N standalone instances of Splunk Enterprise.
func ApplyStandalone(client splcommon.ControllerClient, cr *enterprisev1.Standalone) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}

	// validate and updates defaults for CR
	err := validateStandaloneSpec(&cr.Spec)
	if err != nil {
		// To do: sgontla: later delete these listings. (for now just to test CSPL-320)
		LogSmartStoreVolumes(cr.Status.SmartStore.VolList)
		LogSmartStoreIndexes(cr.Status.SmartStore.IndexList)
		return result, err
	}

	// updates status after function completes
	cr.Status.Phase = splcommon.PhaseError
	cr.Status.Replicas = cr.Spec.Replicas
	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		_, err := CreateSmartStoreConfigMap(client, cr, &cr.Spec.SmartStore)
		if err != nil {
			return result, err
		}

		cr.Status.SmartStore = cr.Spec.SmartStore
	}

	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-standalone", cr.GetName())
	defer func() {
		client.Status().Update(context.TODO(), cr)
	}()

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		terminating, err := splctrl.CheckForDeletion(cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = splcommon.PhaseTerminating
		} else {
			result.Requeue = false
		}
		return result, err
	}

	// create or update general config resources
	_, err = ApplySplunkConfig(client, cr, cr.Spec.CommonSplunkSpec, SplunkStandalone)
	if err != nil {
		return result, err
	}

	// create or update a headless service (this is required by DFS for Spark->standalone comms, possibly other things)
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, true))
	if err != nil {
		return result, err
	}

	// create or update a regular service
	err = splctrl.ApplyService(client, getSplunkService(cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getStandaloneStatefulSet(cr)
	if err != nil {
		return result, err
	}
	mgr := splctrl.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(client, statefulSet, cr.Spec.Replicas)
	cr.Status.ReadyReplicas = statefulSet.Status.ReadyReplicas
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	// no need to requeue if everything is ready
	if cr.Status.Phase == splcommon.PhaseReady {
		result.Requeue = false
	}
	return result, nil
}

// getStandaloneStatefulSet returns a Kubernetes StatefulSet object for Splunk Enterprise standalone instances.
func getStandaloneStatefulSet(cr *enterprisev1.Standalone) (*appsv1.StatefulSet, error) {

	// get generic statefulset for Splunk Enterprise objects
	ss, err := getSplunkStatefulSet(cr, &cr.Spec.CommonSplunkSpec, SplunkStandalone, cr.Spec.Replicas, []corev1.EnvVar{})
	if err != nil {
		return nil, err
	}

	// add spark and java mounts to search head containers
	if cr.Spec.SparkRef.Name != "" {
		addDFCToPodTemplate(&ss.Spec.Template, cr.Spec.SparkRef, cr.Spec.SparkImage, cr.Spec.ImagePullPolicy, cr.Spec.Replicas > 1)
	}

	return ss, nil
}

// validateStandaloneSpec checks validity and makes default updates to a StandaloneSpec, and returns error if something is wrong.
func validateStandaloneSpec(spec *enterprisev1.StandaloneSpec) error {
	if spec.Replicas == 0 {
		spec.Replicas = 1
	}

	err := ValidateSplunkSmartstoreSpec(&spec.SmartStore)
	if err != nil {
		return err
	}

	spec.SparkImage = spark.GetSparkImage(spec.SparkImage)
	return validateCommonSplunkSpec(&spec.CommonSplunkSpec)
}
