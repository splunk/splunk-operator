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

package kvservice

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	"github.com/splunk/splunk-operator/pkg/splunk/resources"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

type tenantKeys struct{ current, previous string }

func parseTenantKeys(raw []byte) (tenantKeys, error) {
	if len(raw) == 0 {
		return tenantKeys{}, nil
	}
	var values []string
	if err := json.Unmarshal(raw, &values); err != nil {
		return tenantKeys{}, fmt.Errorf("invalid keyring entry: %w", err)
	}
	if len(values) == 0 || len(values) > 2 || slices.Contains(values, "") {
		return tenantKeys{}, fmt.Errorf("invalid keyring entry")
	}
	keys := tenantKeys{current: values[0]}
	if len(values) == 2 && values[1] != keys.current {
		keys.previous = values[1]
	}
	return keys, nil
}

func (keys tenantKeys) rotate(next string) (tenantKeys, error) {
	if next == "" {
		return keys, fmt.Errorf("new verification key must not be empty")
	}
	if keys.current == "" {
		return tenantKeys{current: next}, nil
	}
	if next == keys.current {
		return keys, nil
	}
	if keys.previous != "" {
		return keys, fmt.Errorf("has an unretired previous key; retire it before rotating again")
	}
	return tenantKeys{current: next, previous: keys.current}, nil
}

func (keys tenantKeys) values() []string {
	if keys.previous != "" {
		return []string{keys.current, keys.previous}
	}
	return []string{keys.current}
}

// validateTenantKeyEntries checks per-tenant keyring structure, including entries
// retained while a source Secret is unavailable. Key values may be shared across
// tenants; each tenant keeps its own current/previous rotation state.
func validateTenantKeyEntries(data map[string][]byte) error {
	for _, tenant := range slices.Sorted(maps.Keys(data)) {
		keys, err := parseTenantKeys(data[tenant])
		if err != nil || keys.current == "" {
			if err == nil {
				err = fmt.Errorf("no current verification key for tenant %q", tenant)
			}
			return tenantTerminal(
				fmt.Sprintf("Tenant %q has an invalid keyring entry. Correct its generated verification-key Secret entry to contain a current key and at most one previous key.", tenant),
				fmt.Errorf("validate verification-key entry for tenant %q: %w", tenant, err))
		}
	}
	return nil
}

// reconcileKVServiceTenantKeys computes one publication from all source snapshots.
// Missing/unreadable sources retain their last entry; removals always disappear.
// Malformed retained entries fall back to revocation-only publication.
func reconcileKVServiceTenantKeys(ctx context.Context, c splcommon.ControllerClient, cr *enterpriseApi.KVService, mapping map[string]string) (*corev1.Secret, error) {
	keyring := &corev1.Secret{}
	err := c.Get(ctx, types.NamespacedName{Namespace: cr.Namespace, Name: splcommon.KVServiceTenantVerificationKeysName}, keyring)
	create := apierrors.IsNotFound(err)
	if err != nil && !create {
		return nil, fmt.Errorf("get tenant verification-key Secret %s/%s: %w", cr.Namespace, splcommon.KVServiceTenantVerificationKeysName, err)
	}
	if create {
		keyring = resources.NewKVServiceTenantKeyring(cr)
	} else if err := validateTenantKeyringOwner(cr, keyring); err != nil {
		return nil, fmt.Errorf("validate tenant keyring ownership: %w", err)
	}

	retained := make(map[string][]byte, len(mapping))
	desired := make(map[string][]byte, len(mapping))
	var failures []error
	for _, tenant := range slices.Sorted(maps.Keys(mapping)) {
		raw, exists := keyring.Data[tenant]
		if exists {
			retained[tenant] = raw
			desired[tenant] = raw
		}
		source := &corev1.Secret{}
		namespace := mapping[tenant]
		if err := c.Get(ctx, types.NamespacedName{Namespace: namespace, Name: splcommon.GetNamespaceScopedSecretName(namespace)}, source); err != nil {
			failures = append(failures, fmt.Errorf("read namespace authentication Secret %s/%s for tenant %q: %w", namespace, splcommon.GetNamespaceScopedSecretName(namespace), tenant, err))
			continue
		}
		keys, err := parseTenantKeys(raw)
		if err != nil {
			failures = append(failures, tenantTerminal(
				fmt.Sprintf("Tenant %q has an invalid keyring entry in Secret %s/%s. Set the entry to a JSON array with the current key and at most one previous key.", tenant, cr.Namespace, keyring.Name),
				fmt.Errorf("parse verification-key entry for tenant %q: %w", tenant, err)))
			continue
		}
		next := string(source.Data["pass4SymmKey"])
		if next == "" {
			failures = append(failures, tenantTerminal(
				fmt.Sprintf("Set a non-empty pass4SymmKey in namespace Secret %s/%s for tenant %q.", namespace, source.Name, tenant),
				fmt.Errorf("namespace authentication Secret %s/%s for tenant %q has no non-empty pass4SymmKey", namespace, source.Name, tenant)))
			continue
		}
		keys, err = keys.rotate(next)
		if err != nil {
			failures = append(failures, tenantTerminal(
				fmt.Sprintf("Tenant %q has an unretired previous key. Confirm clients use the current key, then remove the previous entry in Secret %s/%s before rotating again.", tenant, cr.Namespace, keyring.Name),
				fmt.Errorf("rotate verification keys for tenant %q: %w", tenant, err)))
			continue
		}
		value, err := json.Marshal(keys.values())
		if err != nil {
			failures = append(failures, fmt.Errorf("encode keyring entry for tenant %q: %w", tenant, err))
			continue
		}
		desired[tenant] = value
	}
	if err := validateTenantKeyEntries(desired); err != nil {
		failures = append(failures, fmt.Errorf("validate tenant keyring entries: %w", err))
		desired = retained // Still revoke removed tenants without replacing malformed retained entries.
	}
	collected := errors.Join(failures...)
	if create || !maps.EqualFunc(keyring.Data, desired, bytes.Equal) {
		keyring.Data = desired
		if create {
			err = c.Create(ctx, keyring)
		} else {
			err = c.Update(ctx, keyring)
		}
		if apierrors.IsAlreadyExists(err) {
			return nil, &tenantReservationError{"waiting for namespace keyring reservation"}
		}
		if err != nil {
			// Publication has not succeeded. Source validation errors must not turn a
			// write failure into a terminal result and prevent another safety attempt.
			if collected != nil {
				return nil, fmt.Errorf("publish tenant keyring (%v): %w", collected, err)
			}
			return nil, fmt.Errorf("publish tenant keyring: %w", err)
		}
	}
	if collected != nil {
		return keyring, fmt.Errorf("reconcile tenant keyring entries: %w", collected)
	}
	return keyring, nil
}

// A joined NotFound plus an API or validation failure must not become a generic
// Pending result that hides the other error. Only all-missing sources qualify.
func onlyMissingTenantSecrets(err error) bool {
	if err == nil {
		return false
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		children := joined.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !onlyMissingTenantSecrets(child) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return onlyMissingTenantSecrets(wrapped.Unwrap())
	}
	return apierrors.IsNotFound(err)
}
