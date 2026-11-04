// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	"context"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/k8sops"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig"
	"github.com/splunk/splunk-operator/pkg/splunk/splunkconfig/kvservice"
)

// ResolveKVServiceClientConfig resolves the CR's namespace enable/tenant labels and address
// annotation and builds its client entries. Reconcilers with other defaults can
// combine the returned entries before calling config.EnsureConfigMap.
func ResolveKVServiceClientConfig(ctx context.Context, c common.ControllerClient, cr common.MetaObject, role common.InstanceType) ([]common.ConfFileEntry, error) {
	settings, _, err := k8sops.GetKVServiceSettings(ctx, c, cr, role)
	if err != nil {
		return nil, err
	}
	set, err := splunkconfig.ForRole(role).
		NonSensitiveConfDir("").
		With(kvservice.Conf(settings.ConnectionString, settings.Tenant)).
		Build()
	return set.NonSensitive, err
}

// EnsureKVServiceDefaults prepares an immutable defaults ConfigMap using the
// shared configuration workflow. Reconcilers call it before StatefulSet
// construction, pass its mount option, and collect obsolete maps after apply.
// A supported consumer without KVService enabled receives explicit local defaults.
func EnsureKVServiceDefaults(ctx context.Context, c common.ControllerClient, cr common.MetaObject, role common.InstanceType) (resources.DefaultsConfigMap, error) {
	entries, err := ResolveKVServiceClientConfig(ctx, c, cr, role)
	if err != nil || len(entries) == 0 {
		return resources.DefaultsConfigMap{}, err
	}
	owner := common.AsOwner(cr, true)
	return EnsureConfigMap(ctx, c, cr, entries, &owner)
}
