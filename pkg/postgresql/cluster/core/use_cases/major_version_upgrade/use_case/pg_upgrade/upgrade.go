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

package pgupgradeflow

import (
	"context"
	"errors"

	"github.com/splunk/splunk-operator/pkg/logging"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
)

var errDriverNotConfigured = errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("pg_upgrade port is not configured"))

type pgUpgradeFlow struct {
	state  mvutypes.Status
	driver PgUpgrade
}

func NewPgUpgradeFlow(driver PgUpgrade, state mvutypes.Status) *pgUpgradeFlow {
	if state == "" {
		state = mvutypes.Scheduled
	}
	return &pgUpgradeFlow{state: state, driver: driver}
}

func (v *pgUpgradeFlow) Upgrade(ctx context.Context) (reconciliationTypes.Report, error) {
	logging.FromContext(ctx).InfoContext(ctx, "major version upgrade in progress", "state", string(v.state))

	handleState := func() (reconciliationTypes.Report, error) {
		switch v.state {
		case mvutypes.Scheduled:
			return v.onScheduled(ctx)
		case mvutypes.Preflight:
			return v.onPreflight(ctx)
		case mvutypes.Upgrading:
			return v.onUpgrading(ctx)
		case mvutypes.Verifying:
			return v.onVerifying(ctx)
		default:
			err := errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("unknown pg_upgrade phase: "+string(v.state)))
			return mvutypes.ReportFromError(err), err
		}
	}
	return handleState()
}

func (v *pgUpgradeFlow) onScheduled(_ context.Context) (reconciliationTypes.Report, error) {
	if v.driver == nil {
		return mvutypes.ReportFromError(errDriverNotConfigured), errDriverNotConfigured
	}
	return reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Preflight),
		Reason:  mvutypes.ReasonPreflightCheckPassed,
		Message: mvutypes.MessagePreflightCheckPassed,
		Retry:   true,
	}, nil
}

func (v *pgUpgradeFlow) onPreflight(ctx context.Context) (reconciliationTypes.Report, error) {
	if v.driver == nil {
		return mvutypes.ReportFromError(errDriverNotConfigured), errDriverNotConfigured
	}
	if err := v.driver.ApplyTargetImage(ctx); err != nil {
		if errors.Is(err, mvutypes.ErrUpgradeFlowFailed) {
			return mvutypes.ReportFromError(err), err
		}
		return reconciliationTypes.Report{
			Name:    mvutypes.UseCaseName,
			Phase:   string(mvutypes.Preflight),
			Reason:  mvutypes.ReasonUpgradeFlowPending,
			Message: mvutypes.MessagePgUpgradeStartPending,
			Retry:   true,
		}, nil
	}

	return reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Upgrading),
		Reason:  mvutypes.ReasonPgUpgradeStarted,
		Message: mvutypes.MessagePgUpgradeStarted,
		Retry:   true,
	}, nil
}

func (v *pgUpgradeFlow) onUpgrading(ctx context.Context) (reconciliationTypes.Report, error) {
	if v.driver == nil {
		return mvutypes.ReportFromError(errDriverNotConfigured), errDriverNotConfigured
	}
	done, err := v.driver.UpgradeComplete(ctx)
	if err != nil {
		if errors.Is(err, mvutypes.ErrUpgradeFlowFailed) ||
			errors.Is(err, mvutypes.ErrUpgradeUnrecoverablePreConversion) ||
			errors.Is(err, mvutypes.ErrUpgradeUnrecoverablePostConversion) {
			return mvutypes.ReportFromError(err), err
		}
		return mvutypes.ReportFromError(mvutypes.ErrUpgradeFlowPending), nil
	}
	if !done {
		return mvutypes.ReportFromError(mvutypes.ErrUpgradeFlowPending), nil
	}

	return reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.Verifying),
		Reason:  mvutypes.ReasonPgUpgradeObservedComplete,
		Message: mvutypes.MessagePgUpgradeObservedComplete,
		Retry:   true,
	}, nil
}

func (v *pgUpgradeFlow) onVerifying(ctx context.Context) (reconciliationTypes.Report, error) {
	if v.driver == nil {
		return mvutypes.ReportFromError(errDriverNotConfigured), errDriverNotConfigured
	}
	verified, err := v.driver.VerifyUpgrade(ctx)
	if err != nil {
		err := errors.Join(mvutypes.ErrUpgradeVerificationFailed, err)
		return mvutypes.ReportFromError(err), err
	}
	if !verified {
		return reconciliationTypes.Report{
			Name:    mvutypes.UseCaseName,
			Phase:   string(mvutypes.Verifying),
			Reason:  mvutypes.ReasonUpgradeFlowPending,
			Message: mvutypes.MessagePgUpgradeVerificationPending,
			Retry:   true,
		}, nil
	}

	return reconciliationTypes.Report{
		Name:    mvutypes.UseCaseName,
		Phase:   string(mvutypes.PostUpgradeBackup),
		Reason:  mvutypes.ReasonPgUpgradeFinalized,
		Message: mvutypes.MessagePgUpgradeFinalized,
		Retry:   true,
	}, nil
}
