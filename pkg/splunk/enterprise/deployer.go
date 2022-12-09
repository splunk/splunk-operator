// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ApplyDeployer reconciles the state for a Splunk Enterprise deployer
func ApplyDeployer(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Deployer) (reconcile.Result, error) {
	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("ApplyDeployer")
	eventPublisher, _ := newK8EventPublisher(client, cr)

	// validate and updates defaults for CR
	err := validateDeployerSpec(ctx, client, cr)
	if err != nil {
		return result, err
	}
	// If needed, Migrate the app framework status
	err = checkAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, false)
	if err != nil {
		return result, err
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := initAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "initAndCheckAppInfoStatus", fmt.Sprintf("init and check app info status failed %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			return result, err
		}
	}

	// updates status after function completes
	cr.Status.Phase = enterpriseApi.PhaseError
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-deployer", cr.GetName())

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr)

	// create or update general config resources
	namespaceScopedSecret, err := ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, SplunkDeployer)
	if err != nil {
		scopedLog.Error(err, "create or update general config failed", "error", err.Error())
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, err
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		/* TODO: Arjun
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getSearchHeadEnv(cr), false)
			if err != nil {
				return result, err
			}
		}
		*/

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, SplunkDeployer)
			if err != nil {
				return result, err
			}
		}

		DeleteOwnerReferencesForResources(ctx, client, cr, nil, SplunkDeployer)
		terminating, err := splctrl.CheckForDeletion(ctx, cr, client)
		if terminating && err != nil { // don't bother if no error, since it will just be removed immmediately after
			cr.Status.Phase = enterpriseApi.PhaseTerminating
		} else {
			result.Requeue = false
		}
		if err != nil {
			eventPublisher.Warning(ctx, "Delete", fmt.Sprintf("delete custom resource failed %s", err.Error()))
		}
		return result, err
	}

	// create or update a deployer service
	err = splctrl.ApplyService(ctx, client, getSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the deployer
	statefulSet, err := getDeployerStatefulSet(ctx, client, cr)
	if err != nil {
		return result, err
	}

	deployerManager := splctrl.DefaultStatefulSetPodManager{}
	phase, err := deployerManager.Update(ctx, client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	/* TODO: Arjun MC
	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = validateMonitoringConsoleRef(ctx, client, statefulSet, getSearchHeadEnv(cr))
	if err != nil {
		return result, err
	}
	*/

	var finalResult *reconcile.Result
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		finalResult = handleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)

		/* TODO Arjun MC related
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: GetSplunkStatefulsetName(SplunkMonitoringConsole, cr.GetNamespace())}
		err = splctrl.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			scopedLog.Error(err, "Error in deleting automated monitoring console resource")
		}
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getDeployerEnv(cr), true)
			if err != nil {
				return result, err
			}
		}
		*/

		// Reset secrets related status structs
		cr.Status.NamespaceSecretResourceVersion = namespaceScopedSecret.ObjectMeta.ResourceVersion

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := addTelApp(ctx, client, podExecClient, numberOfDeployerReplicas, cr)
			if err != nil {
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}
		// Update the requeue result as needed by the app framework
		if finalResult != nil {
			result = *finalResult
		}
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// SHC connected to the deployer adds its owner reference on the deployer CR
// getShcConnDeployer extracts the SHC owner reference, retrieve and return the SHC CR
func getShcConnDeployer(ctx context.Context, c splcommon.ControllerClient, deployerCr splcommon.MetaObject) (*enterpriseApi.SearchHeadCluster, error) {
	reqLogger := log.FromContext(ctx)
	scopedLog := reqLogger.WithName("getShcConnDeployer").WithValues("deployer CR", deployerCr.GetName(), "deployer CR namespace", deployerCr.GetNamespace())

	var shcCr enterpriseApi.SearchHeadCluster

	scopedLog.Info("Arjun in getShcConnDeployer")

	deployerStsName := GetSplunkStatefulsetName(SplunkDeployer, deployerCr.GetName())
	namespacedName := types.NamespacedName{Namespace: deployerCr.GetNamespace(), Name: deployerStsName}
	deployerSts, err := splctrl.GetStatefulSetByName(ctx, c, namespacedName)
	if err != nil {
		scopedLog.Error(err, "Unable to get the stateful set")
		return nil, err
	}

	// Find the ownerRef added by SHC
	depStsOwnRef := deployerSts.GetOwnerReferences()
	for _, ow := range depStsOwnRef {
		scopedLog.Info("Arjun in getShcConnDeployer found ownerReferences", "deployer Owner References", depStsOwnRef)

		if ow.Kind == "SearchHeadCluster" {
			// Found SHC with ownerRef to the deployer
			scopedLog.Info("Arjun in getShcConnDeployer found SHC ownerRef", "SHC Owner Ref for deployer", ow)

			namespacedName := types.NamespacedName{Namespace: deployerCr.GetNamespace(), Name: ow.Name}
			err := c.Get(ctx, namespacedName, &shcCr)
			if err != nil {
				scopedLog.Info("Arjun in getShcConnDeployer couldn't find shc", "shc", shcCr.GetName())
				return nil, err
			}
			scopedLog.Info("Arjun Found a SHC connected to deployer", "shc CR", shcCr.GetName(), "shc CR namespace", shcCr.GetNamespace())
			return &shcCr, nil
		}
	}

	return nil, fmt.Errorf("couldn't find the SHC connected to the deployer via ownerReferences")
}

// getDeployerStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license manager.
func getDeployerStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApi.Deployer) (*appsv1.StatefulSet, error) {
	ss, err := getSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, SplunkDeployer, 1, make([]corev1.EnvVar, 0))
	if err != nil {
		return ss, err
	}

	// Setup App framework staging volume for apps
	setupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateDeployerSpec checks validity and makes default updates to a Deployer, and returns error if something is wrong.
func validateDeployerSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.Deployer) error {
	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false)
		if err != nil {
			return err
		}
	}

	return validateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}
