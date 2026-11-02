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

package config_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
	configworkflow "github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
)

func TestResolveUsers_BuildsMapKeyedByName(t *testing.T) {
	c := fakeClient(
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "observability-user", Namespace: "ns"},
			Data:       map[string][]byte{"password": []byte("observability-pw")},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "automation-user", Namespace: "ns"},
			Data:       map[string][]byte{"password": []byte("automation-pw")},
		},
	)

	users, err := configworkflow.ResolveUsers(context.Background(), c, "ns", []enterpriseApi.SplunkUser{
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
	})
	require.NoError(t, err)
	assert.Equal(t, map[string]common.UserDefault{
		"internal_observability": {Password: "observability-pw", Roles: []string{"observability_role"}},
		"internal_automation":    {Password: "automation-pw", Roles: []string{"automation_role", "user"}},
	}, users)
}

func TestResolveUsers_NoUsers(t *testing.T) {
	users, err := configworkflow.ResolveUsers(context.Background(), fakeClient(), "ns", nil)
	require.NoError(t, err)
	assert.Nil(t, users)
}

func TestResolveUsers_MissingSecret(t *testing.T) {
	_, err := configworkflow.ResolveUsers(context.Background(), fakeClient(), "ns", []enterpriseApi.SplunkUser{
		{
			Name:              "internal_observability",
			Roles:             []string{"observability_role"},
			PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
		},
	})
	assert.EqualError(t, err, `user "internal_observability": get secret "observability-user": secrets "observability-user" not found`)
	assert.True(t, configworkflow.IsUserDependencyError(err))
}

func TestResolveUsers_SecretInOtherNamespaceIsNotRead(t *testing.T) {
	other := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "observability-user", Namespace: "ns"},
		Data:       map[string][]byte{"password": []byte("observability-pw")},
	}
	other.Namespace = "other"

	_, err := configworkflow.ResolveUsers(context.Background(), fakeClient(other), "ns", []enterpriseApi.SplunkUser{
		{
			Name:              "internal_observability",
			Roles:             []string{"observability_role"},
			PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
		},
	})
	assert.EqualError(t, err, `user "internal_observability": get secret "observability-user": secrets "observability-user" not found`)
	assert.True(t, configworkflow.IsUserDependencyError(err))
}

func TestResolveUsers_MissingOrEmptyPasswordKey(t *testing.T) {
	for name, data := range map[string]map[string][]byte{
		"missing key": {"passwd": []byte("misplaced-pw")},
		"empty value": {"password": {}, "other": []byte("misplaced-pw")},
	} {
		t.Run(name, func(t *testing.T) {
			c := fakeClient(&corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "observability-user", Namespace: "ns"},
				Data:       data,
			})
			_, err := configworkflow.ResolveUsers(context.Background(), c, "ns", []enterpriseApi.SplunkUser{
				{
					Name:              "internal_observability",
					Roles:             []string{"observability_role"},
					PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
				},
			})
			assert.EqualError(t, err, `user "internal_observability": secret "observability-user": password is missing or empty`)
			assert.ErrorIs(t, err, configworkflow.ErrUserPasswordMissing)
			assert.True(t, configworkflow.IsUserDependencyError(err))
		})
	}
}

func TestResolveUsers_DoesNotModifyUserSecret(t *testing.T) {
	c := fakeClient(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "observability-user", Namespace: "ns"},
		Data:       map[string][]byte{"password": []byte("observability-pw")},
	})
	before := &corev1.Secret{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "observability-user"}, before))

	_, err := configworkflow.ResolveUsers(context.Background(), c, "ns", []enterpriseApi.SplunkUser{
		{
			Name:              "internal_observability",
			Roles:             []string{"observability_role"},
			PasswordSecretRef: corev1.LocalObjectReference{Name: "observability-user"},
		},
	})
	require.NoError(t, err)

	after := &corev1.Secret{}
	require.NoError(t, c.Get(context.Background(), client.ObjectKey{Namespace: "ns", Name: "observability-user"}, after))
	assert.Equal(t, before.ResourceVersion, after.ResourceVersion)
	assert.Empty(t, after.OwnerReferences)
}

func TestIsUserDependencyError_TransientErrorIsNot(t *testing.T) {
	assert.False(t, configworkflow.IsUserDependencyError(errors.New("connection refused")))
}
