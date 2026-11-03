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
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestIngestorClusterUsersValidation(t *testing.T) {
	apiClient := requireAPIServer(t)

	tests := []struct {
		name    string
		users   []SplunkUser
		wantErr string
	}{
		{
			name: "accepts valid users",
			users: []SplunkUser{
				{
					Name:              "internal_observability",
					Roles:             []string{"observability_role"},
					PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
				},
				{
					Name:              "internal_automation",
					Roles:             []string{"automation_role", "user"},
					PasswordSecretRef: corev1.LocalObjectReference{Name: "automation-user"},
				},
			},
		},
		{
			name: "rejects admin",
			users: []SplunkUser{{
				Name:              "admin",
				Roles:             []string{"user"},
				PasswordSecretRef: corev1.LocalObjectReference{Name: "admin-user"},
			}},
			wantErr: "spec.users[0]: Invalid value: name must not be admin",
		},
		{
			name: "rejects uppercase names",
			users: []SplunkUser{{
				Name:              "Wibble",
				Roles:             []string{"user"},
				PasswordSecretRef: corev1.LocalObjectReference{Name: "wibble-user"},
			}},
			wantErr: `spec.users[0].name: Invalid value: "Wibble": spec.users[0].name in body should match '^[a-z0-9_-]+$'`,
		},
		{
			name: "rejects email-style names",
			users: []SplunkUser{{
				Name:              "svc@example",
				Roles:             []string{"user"},
				PasswordSecretRef: corev1.LocalObjectReference{Name: "svc-user"},
			}},
			wantErr: `spec.users[0].name: Invalid value: "svc@example": spec.users[0].name in body should match '^[a-z0-9_-]+$'`,
		},
		{
			name: "rejects dotted names",
			users: []SplunkUser{{
				Name:              "internal.observability",
				Roles:             []string{"user"},
				PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
			}},
			wantErr: `spec.users[0].name: Invalid value: "internal.observability": spec.users[0].name in body should match '^[a-z0-9_-]+$'`,
		},
		{
			name: "rejects duplicate names",
			users: []SplunkUser{
				{
					Name:              "internal_observability",
					Roles:             []string{"user"},
					PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
				},
				{
					Name:              "internal_observability",
					Roles:             []string{"user"},
					PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
				},
			},
			wantErr: `spec.users[1]: Duplicate value: {"name":"internal_observability"}`,
		},
		{
			name: "rejects empty roles",
			users: []SplunkUser{{
				Name:              "internal_observability",
				Roles:             []string{},
				PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
			}},
			wantErr: "spec.users[0].roles: Invalid value: 0: spec.users[0].roles in body should have at least 1 items",
		},
		{
			name: "rejects an empty role",
			users: []SplunkUser{{
				Name:              "internal_observability",
				Roles:             []string{""},
				PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
			}},
			wantErr: `spec.users[0].roles[0]: Invalid value: "": spec.users[0].roles[0] in body should be at least 1 chars long`,
		},
		{
			name: "rejects duplicate roles",
			users: []SplunkUser{{
				Name:              "internal_observability",
				Roles:             []string{"user", "user"},
				PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
			}},
			wantErr: `spec.users[0].roles[1]: Duplicate value: "user"`,
		},
		{
			name: "accepts an empty passwordSecretRef.name (rejected by the webhook)",
			users: []SplunkUser{{
				Name:              "internal_observability",
				Roles:             []string{"user"},
				PasswordSecretRef: corev1.LocalObjectReference{Name: ""},
			}},
		},
		{
			name: "accepts a non DNS-1123 passwordSecretRef.name (rejected by the webhook)",
			users: []SplunkUser{{
				Name:              "internal_observability",
				Roles:             []string{"user"},
				PasswordSecretRef: corev1.LocalObjectReference{Name: "Observability_User"},
			}},
		},
	}

	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := fmt.Sprintf("ing-users-%d", i)
			ingestorCluster := &IngestorCluster{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
				Spec: IngestorClusterSpec{
					Replicas:         1,
					QueueRef:         corev1.ObjectReference{Name: "queue"},
					ObjectStorageRef: corev1.ObjectReference{Name: "os"},
					Users:            tt.users,
				},
			}
			err := apiClient.Create(t.Context(), ingestorCluster)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.EqualError(t, err, fmt.Sprintf("IngestorCluster.enterprise.splunk.com %q is invalid: %s", name, tt.wantErr))
		})
	}
}
