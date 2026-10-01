// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package clustermaster

import (
	"context"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splclient "github.com/splunk/splunk-operator/pkg/splunk/client/splunk"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	// TODO: Remove this temporary dependency once all CRs have migrated from enterprise.
	reconcileutil "github.com/splunk/splunk-operator/pkg/splunk/reconcile"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/appframework"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/telapp"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const numberOfClusterMasterReplicas int32 = 1

// apply owns the request-level ClusterMaster reconciliation boundary.
func apply(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
	logger := logging.FromContext(ctx).With("controller", "ClusterMaster", "name", namespacedName.Name, "namespace", namespacedName.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	instance := &enterpriseApiV3.ClusterMaster{}
	err := client.Get(ctx, namespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not load cluster manager data: %w", err)
	}

	if instance.GetAnnotations()[enterpriseApiV3.ClusterMasterPausedAnnotation] == "true" {
		return reconcile.Result{Requeue: true, RequeueAfter: splcommon.PauseRetryDelay}, nil
	}

	logger.InfoContext(ctx, "start", "crVersion", instance.GetResourceVersion())
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)

	result, err := ApplyClusterMaster(ctx, client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}

	return result, err
}

// Apply is the request-level entry point used by the controller.
var Apply = apply

// ApplyClusterMaster reconciles the state of a Splunk Enterprise cluster manager.
var ApplyClusterMaster = applyClusterMaster

func applyClusterMaster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApiV3.ClusterMaster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplyClusterMaster")

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)
	cr.Kind = "ClusterMaster"

	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}

	var err error
	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateClusterMasterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateClusterMasterSpec", fmt.Sprintf("validate clustermaster spec failed %s", err.Error()))
		return result, fmt.Errorf("validate clustermaster spec: %w", err)
	}

	// updates status after function completes
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-%s", cr.GetName(), splcommon.ClusterManager)

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) ||
		k8sops.AreRemoteVolumeKeysChanged(ctx, client, cr, splcommon.SplunkClusterMaster, &cr.Spec.SmartStore, cr.Status.ResourceRevMap, &err) {

		if err != nil {
			eventPublisher.Warning(ctx, "AreRemoteVolumeKeysChanged", fmt.Sprintf("check remote volume key change failed %s", err.Error()))
			return result, err
		}

		_, configMapDataChanged, err := k8sops.ApplySmartstoreConfigMap(ctx, client, cr, &cr.Spec.SmartStore)
		if err != nil {
			return result, err
		} else if configMapDataChanged {
			// Do not auto populate with configMapDataChanged flag to NeedToPushMasterApps. Set it only  if
			// configMapDataChanged it true. It mush be reset, only upon initiating the bundle push REST call,
			// once the CM is in ready state otherwise, we keep retrying
			cr.Status.BundlePushTracker.NeedToPushMasterApps = true
			cr.Status.BundlePushTracker.LastCheckInterval = time.Now().Unix()
		}

		cr.Status.SmartStore = cr.Spec.SmartStore
	}

	// This is to take care of case where AreRemoteVolumeKeysChanged returns an error if it returns false.
	if err != nil {
		return result, err
	}

	// If needed, Migrate the app framework status
	err = appframework.CheckAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, false)
	if err != nil {
		return result, err
	}

	// If the app framework is configured then do following things -
	// 1. Initialize the S3Clients based on providers
	// 2. Check the status of apps on remote storage.
	if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
		err := appframework.InitAndCheckAppInfoStatus(ctx, client, cr, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext)
		if err != nil {
			eventPublisher.Warning(ctx, "initAndCheckAppInfoStatus", fmt.Sprintf("init and check app info status failed %s", err.Error()))
			cr.Status.AppContext.IsDeploymentInProgress = false
			return result, err
		}
	}

	// create or update general config resources
	namespaceScopedSecret, err := k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// Smart Store secrets get created manually and should not be managed by the Operator
	if &cr.Spec.SmartStore != nil {
		_ = k8sops.DeleteOwnerReferencesForS3SecretObjects(ctx, client, cr, &cr.Spec.SmartStore)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			extraEnv, _ := VerifyCMasterisMultisite(ctx, cr, namespaceScopedSecret)
			_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, extraEnv, false)
			if err != nil {
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = appframework.UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, appframework.SplunkClusterMaster)
			if err != nil {
				return result, err
			}
		}

		k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkClusterMaster)

		terminating, err := k8sops.CheckForDeletion(ctx, cr, client)
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

	// create or update a regular service for indexer cluster (ingestion)
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkIndexer, false))
	if err != nil {
		return result, err
	}

	// create or update a regular service for the cluster manager
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkClusterMaster, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset for the cluster manager
	statefulSet, err := getClusterMasterStatefulSet(ctx, client, cr)
	if err != nil {
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	extraEnv, err := VerifyCMasterisMultisite(ctx, cr, namespaceScopedSecret)
	err = k8sops.ValidateMonitoringConsoleRef(ctx, client, statefulSet, extraEnv)
	if err != nil {
		return result, err
	}

	clusterMasterManager := k8sops.DefaultStatefulSetPodManager{}
	phase, err := clusterMasterManager.Update(ctx, client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	//Update MC configmap
	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, extraEnv, true)
		if err != nil {
			return result, err
		}
	}

	// no need to requeue if everything is ready
	if cr.Status.Phase == enterpriseApi.PhaseReady {
		//upgrade fron automated MC to MC CRD
		namespacedName := types.NamespacedName{Namespace: cr.GetNamespace(), Name: splutil.GetSplunkStatefulsetName(splcommon.SplunkMonitoringConsole, cr.GetNamespace())}
		err = k8sops.DeleteReferencesToAutomatedMCIfExists(ctx, client, cr, namespacedName)
		if err != nil {
			logger.ErrorContext(ctx, "error in deleting automated MonitoringConsole resource", "error", err)
		}

		// Create podExecClient
		podExecClient := splutil.GetPodExecClient(client, cr, "")

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			err := telapp.AddTelApp(ctx, podExecClient, numberOfClusterMasterReplicas, cr)
			if err != nil {
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
		}

		// Manager apps bundle push requires multiple reconcile iterations in order to reflect the configMap on the CM pod.
		// So keep PerformCmBundlePush() as the last call in this block of code, so that other functionalities are not blocked
		err = performCmasterBundlePush(ctx, client, cr)
		if err != nil {
			return result, err
		}

		finalResult := appframework.HandleAppFrameworkActivity(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig)
		result = *finalResult
	}
	// RequeueAfter if greater than 0, tells the Controller to requeue the reconcile key after the Duration.
	// Implies that Requeue is true, there is no need to set Requeue to true at the same time as RequeueAfter.
	if !result.Requeue {
		result.RequeueAfter = 0
	}

	return result, nil
}

// clusterMasterPodMaster is used to manage the cluster manager pod
type clusterMasterPodManager struct {
	log             *slog.Logger
	cr              *enterpriseApiV3.ClusterMaster
	secrets         *corev1.Secret
	newSplunkClient func(managementURI, username, password string) *splclient.SplunkClient
}

// getClusterMasterClient for clusterMasterPodManager returns a SplunkClient for cluster manager
func (mgr *clusterMasterPodManager) getClusterMasterClient(cr *enterpriseApiV3.ClusterMaster) *splclient.SplunkClient {
	fqdnName := splcommon.GetServiceFQDN(cr.GetNamespace(), splcommon.GetSplunkServiceName(splcommon.SplunkClusterMaster, cr.GetName(), false))
	return mgr.newSplunkClient(fmt.Sprintf("https://%s:8089", fqdnName), "admin", string(mgr.secrets.Data["password"]))
}

// validateClusterMasterSpec checks validity and makes default updates to a ClusterMasterSpec, and returns error if something is wrong.
func validateClusterMasterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApiV3.ClusterMaster) error {

	if !reflect.DeepEqual(cr.Status.SmartStore, cr.Spec.SmartStore) {
		err := reconcileutil.ValidateSplunkSmartstoreSpec(ctx, &cr.Spec.SmartStore)
		if err != nil {
			return err
		}
	}

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := appframework.ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, false, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return reconcileutil.ValidateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// getClusterMasterStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise cluster master.
func getClusterMasterStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApiV3.ClusterMaster) (*appsv1.StatefulSet, error) {
	var extraEnvVar []corev1.EnvVar

	certMounts, err := certs.ReconcileCerts(ctx, client, cr, reconcileutil.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNames(splcommon.SplunkClusterMaster, cr.GetName(), cr.GetNamespace(), 1)))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	ss, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkClusterMaster, 1, extraEnvVar)
	if err != nil {
		return ss, err
	}
	certs.InjectCertMounts(&ss.Spec.Template, certMounts)
	smartStoreConfigMap := k8sops.GetSmartstoreConfigMap(ctx, client, cr, splcommon.SplunkClusterMaster)

	if smartStoreConfigMap != nil {
		resources.SetupInitContainer(&ss.Spec.Template, cr.Spec.Image, cr.Spec.ImagePullPolicy, splcommon.CommandForClusterManagerSmartstore, cr.Spec.CommonSplunkSpec.EtcVolumeStorageConfig.EphemeralStorage)
	}
	// Setup App framework staging volume for apps
	resources.SetupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// VerifyCMasterisMultisite checks if its a multisite
// Defined as a variable to allow mocking in unit tests
var VerifyCMasterisMultisite = func(ctx context.Context, cr *enterpriseApiV3.ClusterMaster, namespaceScopedSecret *corev1.Secret) ([]corev1.EnvVar, error) {
	var err error
	logger := logging.FromContext(ctx).With("func", "VerifyCMasterisMultisite", "name", cr.GetName(), "namespace", cr.GetNamespace())
	mgr := clusterMasterPodManager{log: logger, cr: cr, secrets: namespaceScopedSecret, newSplunkClient: splclient.NewSplunkClient}
	cm := mgr.getClusterMasterClient(cr)
	clusterInfo, err := cm.GetClusterInfo(false)
	if err != nil {
		return nil, err
	}
	multiSite := clusterInfo.MultiSite
	extraEnv := resources.GetClusterMasterExtraEnv(cr)
	if multiSite == "true" {
		extraEnv = append(extraEnv, corev1.EnvVar{Name: "SPLUNK_SITE", Value: "site0"}, corev1.EnvVar{Name: "SPLUNK_MULTISITE_MASTER", Value: splcommon.GetSplunkServiceName(splcommon.SplunkClusterMaster, cr.GetName(), false)})
	}
	return extraEnv, err
}
