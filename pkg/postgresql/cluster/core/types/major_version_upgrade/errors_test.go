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

package majorversionupgradetypes

import (
	"errors"
	"strings"
	"testing"
)

func TestUpgradeErrorConstructorPreservesNestedCause(t *testing.T) {
	dependencyErr := errors.New("dependency failed")
	err := errors.Join(ErrUpgradeFlowPending, dependencyErr)

	if !errors.Is(err, ErrUpgradeFlowPending) {
		t.Fatalf("expected error to match upgrade-flow-pending category")
	}
	if !errors.Is(err, dependencyErr) {
		t.Fatalf("expected error to preserve nested dependency cause")
	}
}

func TestUpgradeErrorReportMatchesConstructorCategory(t *testing.T) {
	err := errors.Join(ErrUpgradeFlowPending, errors.New("dependency failed"))
	report := ReportFromError(err)

	if report.Name != UseCaseName {
		t.Fatalf("expected report name %q, got %q", UseCaseName, report.Name)
	}
	if report.Phase != string(Upgrading) {
		t.Fatalf("expected report phase %q, got %q", Upgrading, report.Phase)
	}
	if report.Reason != ReasonUpgradeFlowPending {
		t.Fatalf("expected report reason %q, got %q", ReasonUpgradeFlowPending, report.Reason)
	}
	if !report.Retry {
		t.Fatalf("expected upgrade-flow-pending report to retry")
	}
	if report.Sleep == nil || *report.Sleep != reportSleepLongRetrySeconds {
		t.Fatalf("expected sleep %d, got %v", reportSleepLongRetrySeconds, report.Sleep)
	}
}

func TestReportFromErrorForMissingBackupProviderIsTerminal(t *testing.T) {
	report := ReportFromError(ErrBackupProviderMissing)
	if report.Phase != string(Failed) {
		t.Fatalf("phase = %q, want %q", report.Phase, Failed)
	}
	if report.Reason != ReasonBackupProviderMissing {
		t.Fatalf("reason = %q, want %q", report.Reason, ReasonBackupProviderMissing)
	}
	if report.Retry {
		t.Fatal("missing backup provider must not retry")
	}
}

func TestCandidatePluginReportsAreActionableWithoutLeakingDependencyErrors(t *testing.T) {
	for _, test := range []struct {
		err    error
		reason string
		phase  Status
		retry  bool
	}{
		{ErrCandidatePluginPending, ReasonCandidatePluginPending, Upgrading, true},
		{ErrCandidatePluginTimedOut, ReasonCandidatePluginTimedOut, Failed, false},
	} {
		t.Run(test.reason, func(t *testing.T) {
			report := ReportFromError(errors.Join(test.err, errors.New("sensitive dependency details")))
			if report.Reason != test.reason || report.Phase != string(test.phase) || report.Retry != test.retry {
				t.Fatalf("unexpected plugin report: %+v", report)
			}
			if strings.Contains(report.Message, "sensitive dependency details") || !strings.Contains(strings.ToLower(report.Message), "blue remains authoritative") {
				t.Fatalf("plugin report must explain source authority without dependency details: %q", report.Message)
			}
			if test.retry && !IsRetryableFailureReason(report.Reason) {
				t.Fatal("a plugin obstacle must be visible as retryable failure, not healthy progress")
			}
		})
	}
}
