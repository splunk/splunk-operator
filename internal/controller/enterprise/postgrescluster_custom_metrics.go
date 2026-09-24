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

package controller

import (
	"fmt"

	monadapter "github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/custom_metrics"
	cnpgmonitoring "github.com/splunk/splunk-operator/pkg/postgresql/cluster/adapter/custom_metrics/cnpg"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	mon "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/custom_metrics"
	monitoring "github.com/splunk/splunk-operator/pkg/postgresql/shared/types/monitoring"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const cnpgProvisioner = "postgresql.cnpg.io"

func newCustomMetricsFactory(c client.Client, scheme *runtime.Scheme) clustercore.CustomMetricsFactory {
	return func(provisioner string, target monitoring.Target) (*mon.Model, error) {
		return newCustomMetricsModel(provisioner, c, scheme, target)
	}
}

func newCustomMetricsModel(
	provisioner string,
	c client.Client,
	scheme *runtime.Scheme,
	target monitoring.Target,
) (*mon.Model, error) {
	var provider mon.Provisioner
	switch provisioner {
	case cnpgProvisioner:
		provider = cnpgmonitoring.New(c, scheme, target)
	default:
		return nil, fmt.Errorf("custom metrics: unsupported provisioner %q", provisioner)
	}

	return mon.NewModel(
		monadapter.NewDataRepository(c),
		monadapter.NewParser(),
		monadapter.NewCollider(cnpgmonitoring.RenderIdentity),
		monadapter.NewAggregator(),
		provider,
	), nil
}
