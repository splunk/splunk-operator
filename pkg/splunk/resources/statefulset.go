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

package resources

import appsv1 "k8s.io/api/apps/v1"

// StatefulSetOption mutates a StatefulSet after it has been constructed.
type StatefulSetOption func(*appsv1.StatefulSet)

// ApplyStatefulSetOptions applies each option to ss in order.
func ApplyStatefulSetOptions(ss *appsv1.StatefulSet, opts ...StatefulSetOption) {
	for _, opt := range opts {
		opt(ss)
	}
}
