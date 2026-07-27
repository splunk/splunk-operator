// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package enterprise

import (
	"fmt"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/config"
)

const (
	DefaultTerminationGracePeriodSeconds int64 = 1200
	DefaultSearchDrainTimeoutSeconds     int64 = 180
	DefaultCaptainTransferTimeoutSeconds int64 = 180
	DefaultPodStartupTimeoutSeconds      int64 = 1800
	DefaultMemberRejoinTimeoutSeconds    int64 = 1800
)

// ResolvedSearchHeadClusterLifecyclePolicy contains the effective values used
// by lifecycle orchestration. It is deliberately separate from the stored API
// so omitted fields remain omitted.
type ResolvedSearchHeadClusterLifecyclePolicy struct {
	TerminationGracePeriodSeconds int64
	PodUpdateStrategy             enterpriseApi.SearchHeadClusterPodUpdateStrategy
	SearchDrainTimeoutSeconds     int64
	CaptainTransferTimeoutSeconds int64
	PodStartupTimeoutSeconds      int64
	MemberRejoinTimeoutSeconds    int64
}

// ResolveTerminationGracePeriodSeconds returns nil while SplunkPodLifecycle is
// disabled so callers preserve the existing Pod template. When enabled, it
// resolves the customer value or the spike default.
func ResolveTerminationGracePeriodSeconds(spec *enterpriseApi.CommonSplunkSpec) *int64 {
	if !config.DefaultMutableFeatureGate.Enabled(config.SplunkPodLifecycle) || spec == nil {
		return nil
	}
	value := DefaultTerminationGracePeriodSeconds
	if spec.TerminationGracePeriodSeconds != nil {
		value = *spec.TerminationGracePeriodSeconds
	}
	return &value
}

// ResolveSearchHeadClusterLifecyclePolicy resolves spike defaults only when
// both lifecycle feature gates are enabled.
func ResolveSearchHeadClusterLifecyclePolicy(spec *enterpriseApi.SearchHeadClusterSpec) (*ResolvedSearchHeadClusterLifecyclePolicy, error) {
	if spec == nil {
		return nil, fmt.Errorf("SearchHeadCluster spec is required")
	}
	if !config.DefaultMutableFeatureGate.Enabled(config.SearchHeadClusterLifecycle) {
		return nil, fmt.Errorf("%s is disabled", config.SearchHeadClusterLifecycle)
	}
	if !config.DefaultMutableFeatureGate.Enabled(config.SplunkPodLifecycle) {
		return nil, fmt.Errorf("%s requires %s=true", config.SearchHeadClusterLifecycle, config.SplunkPodLifecycle)
	}

	resolved := &ResolvedSearchHeadClusterLifecyclePolicy{
		PodUpdateStrategy:             enterpriseApi.SearchHeadClusterPodUpdateStrategyOnDelete,
		SearchDrainTimeoutSeconds:     DefaultSearchDrainTimeoutSeconds,
		CaptainTransferTimeoutSeconds: DefaultCaptainTransferTimeoutSeconds,
		PodStartupTimeoutSeconds:      DefaultPodStartupTimeoutSeconds,
		MemberRejoinTimeoutSeconds:    DefaultMemberRejoinTimeoutSeconds,
	}
	resolved.TerminationGracePeriodSeconds = *ResolveTerminationGracePeriodSeconds(&spec.CommonSplunkSpec)
	if spec.LifecyclePolicy == nil {
		return resolved, nil
	}

	policy := spec.LifecyclePolicy
	if policy.PodUpdateStrategy != "" {
		resolved.PodUpdateStrategy = policy.PodUpdateStrategy
	}
	if policy.SearchDrainTimeoutSeconds != nil {
		resolved.SearchDrainTimeoutSeconds = *policy.SearchDrainTimeoutSeconds
	}
	if policy.CaptainTransferTimeoutSeconds != nil {
		resolved.CaptainTransferTimeoutSeconds = *policy.CaptainTransferTimeoutSeconds
	}
	if policy.PodStartupTimeoutSeconds != nil {
		resolved.PodStartupTimeoutSeconds = *policy.PodStartupTimeoutSeconds
	}
	if policy.MemberRejoinTimeoutSeconds != nil {
		resolved.MemberRejoinTimeoutSeconds = *policy.MemberRejoinTimeoutSeconds
	}
	return resolved, nil
}
