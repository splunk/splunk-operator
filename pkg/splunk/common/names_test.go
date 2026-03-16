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

import (
	"reflect"
	"testing"
)

func TestGetVersionedSecretName(t *testing.T) {
	versionedSecretIdentifier := "splunk-test"
	version := FirstVersion
	secretName := GetVersionedSecretName(versionedSecretIdentifier, version)
	wantSecretName := "splunk-test-secret-v1"
	if secretName != wantSecretName {
		t.Errorf("Incorrect versioned secret name got %s want %s", secretName, wantSecretName)
	}
}

func TestGetNamespaceScopedSecretName(t *testing.T) {
	namespace := "test"
	gotName := GetNamespaceScopedSecretName(namespace)
	wantName := "splunk-test-secret"
	if gotName != wantName {
		t.Errorf("Incorrect namespace scoped secret name got %s want %s", gotName, wantName)
	}
}

func TestGetSplunkSecretTokenTypes(t *testing.T) {
	wantSecretTokens := []string{"hec_token", "password", "pass4SymmKey", "idxc_secret", "shc_secret"}
	secretTokens := GetSplunkSecretTokenTypes()
	if !reflect.DeepEqual(secretTokens, wantSecretTokens) {
		t.Errorf("Incorrect secret tokens returned got %+v want %+v", secretTokens, wantSecretTokens)
	}
}

func TestGetLabelTypes(t *testing.T) {
	// Assigning each type of label to string
	wantLabelTypeMap := map[string]string{"manager": "app.kubernetes.io/managed-by",
		"component": "app.kubernetes.io/component",
		"name":      "app.kubernetes.io/name",
		"partof":    "app.kubernetes.io/part-of",
		"instance":  "app.kubernetes.io/instance",
	}

	gotLabelTypeMap := GetLabelTypes()
	if !reflect.DeepEqual(gotLabelTypeMap, wantLabelTypeMap) {
		t.Errorf("Incorrect label map got %+v want %+v", gotLabelTypeMap, wantLabelTypeMap)
	}
}
