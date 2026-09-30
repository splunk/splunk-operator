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

package licensemaster

import (
	"context"
	"fmt"
	"reflect"
	"time"

	enterpriseApiV3 "github.com/splunk/splunk-operator/api/enterprise/v3"
	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
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

// apply owns the request-level LicenseMaster reconciliation boundary.
func apply(ctx context.Context, client splcommon.ControllerClient, namespacedName types.NamespacedName, recorder record.EventRecorder) (reconcile.Result, error) {
	logger := logging.FromContext(ctx).With("controller", "LicenseMaster", "name", namespacedName.Name, "namespace", namespacedName.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)

	instance := &enterpriseApiV3.LicenseMaster{}
	err := client.Get(ctx, namespacedName, instance)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("could not load license master data: %w", err)
	}

	if instance.GetAnnotations()[enterpriseApiV3.LicenseMasterPausedAnnotation] == "true" {
		return reconcile.Result{Requeue: true, RequeueAfter: splcommon.PauseRetryDelay}, nil
	}

	logger.InfoContext(ctx, "start", "crVersion", instance.GetResourceVersion())
	ctx = context.WithValue(ctx, splcommon.EventRecorderKey, recorder)

	result, err := ApplyLicenseMaster(ctx, client, instance)
	if result.Requeue && result.RequeueAfter != 0 {
		logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
	}

	return result, err
}

// Apply is the request-level entry point used by the controller.
var Apply = apply

// ApplyLicenseMaster reconciles the state for the Splunk Enterprise license master.
var ApplyLicenseMaster = applyLicenseMaster

func applyLicenseMaster(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApiV3.LicenseMaster) (reconcile.Result, error) {

	// unless modified, reconcile for this object will be requeued after 5 seconds
	result := reconcile.Result{
		Requeue:      true,
		RequeueAfter: time.Second * 5,
	}
	logger := logging.FromContext(ctx).With("func", "ApplyLicenseMaster")

	eventPublisher := k8sops.GetEventPublisher(ctx, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, eventPublisher)

	var err error
	// Initialize phase
	cr.Status.Phase = enterpriseApi.PhaseError

	// Update the CR Status
	defer updateCRStatus(ctx, client, cr, &err)

	// validate and updates defaults for CR
	err = validateLicenseMasterSpec(ctx, client, cr)
	if err != nil {
		eventPublisher.Warning(ctx, "validateLicenseMasterSpec", fmt.Sprintf("validate licensemaster spec failed %s", err.Error()))
		return result, fmt.Errorf("validate license master spec: %w", err)
	}

	// If needed, Migrate the app framework status
	err = appframework.CheckAndMigrateAppDeployStatus(ctx, client, cr, &cr.Status.AppContext, &cr.Spec.AppFrameworkConfig, true)
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
	_, err = k8sops.ApplySplunkConfig(ctx, client, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkLicenseMaster)
	if err != nil {
		eventPublisher.Warning(ctx, "ApplySplunkConfig", fmt.Sprintf("create or update general config failed with error %s", err.Error()))
		return result, fmt.Errorf("apply splunk config: %w", err)
	}

	// check if deletion has been requested
	if cr.ObjectMeta.DeletionTimestamp != nil {
		if cr.Spec.MonitoringConsoleRef.Name != "" {
			_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getLicenseMasterURL(cr, &cr.Spec.CommonSplunkSpec), false)
			if err != nil {
				return result, err
			}
		}

		// If this is the last of its kind getting deleted,
		// remove the entry for this CR type from configMap or else
		// just decrement the refCount for this CR type.
		if len(cr.Spec.AppFrameworkConfig.AppSources) != 0 {
			err = appframework.UpdateOrRemoveEntryFromConfigMapLocked(ctx, client, cr, appframework.SplunkLicenseManager)
			if err != nil {
				return result, err
			}
		}

		k8sops.DeleteOwnerReferencesForResources(ctx, client, cr, splcommon.SplunkLicenseMaster)

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

	// create or update a service
	err = k8sops.ApplyService(ctx, client, resources.GetSplunkService(ctx, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkLicenseMaster, false))
	if err != nil {
		return result, err
	}

	// create or update statefulset
	statefulSet, err := getLicenseMasterStatefulSet(ctx, client, cr)
	if err != nil {
		return result, err
	}

	//make changes to respective mc configmap when changing/removing mcRef from spec
	err = k8sops.ValidateMonitoringConsoleRef(ctx, client, statefulSet, getLicenseMasterURL(cr, &cr.Spec.CommonSplunkSpec))
	if err != nil {
		return result, err
	}

	mgr := k8sops.DefaultStatefulSetPodManager{}
	phase, err := mgr.Update(ctx, client, statefulSet, 1)
	if err != nil {
		return result, err
	}
	cr.Status.Phase = phase

	if cr.Spec.MonitoringConsoleRef.Name != "" {
		_, err = k8sops.ApplyMonitoringConsoleEnvConfigMap(ctx, client, cr.GetNamespace(), cr.GetName(), cr.Spec.MonitoringConsoleRef.Name, getLicenseMasterURL(cr, &cr.Spec.CommonSplunkSpec), true)
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

		// Add a splunk operator telemetry app
		if cr.Spec.EtcVolumeStorageConfig.EphemeralStorage || !cr.Status.TelAppInstalled {
			podExecClient := splutil.GetPodExecClient(client, cr, "")
			err := telapp.AddTelApp(ctx, podExecClient, splcommon.NumberOfLicenseManagerReplicas, cr)
			if err != nil {
				return result, err
			}

			// Mark telemetry app as installed
			cr.Status.TelAppInstalled = true
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

// getLicenseMasterStatefulSet returns a Kubernetes StatefulSet object for a Splunk Enterprise license master.
func getLicenseMasterStatefulSet(ctx context.Context, client splcommon.ControllerClient, cr *enterpriseApiV3.LicenseMaster) (*appsv1.StatefulSet, error) {
	certMounts, err := certs.ReconcileCerts(ctx, client, cr, reconcileutil.ToCertEntries(cr.Spec.Certs, certs.AutoDNSNames(splcommon.SplunkLicenseMaster, cr.GetName(), cr.GetNamespace(), 1)))
	if err != nil {
		return nil, fmt.Errorf("reconcile certs: %w", err)
	}
	ss, err := k8sops.GetSplunkStatefulSet(ctx, client, cr, &cr.Spec.CommonSplunkSpec, splcommon.SplunkLicenseMaster, 1, []corev1.EnvVar{})
	if err != nil {
		return ss, err
	}
	certs.InjectCertMounts(&ss.Spec.Template, certMounts)

	// Setup App framework staging volume for apps
	resources.SetupAppsStagingVolume(ctx, client, cr, &ss.Spec.Template, &cr.Spec.AppFrameworkConfig)

	return ss, err
}

// validateLicenseMasterSpec checks validity and makes default updates to a LicenseMasterSpec, and returns error if something is wrong.
func validateLicenseMasterSpec(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApiV3.LicenseMaster) error {

	if !reflect.DeepEqual(cr.Status.AppContext.AppFrameworkConfig, cr.Spec.AppFrameworkConfig) {
		err := appframework.ValidateAppFrameworkSpec(ctx, &cr.Spec.AppFrameworkConfig, &cr.Status.AppContext, true, cr.GetObjectKind().GroupVersionKind().Kind)
		if err != nil {
			return err
		}
	}

	return reconcileutil.ValidateCommonSplunkSpec(ctx, c, &cr.Spec.CommonSplunkSpec, cr)
}

// getLicenseMasterURL returns URL of license master.
func getLicenseMasterURL(cr splcommon.MetaObject, spec *enterpriseApi.CommonSplunkSpec) []corev1.EnvVar {
	if spec.LicenseMasterRef.Name != "" {
		licenseManagerURL := splcommon.GetSplunkServiceName(splcommon.SplunkLicenseMaster, spec.LicenseMasterRef.Name, false)
		if spec.LicenseMasterRef.Namespace != "" {
			licenseManagerURL = splcommon.GetServiceFQDN(spec.LicenseMasterRef.Namespace, licenseManagerURL)
		}
		return []corev1.EnvVar{
			{
				Name:  splcommon.LicenseManagerURL,
				Value: licenseManagerURL,
			},
		}
	}
	return []corev1.EnvVar{
		{
			Name:  splcommon.LicenseManagerURL,
			Value: splcommon.GetSplunkServiceName(splcommon.SplunkLicenseMaster, cr.GetName(), false),
		},
	}
}
