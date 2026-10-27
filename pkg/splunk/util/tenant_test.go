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

package util

import (
	"testing"

	"github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestGetTenantName(t *testing.T) {
	tests := []struct {
		name           string
		namespace      *corev1.Namespace
		watchNamespace string
		want           string
		wantErr        bool
	}{
		{
			name:      "uses namespace name when label is absent and all namespaces are watched",
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-ns"}},
			want:      "tenant-ns",
		},
		{
			name:           "uses tenant label when namespace is watched",
			watchNamespace: "other,tenant-ns",
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   "tenant-ns",
				Labels: map[string]string{common.LabelTenant: "sok-tenant"},
			}},
			want: "sok-tenant",
		},
		{
			name:           "falls back to namespace name when tenant label is empty",
			watchNamespace: "tenant-ns",
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:   "tenant-ns",
				Labels: map[string]string{common.LabelTenant: ""},
			}},
			want: "tenant-ns",
		},
		{
			name:           "returns error for namespace outside watch scope",
			watchNamespace: "other,another",
			namespace:      &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "tenant-ns"}},
			wantErr:        true,
		},
		{
			name:    "returns error for nil namespace",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("WATCH_NAMESPACE", tt.watchNamespace)
			got, err := GetTenantName(tt.namespace)
			if (err != nil) != tt.wantErr {
				t.Fatalf("GetTenantName() error = %v, wantErr %t", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("GetTenantName() = %q, want %q", got, tt.want)
			}
		})
	}
}
