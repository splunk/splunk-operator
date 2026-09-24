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

package custom_metrics

import (
	"context"
	"fmt"

	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

type GateState string

const (
	GateReady   GateState = "Ready"
	GatePending GateState = "Pending"
	GateFailed  GateState = "Failed"
)

type DesiredDatabase struct {
	Name      string
	Selectors []mtypes.QuerySelector
}

type PublicationInput struct {
	OwnerName string
	OwnerUID  string
	Namespace string
	Databases []DesiredDatabase
}

type GateInput struct {
	Contributions []mtypes.DatabaseContribution
	ClusterName   string
	// DisabledAcknowledgementPending keeps an explicit removal tombstone gated
	// after it has replaced the prior Exists=true status.
	DisabledAcknowledgementPending bool
}

type Outcome struct {
	State   GateState
	Reason  string
	Message string
}

// Status persistence remains owned by the enclosing PostgresDatabase reconciler.
type Model struct {
	acknowledgements AcknowledgementRepository
}

func NewModel(acknowledgements AcknowledgementRepository) *Model {
	return &Model{acknowledgements: acknowledgements}
}

func PlanPublication(in PublicationInput) []mtypes.DatabaseContribution {
	out := make([]mtypes.DatabaseContribution, 0, len(in.Databases))
	for _, database := range in.Databases {
		contribution := mtypes.DatabaseContribution{
			Identity: mtypes.ContributorIdentity{
				PostgresDatabaseName: in.OwnerName,
				PostgresDatabaseUID:  in.OwnerUID,
				DatabaseName:         database.Name,
				Namespace:            in.Namespace,
			},
			Exists:    len(database.Selectors) > 0,
			Selectors: append([]mtypes.QuerySelector(nil), database.Selectors...),
		}
		contribution.Revision = mtypes.ContributionRevision(
			database.Name,
			contribution.Exists,
			contribution.Selectors,
		)
		out = append(out, contribution)
	}
	return out
}

func (m *Model) Reconcile(ctx context.Context, in GateInput) (Outcome, error) {
	out := Outcome{
		State:   GateReady,
		Reason:  "CustomMetricsDisabled",
		Message: "Custom metrics are not configured",
	}
	anyExists := false
	for _, contribution := range in.Contributions {
		anyExists = anyExists || contribution.Exists
		if !contribution.Exists && !in.DisabledAcknowledgementPending {
			continue
		}
		ack, found, err := m.acknowledgements.Find(ctx, contribution.Identity)
		if err != nil {
			return out, err
		}
		if !found || ack.DesiredRevision != contribution.Revision {
			out.State = GatePending
			out.Reason = "CustomMetricsPending"
			out.Message = fmt.Sprintf(
				"Waiting for acknowledgement from PostgresCluster %q for database %q",
				in.ClusterName,
				contribution.Identity.DatabaseName,
			)
			continue
		}
		switch ack.Status {
		case mtypes.AcknowledgementFalse:
			message := fmt.Sprintf(
				"Custom metrics for database %q failed",
				contribution.Identity.DatabaseName,
			)
			if ack.Message != "" {
				message += ": " + ack.Message
			}
			return Outcome{
				State:   GateFailed,
				Reason:  ack.Reason,
				Message: message,
			}, nil
		case mtypes.AcknowledgementTrue:
			if ack.AppliedRevision != contribution.Revision {
				out.State = GatePending
				out.Reason = "CustomMetricsPending"
				out.Message = fmt.Sprintf("Waiting for revision %q to be applied for database %q", contribution.Revision, contribution.Identity.DatabaseName)
			}
		default:
			out.State = GatePending
			out.Reason = ack.Reason
			out.Message = ack.Message
		}
	}

	if out.State == GateReady && anyExists {
		out.Reason = "CustomMetricsReady"
		out.Message = "Database custom metrics are acknowledged by the PostgresCluster"
	}
	return out, nil
}
