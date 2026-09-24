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

// Package cnpgmonitoring translates provider-neutral queries for CNPG.
package cnpgmonitoring

import (
	"context"
	"fmt"

	cnpginfra "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Adapter struct {
	client client.Client
	scheme *runtime.Scheme
	target monitoring.Target
}

func New(c client.Client, scheme *runtime.Scheme, target monitoring.Target) *Adapter {
	return &Adapter{client: c, scheme: scheme, target: target}
}

// An empty configuration removes only this feature's ConfigMap and selector.
func (a *Adapter) Apply(ctx context.Context, cfg monitoring.AggregatedConfig) (monitoring.ExpectedState, error) {
	cnpgCluster, err := cnpginfra.GetMonitoringProviderOwner(
		ctx,
		a.client,
		a.target.Namespace,
		a.target.ProviderName,
	)
	if err != nil {
		return monitoring.ExpectedState{}, err
	}
	entries, err := toEntries(cfg)
	if err != nil {
		return monitoring.ExpectedState{}, err
	}
	y, err := cnpginfra.SerializeEntries(entries)
	if err != nil {
		return monitoring.ExpectedState{}, err
	}
	desired := cnpginfra.BuildMonitoringConfig(cnpgCluster.Name, y)
	_, err = cnpginfra.ApplyMonitoringConfig(ctx, a.client, a.scheme, cnpgCluster, desired)
	if err != nil {
		return monitoring.ExpectedState{}, err
	}
	return monitoring.ExpectedState{
		Revision:   desired.Hash,
		Enabled:    y != "",
		QueryCount: queryCount(cfg),
	}, nil
}

func (a *Adapter) Observe(ctx context.Context, expected monitoring.ExpectedState) (monitoring.Observation, error) {
	cnpgCluster, err := cnpginfra.GetMonitoringProviderOwner(
		ctx,
		a.client,
		a.target.Namespace,
		a.target.ProviderName,
	)
	if err != nil {
		return monitoring.Observation{}, err
	}
	ready, message, err := cnpginfra.ObserveMonitoringConfig(
		ctx,
		a.client,
		cnpgCluster,
		expected.Revision,
		expected.Enabled,
	)
	if err != nil {
		return monitoring.Observation{}, err
	}
	if !ready {
		return monitoring.Observation{State: monitoring.ObservationPending, Message: message}, nil
	}
	return monitoring.Observation{
		State: monitoring.ObservationReady,
		Confirmed: &monitoring.ConfirmedState{
			Revision:   expected.Revision,
			Enabled:    expected.Enabled,
			QueryCount: expected.QueryCount,
		},
	}, nil
}

func (a *Adapter) Save(ctx context.Context, confirmed monitoring.ConfirmedState) (monitoring.SaveResult, error) {
	featureOwner, err := cnpginfra.GetMonitoringFeatureOwner(
		ctx,
		a.client,
		a.target.Namespace,
		a.target.FeatureName,
		a.target.FeatureUID,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return monitoring.SaveResult{}, fmt.Errorf("%w: %w", monitoring.ErrConfirmedResourceUnavailable, err)
		}
		return monitoring.SaveResult{}, err
	}
	if !confirmed.Enabled {
		changed, err := cnpginfra.DeleteMonitoringSnapshot(
			ctx,
			a.client,
			featureOwner.Object,
			featureOwner.APIVersion,
			featureOwner.Kind,
			a.target.ProviderName,
		)
		return monitoring.SaveResult{Changed: changed}, err
	}
	providerOwner, err := cnpginfra.GetMonitoringProviderOwner(
		ctx,
		a.client,
		a.target.Namespace,
		a.target.ProviderName,
	)
	if err != nil {
		return monitoring.SaveResult{}, err
	}
	snapshot, err := cnpginfra.ReadMonitoringSnapshot(
		ctx,
		a.client,
		providerOwner,
		confirmed.Revision,
		confirmed.QueryCount,
	)
	if err != nil {
		return monitoring.SaveResult{}, err
	}
	changed, err := cnpginfra.SaveMonitoringSnapshot(
		ctx,
		a.client,
		a.scheme,
		featureOwner.Object,
		featureOwner.APIVersion,
		featureOwner.Kind,
		a.target.ProviderName,
		snapshot,
	)
	return monitoring.SaveResult{Changed: changed}, err
}

func (a *Adapter) Rollback(ctx context.Context) (monitoring.RollbackResult, error) {
	featureOwner, err := cnpginfra.GetMonitoringFeatureOwner(
		ctx,
		a.client,
		a.target.Namespace,
		a.target.FeatureName,
		a.target.FeatureUID,
	)
	if err != nil {
		return monitoring.RollbackResult{}, err
	}
	snapshot, found, message, err := cnpginfra.LoadMonitoringSnapshot(
		ctx,
		a.client,
		featureOwner.Object,
		featureOwner.APIVersion,
		featureOwner.Kind,
		a.target.ProviderName,
	)
	if err != nil || !found {
		return monitoring.RollbackResult{Available: found, Message: message}, err
	}
	providerOwner, err := cnpginfra.GetMonitoringProviderOwner(
		ctx,
		a.client,
		a.target.Namespace,
		a.target.ProviderName,
	)
	if err != nil {
		return monitoring.RollbackResult{}, err
	}
	changed, err := cnpginfra.ApplyMonitoringConfig(
		ctx,
		a.client,
		a.scheme,
		providerOwner,
		cnpginfra.BuildMonitoringConfig(providerOwner.Name, snapshot.YAML),
	)
	if err != nil {
		return monitoring.RollbackResult{}, err
	}
	return monitoring.RollbackResult{
		Available: true,
		Expected: monitoring.ExpectedState{
			Revision:   snapshot.Hash,
			Enabled:    true,
			QueryCount: snapshot.QueryCount,
		},
		Changed: changed,
	}, nil
}

func queryCount(cfg monitoring.AggregatedConfig) int {
	count := len(cfg.ClusterQueries)
	for _, queries := range cfg.DatabaseQueries {
		count += len(queries)
	}
	return count
}
