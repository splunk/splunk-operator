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

package majorversionupgrade

import (
	"context"

	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
)

type backupProvider interface {
	CreateBackup(context.Context, mvutypes.Intent, func(mvutypes.Intent) string) (*mvutypes.BackupInfo, error)
}

type majorUpgradeInfoStore interface {
	ReadMajorUpgradeIntent(context.Context) (mvutypes.Intent, bool, error)
	SaveMajorUpgradeProgress(context.Context, mvutypes.Intent, reconciliationTypes.Report, *mvutypes.BackupInfo) error
}
