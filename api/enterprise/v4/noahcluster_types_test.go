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

package v4

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
)

func TestNoahEnabledUsesReferencePresence(t *testing.T) {
	tests := []struct {
		name string
		spec interface{ NoahEnabled() bool }
		want bool
	}{
		{name: "nil indexer spec", spec: (*IndexerClusterSpec)(nil)},
		{name: "classic indexer", spec: &IndexerClusterSpec{}},
		{name: "empty indexer reference", spec: &IndexerClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{}}, want: true},
		{name: "named indexer reference", spec: &IndexerClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}}, want: true},
		{name: "nil search head spec", spec: (*SearchHeadClusterSpec)(nil)},
		{name: "classic search head", spec: &SearchHeadClusterSpec{}},
		{name: "empty search head reference", spec: &SearchHeadClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{}}, want: true},
		{name: "named search head reference", spec: &SearchHeadClusterSpec{NoahClusterRef: &corev1.LocalObjectReference{Name: "noah"}}, want: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, test.spec.NoahEnabled())
		})
	}
}
