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

package kvservice

import (
	"context"
	"errors"
	"fmt"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Apply is the request-level boundary for KVService reconciliation.
func Apply(ctx context.Context, c splcommon.ControllerClient, key types.NamespacedName, recorder events.EventRecorder) (result reconcile.Result, err error) {
	logger := logging.FromContext(ctx).With("controller", "KVService", "name", key.Name, "namespace", key.Namespace, "reconcileID", controller.ReconcileIDFromContext(ctx))
	ctx = logging.WithLogger(ctx, logger)
	defer func() {
		if err == nil && result.RequeueAfter > 0 {
			logger.InfoContext(ctx, "requeued", "periodSeconds", int(result.RequeueAfter/time.Second))
		}
	}()

	cr := &enterpriseApi.KVService{}
	if err := c.Get(ctx, key, cr); err != nil {
		if apierrors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("load KVService: %w", err)
	}
	logger.InfoContext(ctx, "start", "crVersion", cr.GetResourceVersion(), "generation", cr.Generation)
	cr.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("KVService"))
	if cr.Annotations[enterpriseApi.KVServicePausedAnnotation] == "true" && cr.DeletionTimestamp == nil {
		if cr.Status.Phase == "" {
			cr.Status.Phase = enterpriseApi.PhasePending
		}
		setConditions(cr, true, "")
		if err := k8sops.UpdateKVServiceStatus(ctx, c, cr); err != nil {
			return reconcile.Result{}, fmt.Errorf("update paused KVService status: %w", err)
		}
		logger.InfoContext(ctx, "KVService reconciliation paused")
		return reconcile.Result{RequeueAfter: splcommon.PauseRetryDelay}, nil
	}
	return ApplyKVService(ctx, c, recorder, cr)
}
func setConditions(cr *enterpriseApi.KVService, paused bool, message string) {
	state := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{Phase: cr.Status.Phase, IsPaused: paused, Message: message, Generation: cr.Generation})
	cr.Status.Phase, cr.Status.Conditions = state.Phase, state.Conditions
	cr.Status.ObservedGeneration = cr.Generation
}

// ApplyKVService reconciles config and workloads and records status for every outcome.
func ApplyKVService(ctx context.Context, c splcommon.ControllerClient, recorder events.EventRecorder, cr *enterpriseApi.KVService) (result reconcile.Result, err error) {
	logger := logging.FromContext(ctx).With("func", "ApplyKVService")
	logger.DebugContext(ctx, "reconciling KVService", "generation", cr.Generation, "replicas", cr.Spec.Replicas)
	previousPhase := cr.Status.Phase
	result.RequeueAfter = 5 * time.Second
	cr.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("KVService"))
	cr.Status.Phase = enterpriseApi.PhaseError
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}
	publisher := k8sops.NewK8EventPublisherWithEventsRecorder(recorder, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, publisher)
	defer func() {
		message := ""
		if err != nil {
			message = err.Error()
		}
		setConditions(cr, false, message)
		statusErr := k8sops.UpdateKVServiceStatus(ctx, c, cr)
		if statusErr != nil && !(apierrors.IsNotFound(statusErr) && cr.DeletionTimestamp != nil) {
			err = errors.Join(err, fmt.Errorf("update KVService status: %w", statusErr))
		}
		if statusErr == nil && previousPhase != cr.Status.Phase {
			logger.InfoContext(ctx, "KVService phase changed", "previousPhase", previousPhase, "phase", cr.Status.Phase, "readyReplicas", cr.Status.ReadyReplicas)
		}
	}()
	if cr.DeletionTimestamp != nil {
		logger.InfoContext(ctx, "cleaning up KVService")
		cr.Status.Phase = enterpriseApi.PhaseTerminating
		if err = k8sops.DeleteKVServiceCR(ctx, c, cr); err != nil {
			return result, fmt.Errorf("clean up shared KVService: %w", err)
		}
		if _, err = splutil.RemoveSecretOwnerRef(ctx, c, splcommon.GetNamespaceScopedSecretName(cr.Namespace), cr); err != nil && !apierrors.IsNotFound(err) {
			return result, fmt.Errorf("remove KVService secret owner reference: %w", err)
		}
		terminating, deleteErr := k8sops.CheckForDeletion(ctx, cr, c)
		if !terminating {
			result.RequeueAfter = 0
		}
		if deleteErr != nil {
			return result, fmt.Errorf("check KVService deletion: %w", deleteErr)
		}
		return result, nil
	}
	if err = validateKVServiceSpec(ctx, c, cr); err != nil {
		publisher.Warning(ctx, splcommon.EventReasonValidateSpecFailed, "KVService spec validation failed")
		return result, fmt.Errorf("validate KVService spec: %w", err)
	}
	cr.Status.Replicas = cr.Spec.Replicas
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-kvservice", cr.Name)
	if _, err = k8sops.ApplySplunkConfig(ctx, c, cr, cr.Spec.CommonSplunkSpec, splcommon.SplunkKVService); err != nil {
		publisher.Warning(ctx, splcommon.EventReasonApplySplunkConfigFailed, "KVService configuration failed")
		return result, fmt.Errorf("apply KVService configuration: %w", err)
	}
	if err = k8sops.ApplyService(ctx, c, resources.GetKVServiceService(cr, &cr.Spec)); err != nil {
		publisher.Warning(ctx, splcommon.EventReasonApplyServiceFailed, "KVService Service reconciliation failed")
		return result, fmt.Errorf("apply KVService Service: %w", err)
	}
	deployment, err := k8sops.GetKVServiceDeployment(ctx, c, cr)
	if err != nil {
		return result, fmt.Errorf("configure KVService Deployment: %w", err)
	}
	phase, err := k8sops.ApplyDeployment(ctx, c, deployment)
	if err != nil {
		publisher.Warning(ctx, splcommon.EventReasonDeploymentFailed, "KVService Deployment reconciliation failed")
		return result, fmt.Errorf("apply KVService Deployment: %w", err)
	}
	cr.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	cr.Status.Phase = phase
	if phase == enterpriseApi.PhaseReady {
		publisher.Normal(ctx, splcommon.EventReasonReady, fmt.Sprintf("KVService %s is ready with %d replicas", cr.Name, cr.Status.ReadyReplicas))
		result.RequeueAfter = 0
	}
	return result, nil
}
