// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package common

import (
	corev1 "k8s.io/api/core/v1"
)

// VolumeReferencesConfigMap reports whether a Volume sources data from the named ConfigMap —
// either via a direct ConfigMap volume or via any source inside a Projected volume.
func VolumeReferencesConfigMap(vol corev1.Volume, cmName string) bool {
	if vol.ConfigMap != nil && vol.ConfigMap.Name == cmName {
		return true
	}
	if vol.Projected != nil {
		for _, src := range vol.Projected.Sources {
			if src.ConfigMap != nil && src.ConfigMap.Name == cmName {
				return true
			}
		}
	}
	return false
}
