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

package storage_test

import (
	"context"
	"testing"

	splstorage "github.com/splunk/splunk-operator/pkg/splunk/client/storage"
	storageaws "github.com/splunk/splunk-operator/pkg/splunk/client/storage/aws"
)

func TestRegisterRemoteDataClient(t *testing.T) {
	ctx := context.TODO()
	// clear any stale entries present in the splstorage.RemoteDataClientsMap map
	for k := range splstorage.RemoteDataClientsMap {
		delete(splstorage.RemoteDataClientsMap, k)
	}

	// 1. Test for aws
	splstorage.RegisterRemoteDataClient(ctx, "aws")
	if len(splstorage.RemoteDataClientsMap) == 0 {
		t.Errorf("We should have initialized the client for aws.")
	}

	// 2. Test for minio
	splstorage.RegisterRemoteDataClient(ctx, "minio")
	if len(splstorage.RemoteDataClientsMap) == 1 {
		t.Errorf("We should have initialized the client for minio as well.")
	}

	// 3. Test for azure
	splstorage.RegisterRemoteDataClient(ctx, "azure")
	if len(splstorage.RemoteDataClientsMap) == 1 {
		t.Errorf("We should have initialized the client for azure as well.")
	}

	// 3. Test for invalid provider
	splstorage.RegisterRemoteDataClient(ctx, "invalid")
	if len(splstorage.RemoteDataClientsMap) > 3 {
		t.Errorf("We should only have initialized the client for aws, minio and azure but not for an invalid provider.")
	}

}

func TestGetSetRemoteDataClientFuncPtr(t *testing.T) {
	c := &splstorage.GetRemoteDataClientWrapper{}
	ctx := context.TODO()

	fn := c.GetRemoteDataClientFuncPtr(ctx)
	if fn != nil {
		t.Errorf("We should have received a nil function pointer")
	}

	c.SetRemoteDataClientFuncPtr(ctx, "aws", storageaws.NewS3Client)
	if c.GetRemoteDataClient == nil {
		t.Errorf("We should have set GetRemoteDataClient func pointer for AWS client.")
	}
}

func TestGetSetRemoteDataClientInitFuncPtr(t *testing.T) {
	ctx := context.TODO()
	c := &splstorage.GetRemoteDataClientWrapper{}

	fn := c.GetRemoteDataClientInitFuncPtr(ctx)
	if fn != nil {
		t.Errorf("We should have received a nil init function pointer")
	}

	c.SetRemoteDataClientInitFuncPtr(ctx, "aws", storageaws.InitClientWrapper)
	if c.GetInitFunc == nil {
		t.Errorf("We should have set GetInitFunc func pointer for AWS client.")
	}
}
