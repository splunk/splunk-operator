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

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	mtypes "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
)

type AcknowledgementRepository struct {
	status *enterprisev4.CustomMetricsStatus
}

func NewAcknowledgementRepository(status *enterprisev4.CustomMetricsStatus) AcknowledgementRepository {
	return AcknowledgementRepository{status: status}
}

func (r AcknowledgementRepository) Find(_ context.Context, identity mtypes.ContributorIdentity) (mtypes.DatabaseAcknowledgement, bool, error) {
	if r.status == nil {
		return mtypes.DatabaseAcknowledgement{}, false, nil
	}
	for _, current := range r.status.DatabaseContributions {
		if current.PostgresDatabaseName != identity.PostgresDatabaseName ||
			current.PostgresDatabaseUID != identity.PostgresDatabaseUID ||
			current.DatabaseName != identity.DatabaseName {
			continue
		}
		return mtypes.DatabaseAcknowledgement{
			Identity:        identity,
			DesiredRevision: current.DesiredRevision,
			AppliedRevision: current.AppliedRevision,
			Status:          mtypes.AcknowledgementStatus(current.Status),
			Reason:          current.Reason,
			Message:         current.Message,
		}, true, nil
	}
	return mtypes.DatabaseAcknowledgement{}, false, nil
}
