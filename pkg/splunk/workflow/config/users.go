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

package config

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	k8sclient "sigs.k8s.io/controller-runtime/pkg/client"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/common"
)

// UserPasswordSecretKey is the key in a user's Secret that holds the password.
const UserPasswordSecretKey = "password"

// ErrUserPasswordMissing is returned when a user's Secret has no password, or an empty one.
var ErrUserPasswordMissing = errors.New("password is missing or empty")

// IsUserDependencyError reports whether err is caused by a missing Secret or password,
// which only the Secret's owner can fix.
func IsUserDependencyError(err error) bool {
	return errors.Is(err, ErrUserPasswordMissing) || k8serrors.IsNotFound(err)
}

// ResolveUsers returns the splunk.users map, keyed by username, with each password read
// from the user's Secret. Errors never contain the password.
func ResolveUsers(ctx context.Context, reader k8sclient.Reader, namespace string, users []enterpriseApi.SplunkUser) (map[string]common.UserDefault, error) {
	if len(users) == 0 {
		return nil, nil
	}

	resolved := make(map[string]common.UserDefault, len(users))
	for _, user := range users {
		key := types.NamespacedName{Namespace: namespace, Name: user.PasswordSecretRef.Name}
		secret := &corev1.Secret{}
		if err := reader.Get(ctx, key, secret); err != nil {
			return nil, fmt.Errorf("user %q: get secret %q: %w", user.Name, key.Name, err)
		}
		password := secret.Data[UserPasswordSecretKey]
		if len(password) == 0 {
			return nil, fmt.Errorf("user %q: secret %q: %w", user.Name, key.Name, ErrUserPasswordMissing)
		}
		resolved[user.Name] = common.UserDefault{
			Password: string(password),
			Roles:    append([]string(nil), user.Roles...),
		}
	}
	return resolved, nil
}
