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

package usecases

import (
	"context"
	"errors"
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	majorversionupgradetypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
)

func TestReconcileSkipsUnscheduledUseCase(t *testing.T) {
	useCase := &fakeUseCase{}

	report, err := reconcilerFromUseCases([]string{"test"}, map[string]UseCase{"test": useCase}).Reconcile(t.Context())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if report != nil {
		t.Fatalf("Reconcile() report = %#v, want nil", report)
	}
	if useCase.acted {
		t.Fatalf("Act() was called for unscheduled use case")
	}
}

func TestReconcileActsScheduledUseCase(t *testing.T) {
	useCase := &fakeUseCase{
		scheduled: true,
		report: reconciliationTypes.Report{
			Name: "test",
		},
	}

	report, err := reconcilerFromUseCases([]string{"test"}, map[string]UseCase{"test": useCase}).Reconcile(t.Context())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if report != nil {
		t.Fatalf("Reconcile() report = %#v, want nil", report)
	}
	if !useCase.acted {
		t.Fatalf("Act() was not called")
	}
}

func TestReconcileReturnsRetryReport(t *testing.T) {
	useCase := &fakeUseCase{
		scheduled: true,
		report: reconciliationTypes.Report{
			Name:  "test",
			Retry: true,
		},
	}

	report, err := reconcilerFromUseCases([]string{"test"}, map[string]UseCase{"test": useCase}).Reconcile(t.Context())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if report == nil || !report.Retry {
		t.Fatalf("Reconcile() report = %#v, want retry report", report)
	}
}

func TestCheckPrerequisitesReturnsUnmetError(t *testing.T) {
	prereqErr := errors.New("source version not ready")
	reconciler := reconcilerFromUseCases([]string{"test"}, map[string]UseCase{
		"test": &fakeUseCase{prerequisiteErr: prereqErr},
	})

	if err := reconciler.CheckPrerequisites(t.Context()); !errors.Is(err, prereqErr) {
		t.Fatalf("CheckPrerequisites() error = %v, want %v", err, prereqErr)
	}
}

func TestCheckPrerequisitesPassesWhenAllMet(t *testing.T) {
	reconciler := reconcilerFromUseCases([]string{"test"}, map[string]UseCase{
		"test": &fakeUseCase{},
	})

	if err := reconciler.CheckPrerequisites(t.Context()); err != nil {
		t.Fatalf("CheckPrerequisites() error = %v, want nil", err)
	}
}

func TestReconcileOnlyActsFirstScheduledUseCase(t *testing.T) {
	first := &fakeUseCase{
		scheduled: true,
		report:    reconciliationTypes.Report{Name: "first", Retry: false},
	}
	second := &fakeUseCase{scheduled: true}
	reconciler := reconcilerFromUseCases([]string{"first", "second"}, map[string]UseCase{
		"first":  first,
		"second": second,
	})

	if err := reconciler.Schedule(t.Context()); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	report, err := reconciler.Reconcile(t.Context())
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if report != nil {
		t.Fatalf("Reconcile() report = %#v, want nil", report)
	}
	if !first.acted {
		t.Fatalf("first use case was not acted on")
	}
	if second.acted {
		t.Fatalf("second use case was acted on despite single-active invariant")
	}
}

func TestReconcileReturnsScheduleError(t *testing.T) {
	scheduleErr := errors.New("schedule failed")
	useCase := &fakeUseCase{
		scheduleErr: scheduleErr,
	}

	report, err := reconcilerFromUseCases([]string{"test"}, map[string]UseCase{"test": useCase}).Reconcile(t.Context())
	if !errors.Is(err, scheduleErr) {
		t.Fatalf("Reconcile() error = %v, want %v", err, scheduleErr)
	}
	if report != nil {
		t.Fatalf("Reconcile() report = %#v, want nil", report)
	}
}

func TestBlocksComponentsReturnsOnlyScheduledUseCaseBlocks(t *testing.T) {
	reconciler := reconcilerFromUseCases([]string{"scheduled", "unscheduled"}, map[string]UseCase{
		"scheduled": &fakeUseCase{
			scheduled:  true,
			components: []string{"provisioner"},
		},
		"unscheduled": &fakeUseCase{
			components: []string{"backup"},
		},
	})

	if err := reconciler.Schedule(t.Context()); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}

	blocked := reconciler.BlocksComponents()
	if _, ok := blocked["provisioner"]; !ok {
		t.Fatalf("BlocksComponents() missing scheduled component block")
	}
	if _, ok := blocked["backup"]; ok {
		t.Fatalf("BlocksComponents() included unscheduled component block")
	}
}

func TestReconcilerSkipsUseCaseAbsentFromRegistry(t *testing.T) {
	reconciler := NewUseCaseReconciler(nil, map[string]Factory{})

	if err := reconciler.Schedule(t.Context()); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if len(reconciler.BlocksComponents()) != 0 {
		t.Fatalf("absent use case contributed component blocks: %v", reconciler.BlocksComponents())
	}
	report, err := reconciler.Reconcile(t.Context())
	if err != nil || report != nil {
		t.Fatalf("Reconcile() = (%#v, %v), want (nil, nil)", report, err)
	}
}

func TestReconcilerConstructsRegisteredFactoryOnceAndActs(t *testing.T) {
	allow := true
	useCase := &fakeUseCase{scheduled: true}
	built := 0
	reconciler := NewUseCaseReconciler(
		&platformv1alpha1.PostgresClusterSpec{PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{Allow: &allow}},
		map[string]Factory{
			majorversionupgradetypes.UseCaseName: func() UseCase { built++; return useCase },
		},
	)

	if err := reconciler.Schedule(t.Context()); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	if _, err := reconciler.Reconcile(t.Context()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}

	if built != 1 {
		t.Fatalf("Factory was invoked %d times; want 1 (memoised across Schedule/Reconcile)", built)
	}
	if !useCase.acted {
		t.Fatalf("Act() did not run for a registered, scheduled use case")
	}
}

func TestReconcilerSkipsFactoryReturningNil(t *testing.T) {
	allow := true
	reconciler := NewUseCaseReconciler(
		&platformv1alpha1.PostgresClusterSpec{PostgresMajorUpgradeConfig: &platformv1alpha1.PostgresMajorUpgradeConfig{Allow: &allow}},
		map[string]Factory{
			majorversionupgradetypes.UseCaseName: func() UseCase { return nil },
		},
	)

	if err := reconciler.Schedule(t.Context()); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	report, err := reconciler.Reconcile(t.Context())
	if err != nil || report != nil {
		t.Fatalf("Reconcile() = (%#v, %v), want (nil, nil) for a nil-returning factory", report, err)
	}
}

func TestReconcilerTriggerPolicySkipsUntriggeredUseCase(t *testing.T) {
	built := 0
	// allow is absent (nil) — trigger policy should gate the use case out.
	reconciler := NewUseCaseReconciler(
		&platformv1alpha1.PostgresClusterSpec{},
		map[string]Factory{
			majorversionupgradetypes.UseCaseName: func() UseCase { built++; return &fakeUseCase{scheduled: true} },
		},
	)

	if err := reconciler.Schedule(t.Context()); err != nil {
		t.Fatalf("Schedule() error = %v", err)
	}
	report, err := reconciler.Reconcile(t.Context())
	if err != nil || report != nil {
		t.Fatalf("Reconcile() = (%#v, %v), want (nil, nil) for untriggered use case", report, err)
	}
	if built != 0 {
		t.Fatalf("factory was invoked %d times; want 0 — trigger policy should prevent construction", built)
	}
}

// TestScheduleDefersUseCaseWhenPrerequisitesFail verifies that an expected
// not-ready prerequisite is skipped by Schedule — it does not block other use
// cases, is not added to the scheduled set, and therefore neither blocks
// components nor runs Act.
func TestScheduleDefersUseCaseWhenPrerequisitesFail(t *testing.T) {
	prereqErr := errors.Join(ErrPrerequisiteNotReady, errors.New("source version not ready"))
	deferred := &fakeUseCase{prerequisiteErr: prereqErr, components: []string{"provisioner"}}
	reconciler := reconcilerFromUseCases([]string{"test"}, map[string]UseCase{"test": deferred})

	if err := reconciler.Schedule(t.Context()); err != nil {
		t.Fatalf("Schedule() error = %v, want nil (prerequisite failure should defer, not propagate)", err)
	}
	if len(reconciler.BlocksComponents()) != 0 {
		t.Fatalf("BlocksComponents() = %v, want empty: deferred use case must not block components", reconciler.BlocksComponents())
	}
	report, err := reconciler.Reconcile(t.Context())
	if err != nil || report != nil {
		t.Fatalf("Reconcile() = (%#v, %v), want (nil, nil): deferred use case must not act", report, err)
	}
	if deferred.acted {
		t.Fatalf("Act() was called for a use case whose prerequisites were not met")
	}
}

func TestSchedulePropagatesUnexpectedPrerequisiteError(t *testing.T) {
	prereqErr := errors.New("state read failed")
	reconciler := reconcilerFromUseCases([]string{"test"}, map[string]UseCase{
		"test": &fakeUseCase{prerequisiteErr: prereqErr},
	})

	if err := reconciler.Schedule(t.Context()); !errors.Is(err, prereqErr) {
		t.Fatalf("Schedule() error = %v, want %v", err, prereqErr)
	}
}

// reconcilerFromUseCases builds a reconciler over an explicit order from
// already-constructed use cases, wrapping each in a trivial factory. Test-only:
// production wiring supplies lazy factories via NewUseCaseReconciler — this
// helper exists so the behavioural tests above can drive
// Schedule/Reconcile/BlocksComponents with eager fakes and arbitrary
// names/order without that wrapping leaking into the production constructor.
func reconcilerFromUseCases(order []string, available map[string]UseCase) *Reconciler {
	registry := make(map[string]Factory, len(available))
	for name, useCase := range available {
		uc := useCase
		registry[name] = func() UseCase { return uc }
	}
	return &Reconciler{order: append([]string(nil), order...), factory: registry}
}

type fakeUseCase struct {
	scheduled       bool
	scheduleErr     error
	prerequisiteErr error
	components      []string
	report          reconciliationTypes.Report
	actErr          error
	acted           bool
}

func (f *fakeUseCase) Prerequisites(context.Context) error {
	return f.prerequisiteErr
}

func (f *fakeUseCase) Schedule(context.Context) (bool, error) {
	return f.scheduled, f.scheduleErr
}

func (f *fakeUseCase) BlocksComponents() []string {
	return f.components
}

func (f *fakeUseCase) Act(context.Context) (reconciliationTypes.Report, error) {
	f.acted = true
	return f.report, f.actErr
}
