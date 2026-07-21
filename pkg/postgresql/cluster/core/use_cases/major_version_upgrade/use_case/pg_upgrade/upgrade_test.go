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
	"fmt"
	"testing"

	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
)

func TestPgUpgradeFlowScheduledAdvancesToPreflight(t *testing.T) {
	report, err := NewPgUpgradeFlow(&fakePgUpgrade{}, mvutypes.Scheduled).Upgrade(t.Context())
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if report.Phase != string(mvutypes.Preflight) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Preflight)
	}
	if !report.Retry {
		t.Fatalf("expected preflight report to retry")
	}
}

func TestPgUpgradeFlowPreflightTerminatesOnPermanentDriverError(t *testing.T) {
	terminalErr := errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("target image is empty"))
	driver := &fakePgUpgrade{applyErr: terminalErr}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Preflight).Upgrade(t.Context())
	if err == nil {
		t.Fatalf("Upgrade() error = nil, want terminal error")
	}
	if !errors.Is(err, mvutypes.ErrUpgradeFlowFailed) {
		t.Fatalf("Upgrade() error = %v, want ErrUpgradeFlowFailed", err)
	}
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
	if report.Retry {
		t.Fatalf("terminal error must not retry")
	}
}

func TestPgUpgradeFlowPreflightRetriesOnTransientDriverError(t *testing.T) {
	driver := &fakePgUpgrade{applyErr: errors.New("connection refused")}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Preflight).Upgrade(t.Context())
	if err != nil {
		t.Fatalf("Upgrade() error = %v, want nil for transient error", err)
	}
	if report.Phase != string(mvutypes.Preflight) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Preflight)
	}
	if !report.Retry {
		t.Fatalf("transient error must retry")
	}
}

func TestPgUpgradeFlowPreflightStartsUpgrade(t *testing.T) {
	driver := &fakePgUpgrade{}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Preflight).Upgrade(t.Context())
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if !driver.started {
		t.Fatalf("expected upgrade to start")
	}
	if report.Phase != string(mvutypes.Upgrading) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Upgrading)
	}
}

func TestPgUpgradeFlowUpgradingWaitsUntilComplete(t *testing.T) {
	driver := &fakePgUpgrade{}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Upgrading).Upgrade(t.Context())
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if report.Phase != string(mvutypes.Upgrading) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Upgrading)
	}
	if !report.Retry {
		t.Fatalf("expected upgrading report to retry")
	}
}

func TestPgUpgradeFlowUpgradingMovesToVerifyingWhenComplete(t *testing.T) {
	driver := &fakePgUpgrade{complete: true}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Upgrading).Upgrade(t.Context())
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if report.Phase != string(mvutypes.Verifying) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Verifying)
	}
}

func TestPgUpgradeFlowUpgradingFailsOnTerminalDriverError(t *testing.T) {
	driver := &fakePgUpgrade{
		completeErr: errors.Join(mvutypes.ErrUpgradeFlowFailed, errors.New("cnpg cluster requires user action")),
	}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Upgrading).Upgrade(t.Context())
	if err == nil {
		t.Fatalf("Upgrade() error = nil, want terminal error")
	}
	if !errors.Is(err, mvutypes.ErrUpgradeFlowFailed) {
		t.Fatalf("Upgrade() error = %v, want ErrUpgradeFlowFailed", err)
	}
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
	if report.Retry {
		t.Fatalf("terminal driver error should not retry")
	}
}

func TestPgUpgradeFlowUpgradingFailsOnUnrecoverablePreConversion(t *testing.T) {
	driver := &fakePgUpgrade{
		completeErr: fmt.Errorf("%w: pg_upgrade job exited code 1", mvutypes.ErrUpgradeUnrecoverablePreConversion),
	}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Upgrading).Upgrade(t.Context())
	if err == nil {
		t.Fatalf("Upgrade() error = nil, want terminal error")
	}
	if !errors.Is(err, mvutypes.ErrUpgradeUnrecoverablePreConversion) {
		t.Fatalf("Upgrade() error = %v, want ErrUpgradeUnrecoverablePreConversion", err)
	}
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
	if report.Retry {
		t.Fatalf("unrecoverable pre-conversion error must not retry")
	}
}

func TestPgUpgradeFlowUpgradingFailsOnUnrecoverablePostConversion(t *testing.T) {
	driver := &fakePgUpgrade{
		completeErr: fmt.Errorf("%w: pg_upgrade job exited code 1", mvutypes.ErrUpgradeUnrecoverablePostConversion),
	}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Upgrading).Upgrade(t.Context())
	if err == nil {
		t.Fatalf("Upgrade() error = nil, want terminal error")
	}
	if !errors.Is(err, mvutypes.ErrUpgradeUnrecoverablePostConversion) {
		t.Fatalf("Upgrade() error = %v, want ErrUpgradeUnrecoverablePostConversion", err)
	}
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
	if report.Retry {
		t.Fatalf("unrecoverable post-conversion error must not retry")
	}
}

func TestPgUpgradeFlowVerifyingMovesToPostUpgradeBackup(t *testing.T) {
	driver := &fakePgUpgrade{}

	report, err := NewPgUpgradeFlow(driver, mvutypes.Verifying).Upgrade(t.Context())
	if err != nil {
		t.Fatalf("Upgrade() error = %v", err)
	}
	if !driver.verified {
		t.Fatalf("expected upgrade verification to run")
	}
	if report.Phase != string(mvutypes.PostUpgradeBackup) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.PostUpgradeBackup)
	}
}

func TestPgUpgradeFlowVerificationFailureReportsFailedPhase(t *testing.T) {
	driver := &fakePgUpgrade{verifyErr: errors.Join(mvutypes.ErrUpgradeVerificationFailed, errors.New("data checksum mismatch"))}

	report, _ := NewPgUpgradeFlow(driver, mvutypes.Verifying).Upgrade(t.Context())
	if report.Phase != string(mvutypes.Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, mvutypes.Failed)
	}
}

type fakePgUpgrade struct {
	started     bool
	applyErr    error
	complete    bool
	completeErr error
	verified    bool
	verifyErr   error
}

func (f *fakePgUpgrade) ApplyTargetImage(context.Context) error {
	if f.applyErr != nil {
		return f.applyErr
	}
	f.started = true
	return nil
}

func (f *fakePgUpgrade) UpgradeComplete(context.Context) (bool, error) {
	return f.complete, f.completeErr
}

func (f *fakePgUpgrade) VerifyUpgrade(context.Context) error {
	if f.verifyErr != nil {
		return f.verifyErr
	}
	f.verified = true
	return nil
}
