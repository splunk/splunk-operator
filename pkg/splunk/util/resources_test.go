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

package util

import (
	"testing"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestEffectiveResourcesDoesNotMutateSpec(t *testing.T) {
	resources := corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU: resource.MustParse("250m"),
		},
	}

	effective := EffectiveResources(resources, false, SplunkDefaultResources())

	require.Equal(t, "250m", effective.Requests.Cpu().String())
	require.Equal(t, splcommon.DefaultRequestsMemory, effective.Requests.Memory().String())
	require.Equal(t, splcommon.DefaultLimitsCPU, effective.Limits.Cpu().String())
	require.Equal(t, splcommon.DefaultLimitsMemory, effective.Limits.Memory().String())
	require.Len(t, resources.Requests, 1)
	require.Nil(t, resources.Limits)
}
