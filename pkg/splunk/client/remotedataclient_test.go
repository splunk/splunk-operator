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

package client

import (
	"context"
	"testing"
)

func TestRegisterRemoteDataClient(t *testing.T) {
	ctx := context.TODO()
	// clear any stale entries present in the RemoteDataClientsMap map
	for k := range RemoteDataClientsMap {
		delete(RemoteDataClientsMap, k)
	}

	// 1. Test for aws
	RegisterRemoteDataClient(ctx, "aws")
	if len(RemoteDataClientsMap) == 0 {
		t.Errorf("We should have initialized the client for aws.")
	}

	// 2. Test for minio
	RegisterRemoteDataClient(ctx, "minio")
	if len(RemoteDataClientsMap) == 1 {
		t.Errorf("We should have initialized the client for minio as well.")
	}

	// 3. Test for azure
	RegisterRemoteDataClient(ctx, "azure")
	if len(RemoteDataClientsMap) == 1 {
		t.Errorf("We should have initialized the client for azure as well.")
	}

	// 3. Test for invalid provider
	RegisterRemoteDataClient(ctx, "invalid")
	if len(RemoteDataClientsMap) > 3 {
		t.Errorf("We should only have initialized the client for aws, minio and azure but not for an invalid provider.")
	}

}

func TestGetSetRemoteDataClientFuncPtr(t *testing.T) {
	c := &GetRemoteDataClientWrapper{}
	ctx := context.TODO()

	fn := c.GetRemoteDataClientFuncPtr(ctx)
	if fn != nil {
		t.Errorf("We should have received a nil function pointer")
	}

	c.SetRemoteDataClientFuncPtr(ctx, "aws", NewAWSS3Client)
	if c.GetRemoteDataClient == nil {
		t.Errorf("We should have set GetRemoteDataClient func pointer for AWS client.")
	}
}

func TestGetSetRemoteDataClientInitFuncPtr(t *testing.T) {
	ctx := context.TODO()
	c := &GetRemoteDataClientWrapper{}

	fn := c.GetRemoteDataClientInitFuncPtr(ctx)
	if fn != nil {
		t.Errorf("We should have received a nil init function pointer")
	}

	c.SetRemoteDataClientInitFuncPtr(ctx, "aws", InitAWSClientWrapper)
	if c.GetInitFunc == nil {
		t.Errorf("We should have set GetInitFunc func pointer for AWS client.")
	}
}
