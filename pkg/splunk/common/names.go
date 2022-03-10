// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

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

package common

import "fmt"

const (
	// namespace scoped secret name
	namespaceScopedSecretNameTemplateStr = "splunk-%s-secret"

	// versionedSecretIdentifier based secret name
	versionedSecretNameTemplateStr = "%s-secret-v%s"

	// FirstVersion represents the first version of versioned secrets
	FirstVersion = "1"

	// SecretBytes used to generate Splunk secrets
	SecretBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

	// HexBytes used to generate random hexadecimal strings (e.g. HEC tokens)
	HexBytes = "ABCDEF01234567890"

	// MinimumVersionedSecrets holds the minimum number of secrets to be held per version
	MinimumVersionedSecrets = 3

	// IdxcSecret represents indexer cluster pass4Symmkey secret token
	IdxcSecret = "idxc_secret"

	// PvcNamePrefix is a helper string representing prefix for persistent volume claim names
	PvcNamePrefix = "pvc-%s"

	// SplunkMountNamePrefix is a helper string representing Splunk Volume mount names
	SplunkMountNamePrefix = "mnt-splunk-%s"

	// SplunkMountDirecPrefix is a helper string representing Splunk Volume mount directory
	SplunkMountDirecPrefix = "/opt/splunk/%s"

	// EtcVolumeStorage indicates /opt/splunk/etc volume mounted on Pods
	EtcVolumeStorage = "etc"

	// VarVolumeStorage indicates /opt/splunk/etc volume mounted on Pods
	VarVolumeStorage = "var"

	// DefaultEtcVolumeStorageCapacity represents default storage capacity for etc volume
	DefaultEtcVolumeStorageCapacity = "10Gi"

	// DefaultVarVolumeStorageCapacity represents default storage capacity for var volume
	DefaultVarVolumeStorageCapacity = "100Gi"

	// SortFieldContainerPort represents field name ContainerPort for sorting
	SortFieldContainerPort = "ContainerPort"

	// SortFieldPort represents field name Port for sorting
	SortFieldPort = "Port"

	// SortFieldName represents field name Name for sorting
	SortFieldName = "Name"

	// SortFieldKey represents field name Key for sorting
	SortFieldKey = "Key"

	// Appframework specific polling intervals
	// Need to make sure to change the comment in
	// the spec section for AppFramework about the polling intervals.

	// DefaultAppsRepoPollInterval sets the polling interval to one hour
	DefaultAppsRepoPollInterval int64 = 60 * 60

	// MinAppsRepoPollInterval sets the polling interval to one minute
	MinAppsRepoPollInterval int64 = 60

	// MaxAppsRepoPollInterval sets the polling interval to one day
	MaxAppsRepoPollInterval int64 = 60 * 60 * 24
)

// GetVersionedSecretName returns a versioned secret name
func GetVersionedSecretName(versionedSecretIdentifier string, version string) string {
	return fmt.Sprintf(versionedSecretNameTemplateStr, versionedSecretIdentifier, version)
}

// GetNamespaceScopedSecretName gets namespace scoped secret name
func GetNamespaceScopedSecretName(namespace string) string {
	return fmt.Sprintf(namespaceScopedSecretNameTemplateStr, namespace)
}

// GetSplunkSecretTokenTypes returns all types of Splunk secret tokens
func GetSplunkSecretTokenTypes() []string {
	return []string{"hec_token", "password", "pass4SymmKey", "idxc_secret", "shc_secret"}
}

// GetLabelTypes returns a map of label types to strings
func GetLabelTypes() map[string]string {
	// Assigning each type of label to string
	return map[string]string{"manager": "app.kubernetes.io/managed-by",
		"component": "app.kubernetes.io/component",
		"name":      "app.kubernetes.io/name",
		"partof":    "app.kubernetes.io/part-of",
		"instance":  "app.kubernetes.io/instance",
	}
}
