/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package clusterreadiness

import (
	"context"
	"errors"

	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/database/core/types/reconciliation"
)

const (
	conditionClusterReady = "ClusterReady"
	phasePending          = "Pending"
	phaseProvisioning     = "Provisioning"

	reasonClusterNotFound            = "ClusterNotFound"
	reasonClusterProvisioning        = "ClusterProvisioning"
	reasonClusterRecovery            = "ClusterRecovery"
	reasonClusterInfoFetchFailed     = "ClusterInfoFetchNotPossible"
	reasonClusterReaderNotConfigured = "ClusterReaderNotConfigured"
	reasonClusterAvailable           = "ClusterAvailable"

	messageClusterNotFound            = "Cluster CR not found"
	messageClusterProvisioning        = "Cluster is not in ready state yet"
	messageClusterRecovery            = "Cluster is recovering; waiting for it to become ready"
	messageClusterInfoFetchFailed     = "Can't reach Cluster CR due to transient errors"
	messageClusterReaderNotConfigured = "Cluster readiness reader is not configured"
	messageClusterAvailable           = "Cluster is operational"
)

// Result is the authoritative readiness decision. Events are intentionally not
// part of it because their transition policy belongs to the database facade.
type Result struct {
	Facts   ResolvedClusterFacts
	Outcome reconciliationTypes.Outcome
}

// Gate observes one referenced cluster. It does not mutate Kubernetes objects
// and intentionally has no Reconcile method.
type Gate struct {
	reader ClusterReader
}

// New creates an observation-only database cluster readiness gate.
func New(reader ClusterReader) Gate {
	return Gate{reader: reader}
}

// Observe reads cluster facts and classifies the database prerequisite.
func (g Gate) Observe(ctx context.Context, input Input) Result {
	if g.reader == nil {
		return Result{Outcome: reconciliationTypes.RetryableRequeue(
			conditionClusterReady,
			reasonClusterReaderNotConfigured,
			messageClusterReaderNotConfigured,
			phasePending,
			ErrClusterReaderNotConfigured,
		)}
	}

	facts, err := g.reader.Read(ctx, input.Namespace, input.Name)
	if err != nil {
		if errors.Is(err, ErrClusterNotFound) {
			return Result{Outcome: reconciliationTypes.Waiting(
				conditionClusterReady,
				reasonClusterNotFound,
				messageClusterNotFound,
				phasePending,
				reconciliationTypes.ClusterNotFoundRetryDelay,
			)}
		}
		return Result{Outcome: reconciliationTypes.RetryableRequeue(
			conditionClusterReady,
			reasonClusterInfoFetchFailed,
			messageClusterInfoFetchFailed,
			phasePending,
			err,
		)}
	}

	if facts.Lifecycle != LifecycleReady || facts.Provider == nil {
		if facts.Recovery == RecoveryInProgress && (input.WasReady || input.PreviousClusterReadyReason == reasonClusterRecovery) {
			return Result{Facts: facts, Outcome: reconciliationTypes.Waiting(
				conditionClusterReady,
				reasonClusterRecovery,
				messageClusterRecovery,
				phasePending,
				reconciliationTypes.ReadinessRetryDelay,
			)}
		}
		return Result{Facts: facts, Outcome: reconciliationTypes.Waiting(
			conditionClusterReady,
			reasonClusterProvisioning,
			messageClusterProvisioning,
			phasePending,
			reconciliationTypes.ReadinessRetryDelay,
		)}
	}

	return Result{Facts: facts, Outcome: reconciliationTypes.ConvergedStatus(
		conditionClusterReady,
		reasonClusterAvailable,
		messageClusterAvailable,
		phaseProvisioning,
	)}
}
