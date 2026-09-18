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

package reconciliationTypes

// Phase is a PostgresDatabase reconciliation phase persisted in status.
type Phase string

// ConditionType is a PostgresDatabase condition type persisted in status.
type ConditionType string

// ConditionReason is a PostgresDatabase condition reason persisted in status.
type ConditionReason string

const (
	PhaseProvisioning Phase = "Provisioning"
	PhaseFailed       Phase = "Failed"

	ConditionRolesReady ConditionType = "RolesReady"

	ReasonWaitingForCNPG      ConditionReason = "WaitingForCNPG"
	ReasonRoleConflict        ConditionReason = "RoleConflict"
	ReasonRoleReconcileFailed ConditionReason = "RoleReconcileFailed"
	ReasonRolesAvailable      ConditionReason = "RolesAvailable"
)
