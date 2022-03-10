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

/*
Package test includes common code used for testing other modules.
This package has no dependencies outside of the standard go and kubernetes libraries,
and the splunk.common package.
*/
package test

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// GetMockS3SecretKeys returns S3 secret keys
func GetMockS3SecretKeys(name string) corev1.Secret {
	accessKey := []byte{'1'}
	secretKey := []byte{'2'}

	// Create S3 secret
	s3Secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: "test",
		},
		Data: map[string][]byte{
			"s3_access_key": accessKey,
			"s3_secret_key": secretKey,
		},
	}
	return s3Secret
}
