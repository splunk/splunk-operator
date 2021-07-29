// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

package client

import (
	"testing"
)

func TestRegisterS3Client(t *testing.T) {

	// clear any stale entries present in the S3clients map
	for k := range S3Clients {
		delete(S3Clients, k)
	}

	// 1. Test for aws
	RegisterS3Client("aws")
	if len(S3Clients) == 0 {
		t.Errorf("We should have initialized the client for aws.")
	}

	// 2. Test for minio
	RegisterS3Client("minio")
	if len(S3Clients) == 1 {
		t.Errorf("We should have initialized the client for minio as well.")
	}

	// 3. Test for invalid provider
	RegisterS3Client("invalid")
	if len(S3Clients) > 2 {
		t.Errorf("We should only have initialized the client for aws and minio and not for an invalid provider.")
	}

}

func TestGetSetS3ClientFuncPtr(t *testing.T) {
	c := &GetS3ClientWrapper{}

	fn := c.GetS3ClientFuncPtr()
	if fn != nil {
		t.Errorf("We should have received a nil function pointer")
	}

	c.SetS3ClientFuncPtr("aws", NewAWSS3Client)
	if c.GetS3Client == nil {
		t.Errorf("We should have set GetS3Client func pointer for AWS client.")
	}
}

func TestGetSetS3ClientInitFuncPtr(t *testing.T) {
	c := &GetS3ClientWrapper{}

	fn := c.GetS3ClientInitFuncPtr()
	if fn != nil {
		t.Errorf("We should have received a nil init function pointer")
	}

	c.SetS3ClientInitFuncPtr("aws", InitAWSClientWrapper)
	if c.GetInitFunc == nil {
		t.Errorf("We should have set GetInitFunc func pointer for AWS client.")
	}
}
