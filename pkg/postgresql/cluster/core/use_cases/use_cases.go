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

	enterprisev4 "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	majorversionupgradetypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	reconciliationTypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/reconciliation"
	mvupredicate "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/use_cases/major_version_upgrade/predicate"
)

// ErrPrerequisiteNotReady marks a dependency that is expected to become
// available during normal reconciliation. Other prerequisite errors indicate
// a read or configuration failure and must be propagated.
var ErrPrerequisiteNotReady = errors.New("use case prerequisite is not ready")

type UseCase interface {
	Prerequisites(context.Context) error
	Schedule(context.Context) (bool, error)
	BlocksComponents() []string
	Act(context.Context) (reconciliationTypes.Report, error)
}

type Factory func() UseCase

// Predicate checks whether a usecase has been triggered by the user, no i/o, pure.
type Predicate func(spec *enterprisev4.PostgresClusterSpec) bool

type Reconciler struct {
	// building blocks
	factory   map[string]Factory
	predicate map[string]Predicate

	// runtime
	order     []string
	scheduled map[string]UseCase

	// misc
	spec *enterprisev4.PostgresClusterSpec
	// cache of produced usecases to have uniform
	// data across Schedule/Reconcile.
	cache map[string]UseCase
}

// Not a map, order/priority can matter here
var defaultUseCases = []struct {
	name      string
	predicate Predicate
}{
	{majorversionupgradetypes.UseCaseName, mvupredicate.Predicate},
}

func NewUseCaseReconciler(spec *enterprisev4.PostgresClusterSpec, factory map[string]Factory) *Reconciler {
	order := make([]string, 0, len(defaultUseCases))
	predicate := make(map[string]Predicate, len(defaultUseCases))
	for _, uc := range defaultUseCases {
		order = append(order, uc.name)
		predicate[uc.name] = uc.predicate
	}
	return &Reconciler{order: order, predicate: predicate, spec: spec, factory: factory}
}

// checks cache for previous entries, if not
// check the minimum construction condition for usecase(predicate)
// constructs the usecase using the provided factory method and stores
func (r *Reconciler) get(name string) UseCase {
	if uc, ok := r.cache[name]; ok {
		return uc
	}
	if p, ok := r.predicate[name]; ok && !p(r.spec) {
		return nil
	}
	factory, ok := r.factory[name]
	if !ok || factory == nil {
		return nil
	}
	uc := factory()
	if uc == nil {
		return nil
	}
	if r.cache == nil {
		r.cache = make(map[string]UseCase, len(r.factory))
	}
	r.cache[name] = uc
	return uc
}

func (r *Reconciler) Schedule(ctx context.Context) error {
	r.scheduled = make(map[string]UseCase, len(r.factory))

	for _, name := range r.order {
		useCase := r.get(name)
		if useCase == nil {
			continue
		}
		// Only expected not-ready states are deferred. Read or configuration
		// failures must escape so controller-runtime can requeue the resource.
		if err := useCase.Prerequisites(ctx); err != nil {
			if errors.Is(err, ErrPrerequisiteNotReady) {
				continue
			}
			return err
		}
		enabled, err := useCase.Schedule(ctx)
		if err != nil {
			return err
		}
		if enabled {
			logging.FromContext(ctx).InfoContext(ctx, "use case scheduled", "name", name)
			r.scheduled[name] = useCase
		}
	}
	return nil
}

func (r *Reconciler) BlocksComponents() map[string]struct{} {
	blocked := map[string]struct{}{}
	for _, useCase := range r.scheduled {
		for _, component := range useCase.BlocksComponents() {
			blocked[component] = struct{}{}
		}
	}
	return blocked
}

func (r *Reconciler) CheckPrerequisites(ctx context.Context) error {
	for _, name := range r.order {
		useCase := r.get(name)
		if useCase == nil {
			continue
		}
		if err := useCase.Prerequisites(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *Reconciler) Reconcile(ctx context.Context) (*reconciliationTypes.Report, error) {
	logger := logging.FromContext(ctx)
	if r.scheduled == nil {
		if err := r.Schedule(ctx); err != nil {
			return nil, err
		}
	}

	for _, name := range r.order {
		useCase := r.scheduled[name]
		if useCase == nil {
			continue
		}
		logger.InfoContext(ctx, "use case reconciling", "name", name)
		report, err := useCase.Act(ctx)
		if err != nil || report.Retry {
			return &report, err
		}
		logger.InfoContext(ctx, "use case completed", "name", name)
		return nil, nil
	}
	return nil, nil
}
