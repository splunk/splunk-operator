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

const (
	// InvalidSecretObjectError represents an invalid secret object error
	InvalidSecretObjectError = "invalid secret object"

	// PodNotFoundError indicates Pod is not found
	PodNotFoundError = "couldn't find pod"

	// PodSecretNotFoundError indicates that a mounted secret wasn't found on the Pod
	PodSecretNotFoundError = "couldn't find secret in Pod %s"

	// SecretNotFoundError indicates Pod is not found
	SecretNotFoundError = "couldn't find secret"

	// SecretTokenNotRetrievable indicates missing secret token in pod secret
	SecretTokenNotRetrievable = "couldn't retrieve %s from secret data"

	// EmptyClusterManagerRef indicates an empty cluster manager reference
	EmptyClusterManagerRef = "empty cluster manager reference"
)
