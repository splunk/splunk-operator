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
	"slices"
	"time"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/certs"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
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
	state := splcommon.SetPhaseAndConditions(cr.Status.Conditions, splcommon.PhaseConditionInput{
		Phase: cr.Status.Phase, IsPaused: paused, Message: message, Generation: cr.Generation,
	})
	cr.Status.Phase, cr.Status.Conditions = state.Phase, state.Conditions
	cr.Status.ObservedGeneration = cr.Generation
}

// ApplyKVService reconciles config and workloads and records status for every outcome.
func ApplyKVService(ctx context.Context, c splcommon.ControllerClient, recorder events.EventRecorder, cr *enterpriseApi.KVService) (result reconcile.Result, err error) {
	logger := logging.FromContext(ctx).With("func", "ApplyKVService")
	logger.DebugContext(ctx, "reconciling KVService", "generation", cr.Generation, "replicas", ptr.Deref(cr.Spec.Replicas, 0))
	previousPhase := cr.Status.Phase
	previousConditions := slices.Clone(cr.Status.Conditions)
	previousTenants := slices.Clone(cr.Status.Tenants)
	result.RequeueAfter = 5 * time.Second
	cr.SetGroupVersionKind(enterpriseApi.GroupVersion.WithKind("KVService"))
	cr.Status.Phase = enterpriseApi.PhaseError
	var statusMessage string
	var tenantsEvaluated bool
	var postgres tenantPostgresResult
	if cr.Status.ResourceRevMap == nil {
		cr.Status.ResourceRevMap = make(map[string]string)
	}
	publisher := k8sops.NewK8EventPublisherWithEventsRecorder(recorder, cr)
	ctx = context.WithValue(ctx, splcommon.EventPublisherKey, publisher)
	defer func() {
		message := statusMessage
		terminalMessage, terminal := splcommon.TerminalMessage(err)
		if err != nil {
			message = err.Error()
		}
		if terminal {
			message = terminalMessage
		}
		setConditions(cr, false, message)
		setTenantCondition(cr, tenantsEvaluated)
		setClientProtocolCondition(cr, postgres)
		if terminal {
			reason, _ := splcommon.TerminalReason(err)
			cr.Status.Phase = enterpriseApi.PhaseError
			cr.Status.Conditions = splcommon.UpsertStalledCondition(cr.Status.Conditions, reason, terminalMessage, cr.Generation)
			result = reconcile.Result{}
		} else {
			cr.Status.Conditions = splcommon.ClearStalledCondition(cr.Status.Conditions, cr.Generation)
		}
		statusErr := k8sops.UpdateKVServiceStatus(ctx, c, cr)
		if statusErr != nil && !(apierrors.IsNotFound(statusErr) && cr.DeletionTimestamp != nil) {
			if terminal {
				// Retry a failed status write even when the spec error itself is terminal.
				logger.ErrorContext(ctx, "could not persist terminal KVService condition", "error", statusErr)
				err = fmt.Errorf("update KVService status: %w", statusErr)
			} else {
				err = errors.Join(err, fmt.Errorf("update KVService status: %w", statusErr))
			}
		}
		if statusErr == nil {
			if tenantsEvaluated {
				recordTenantEvents(ctx, publisher, cr, previousTenants)
			}
			recordClientProtocolEvent(ctx, publisher, cr, previousConditions)
			k8sops.EmitStalledTransitionEvents(ctx, publisher, cr.Name, previousConditions, cr.Status.Conditions)
			if previousPhase != enterpriseApi.PhaseReady && cr.Status.Phase == enterpriseApi.PhaseReady {
				publisher.Normal(ctx, splcommon.EventReasonReady, fmt.Sprintf("KVService %s is ready with %d replicas", cr.Name, cr.Status.ReadyReplicas))
			}
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
		terminating, deleteErr := k8sops.CheckForDeletion(ctx, cr, c)
		if !terminating {
			result.RequeueAfter = 0
		}
		if deleteErr != nil {
			return result, fmt.Errorf("check KVService deletion: %w", deleteErr)
		}
		return result, nil
	}
	if (cr.Spec.Scheme == "" || cr.Spec.Scheme == "http") && len(cr.Spec.Certs) > 0 {
		logger.WarnContext(ctx, "spec.certs is ignored because spec.scheme is http; set spec.scheme to https to enable TLS", "name", cr.Name, "namespace", cr.Namespace)
	}
	if err = validateKVServiceSpec(ctx, c, cr); err != nil {
		publisher.Warning(ctx, splcommon.EventReasonValidateSpecFailed, "KVService spec validation failed")
		return result, fmt.Errorf("validate KVService spec: %w", err)
	}
	// Stop autoscaling even when PostgreSQL or workload dependencies are unavailable.
	if cr.Spec.Autoscaling == nil {
		if err = reconcileAutoscaler(ctx, c, cr); err != nil {
			publisher.Warning(ctx, splcommon.EventReasonAutoscalerFailed, "KVService autoscaler reconciliation failed")
			return result, fmt.Errorf("reconcile KVService autoscaler: %w", err)
		}
	}
	var postgresErr error
	postgres, postgresErr = ensureTenantPostgresReady(ctx, c, cr)
	statusMessage = postgres.message
	tenantsEvaluated = postgres.tenantsEvaluated
	if postgresErr != nil {
		return result, fmt.Errorf("prepare KVService PostgreSQL dependency: %w", postgresErr)
	}
	if !postgres.ready {
		return result, nil
	}
	cr.Status.Replicas = ptr.Deref(cr.Spec.Replicas, 0)
	cr.Status.Selector = fmt.Sprintf("app.kubernetes.io/instance=splunk-%s-kvservice", cr.Name)
	// Validate TLS before changing either serving resource.
	certMounts, extraEnv, certErr := reconcileKVServiceCertificates(ctx, c, cr)
	if certErr != nil {
		return result, certErr
	}
	if err = k8sops.ApplyService(ctx, c, resources.GetKVServiceService(cr, &cr.Spec)); err != nil {
		publisher.Warning(ctx, splcommon.EventReasonApplyServiceFailed, "KVService Service reconciliation failed")
		return result, fmt.Errorf("apply KVService Service: %w", err)
	}
	deployment, err := k8sops.GetKVServiceDeploymentWithPostgresDependencies(ctx, c, cr, postgres.dependencies, extraEnv)
	if err != nil {
		return result, fmt.Errorf("configure KVService Deployment: %w", err)
	}
	certs.InjectCertMounts(&deployment.Spec.Template, certMounts)
	phase, err := k8sops.ApplyDeployment(ctx, c, deployment)
	if err != nil {
		publisher.Warning(ctx, splcommon.EventReasonDeploymentFailed, "KVService Deployment reconciliation failed")
		return result, fmt.Errorf("apply KVService Deployment: %w", err)
	}
	cr.Status.ReadyReplicas = deployment.Status.ReadyReplicas
	if cr.Spec.Autoscaling != nil {
		if err = reconcileAutoscaler(ctx, c, cr); err != nil {
			publisher.Warning(ctx, splcommon.EventReasonAutoscalerFailed, "KVService autoscaler reconciliation failed")
			return result, fmt.Errorf("reconcile KVService autoscaler: %w", err)
		}
	}
	cr.Status.Phase = phase
	if phase == enterpriseApi.PhaseReady {
		result.RequeueAfter = 0
		if postgres.retryTenants {
			result.RequeueAfter = 5 * time.Second
		}
	}
	return result, nil
}

func recordTenantEvents(ctx context.Context, publisher *k8sops.K8EventPublisher, cr *enterpriseApi.KVService, previous []enterpriseApi.KVServiceTenantStatus) {
	if cr.Status.Tenants == nil {
		return
	}
	old := make(map[string]enterpriseApi.KVServiceTenantStatus, len(previous))
	for _, tenant := range previous {
		old[tenant.Namespace] = tenant
	}
	for _, tenant := range cr.Status.Tenants {
		if before, exists := old[tenant.Namespace]; exists && before == tenant {
			continue
		}
		message := fmt.Sprintf("Tenant %q in namespace %q: %s", tenant.Name, tenant.Namespace, tenant.Message)
		if tenant.Ready {
			publisher.Normal(ctx, splcommon.EventReasonTenantReady, message)
		} else {
			publisher.Warning(ctx, splcommon.EventReasonTenantNotReady, message)
		}
	}
}

// Preserve last-known tenant details on early returns without claiming they were
// evaluated in this reconciliation. Namespace changes do not bump CR generation.
func setTenantCondition(cr *enterpriseApi.KVService, evaluated bool) {
	condition := metav1.Condition{
		Type:               "TenantsReady",
		Status:             metav1.ConditionUnknown,
		Reason:             "TenantEvaluationPending",
		Message:            "Tenant prerequisites were not evaluated in this reconciliation; tenant entries are last-known observations",
		ObservedGeneration: cr.Generation,
	}
	if evaluated {
		ready := 0
		for _, tenant := range cr.Status.Tenants {
			if tenant.Ready {
				ready++
			}
		}
		condition.Status = metav1.ConditionTrue
		condition.Reason = "AllTenantsReady"
		condition.Message = fmt.Sprintf("%d of %d tenants have ready database and authentication dependencies", ready, len(cr.Status.Tenants))
		if len(cr.Status.Tenants) == 0 {
			condition.Reason = "NoTenants"
			condition.Message = "No tenant namespaces select this KVService"
		} else if ready < len(cr.Status.Tenants) {
			condition.Status = metav1.ConditionFalse
			condition.Reason = "TenantsBlocked"
		}
	}
	apimeta.SetStatusCondition(&cr.Status.Conditions, condition)
}
