// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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
