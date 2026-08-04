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

package monitoring

import "time"

type QuerySelector struct {
	ConfigMapName string
	ConfigMapKey  string
}

// ContributorIdentity is stable across the database/cluster status handshake.
type ContributorIdentity struct {
	PostgresDatabaseName string
	PostgresDatabaseUID  string
	DatabaseName         string
	Namespace            string
}

// Exists=false is an explicit removal tombstone.
type DatabaseContribution struct {
	Identity          ContributorIdentity
	Revision          string
	Exists            bool
	Selectors         []QuerySelector
	CreationTimestamp time.Time
}

// Unpublished entries are not yet eligible for aggregation.
type DatabaseContributionSnapshot struct {
	Contributions []DatabaseContribution
	Unpublished   []ContributorIdentity
}

// AcknowledgementStatus mirrors ConditionStatus without a Kubernetes dependency.
type AcknowledgementStatus string

const (
	AcknowledgementTrue    AcknowledgementStatus = "True"
	AcknowledgementFalse   AcknowledgementStatus = "False"
	AcknowledgementUnknown AcknowledgementStatus = "Unknown"
)

type DatabaseAcknowledgement struct {
	Identity        ContributorIdentity
	DesiredRevision string
	AppliedRevision string
	Status          AcknowledgementStatus
	Reason          string
	Message         string
}
