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

package core

import (
	"context"
	"fmt"

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	dbmetrics "github.com/splunk/splunk-operator/pkg/postgresql/database/core/custom_metrics"
	"github.com/splunk/splunk-operator/pkg/postgresql/shared/ports"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func reconcileCustomMetricsGate(
	ctx context.Context,
	rc *ReconcileContext,
	postgresDB *enterprisev4.PostgresDatabase,
	cluster *enterprisev4.PostgresCluster,
) (dbmetrics.Outcome, error) {
	var repository dbmetrics.AcknowledgementRepository = emptyAcknowledgementRepository{}
	if rc.NewCustomMetricsAcknowledgementRepo != nil {
		repository = rc.NewCustomMetricsAcknowledgementRepo(cluster)
	}

	input := customMetricsGateInput(postgresDB)
	if condition := meta.FindStatusCondition(postgresDB.Status.Conditions, string(customMetricsReady)); condition != nil {
		input.DisabledAcknowledgementPending =
			condition.Status != metav1.ConditionTrue &&
				condition.Reason == string(reasonCustomMetricsPending)
	}
	return dbmetrics.NewModel(repository).Reconcile(ctx, input)
}

func customMetricsPublicationInput(postgresDB *enterprisev4.PostgresDatabase) dbmetrics.PublicationInput {
	input := dbmetrics.PublicationInput{
		OwnerName: postgresDB.Name,
		OwnerUID:  string(postgresDB.UID),
		Namespace: postgresDB.Namespace,
	}
	for _, database := range postgresDB.Spec.Databases {
		desired := dbmetrics.DesiredDatabase{Name: database.Name}
		if database.Monitoring != nil {
			for _, selector := range database.Monitoring.CustomQueriesConfigMap {
				desired.Selectors = append(desired.Selectors, mtypes.QuerySelector{
					ConfigMapName: selector.Name,
					ConfigMapKey:  selector.Key,
				})
			}
		}
		input.Databases = append(input.Databases, desired)
	}
	return input
}

func customMetricsGateInput(postgresDB *enterprisev4.PostgresDatabase) dbmetrics.GateInput {
	input := dbmetrics.GateInput{ClusterName: postgresDB.Spec.ClusterRef.Name}
	if postgresDB.Status.CustomMetricsPublication == nil {
		return input
	}
	for _, database := range postgresDB.Status.CustomMetricsPublication.Contributions {
		contribution := mtypes.DatabaseContribution{
			Identity: mtypes.ContributorIdentity{
				PostgresDatabaseName: postgresDB.Name,
				PostgresDatabaseUID:  string(postgresDB.UID),
				DatabaseName:         database.DatabaseName,
				Namespace:            postgresDB.Namespace,
			},
			Revision: database.Revision,
			Exists:   database.Exists,
		}
		for _, selector := range database.CustomQueriesConfigMap {
			contribution.Selectors = append(contribution.Selectors, mtypes.QuerySelector{
				ConfigMapName: selector.Name,
				ConfigMapKey:  selector.Key,
			})
		}
		input.Contributions = append(input.Contributions, contribution)
	}
	return input
}

type emptyAcknowledgementRepository struct{}

func (emptyAcknowledgementRepository) Find(_ context.Context, _ mtypes.ContributorIdentity) (mtypes.DatabaseAcknowledgement, bool, error) {
	return mtypes.DatabaseAcknowledgement{}, false, nil
}

func persistCustomMetricsPublication(
	ctx context.Context,
	c client.Client,
	postgresDB *enterprisev4.PostgresDatabase,
) (bool, error) {
	contributions := dbmetrics.PlanPublication(customMetricsPublicationInput(postgresDB))
	previouslyActive := make(map[string]struct{})
	if postgresDB.Status.CustomMetricsPublication != nil {
		for _, contribution := range postgresDB.Status.CustomMetricsPublication.Contributions {
			if contribution.Exists {
				previouslyActive[contribution.DatabaseName] = struct{}{}
			}
		}
	}
	disabledAcknowledgementRequired := false
	desired := &enterprisev4.PostgresDatabaseCustomMetricsPublication{
		ObservedGeneration: postgresDB.Generation,
		Contributions:      make([]enterprisev4.DatabaseCustomMetricsContribution, 0, len(contributions)),
	}
	for _, contribution := range contributions {
		info := enterprisev4.DatabaseCustomMetricsContribution{
			DatabaseName: contribution.Identity.DatabaseName,
			Revision:     contribution.Revision,
			Exists:       contribution.Exists,
		}
		for _, selector := range contribution.Selectors {
			info.CustomQueriesConfigMap = append(info.CustomQueriesConfigMap, corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: selector.ConfigMapName},
				Key:                  selector.ConfigMapKey,
			})
		}
		desired.Contributions = append(desired.Contributions, info)
		if !contribution.Exists {
			if _, wasActive := previouslyActive[contribution.Identity.DatabaseName]; wasActive {
				disabledAcknowledgementRequired = true
			}
		}
	}
	if equality.Semantic.DeepEqual(postgresDB.Status.CustomMetricsPublication, desired) {
		return false, nil
	}
	if disabledAcknowledgementRequired {
		meta.SetStatusCondition(&postgresDB.Status.Conditions, metav1.Condition{
			Type:   string(customMetricsReady),
			Status: metav1.ConditionUnknown,
			Reason: string(reasonCustomMetricsPending),
			Message: fmt.Sprintf(
				"Waiting for acknowledgement from PostgresCluster %q for disabled custom metrics",
				postgresDB.Spec.ClusterRef.Name,
			),
			ObservedGeneration: postgresDB.Generation,
		})
	}
	postgresDB.Status.CustomMetricsPublication = desired
	if err := c.Status().Update(ctx, postgresDB); err != nil {
		return false, err
	}
	return true, nil
}

func persistCustomMetricsStatus(
	ctx context.Context,
	rc *ReconcileContext,
	postgresDB *enterprisev4.PostgresDatabase,
	outcome dbmetrics.Outcome,
	status metav1.ConditionStatus,
	phase reconcileDBPhases,
) error {
	if !applyCustomMetricsStatus(rc, postgresDB, outcome, status, phase) {
		return nil
	}
	return rc.Client.Status().Update(ctx, postgresDB)
}

func applyCustomMetricsStatus(
	rc *ReconcileContext,
	postgresDB *enterprisev4.PostgresDatabase,
	outcome dbmetrics.Outcome,
	status metav1.ConditionStatus,
	phase reconcileDBPhases,
) bool {
	before := postgresDB.Status.DeepCopy()
	postgresDB.Status.Databases = populateDatabaseStatus(postgresDB, true, rolesExist)
	reason := conditionReasons(outcome.Reason)
	if reason == "" {
		reason = reasonCustomMetricsFailed
	}
	applyStatus(postgresDB, customMetricsReady, status, reason, outcome.Message, phase)
	if equality.Semantic.DeepEqual(*before, postgresDB.Status) {
		return false
	}
	if rc.Metrics != nil {
		rc.Metrics.IncStatusTransition(ports.ControllerDatabase, string(customMetricsReady), string(status), string(reason))
	}
	return true
}
