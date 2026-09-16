/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	 http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package types

import "errors"

// ErrConnectionMetadataConflict identifies a retryable write conflict without
// exposing Kubernetes error types to database policy.
var ErrConnectionMetadataConflict = errors.New("connection metadata write conflict")

// ErrConnectionEndpointProviderRead identifies a transient failure while
// reading provider state, before connection metadata can be evaluated.
var ErrConnectionEndpointProviderRead = errors.New("connection endpoint provider read failed")

const (
	ConnectionKeyDatabaseName = "DATABASE_NAME"
	ConnectionKeyAdminUser    = "ADMIN_USER_NAME"
	ConnectionKeyRWUser       = "RW_USER_NAME"
)

// ConnectionEndpointRequest identifies the provider resource whose endpoints
// are published for a PostgresDatabase.
type ConnectionEndpointRequest struct {
	ProviderResourceName string
	Namespace            string
	PoolerEnabled        bool
	PoolerRWReady        bool
	PoolerROReady        bool
}

// ConnectionMetadataTarget identifies the PostgresDatabase that owns generated
// connection metadata.
type ConnectionMetadataTarget struct {
	Name      string
	Namespace string
	UID       string
}

// ConnectionMetadataPublication is the desired data for one database
// connection ConfigMap.
type ConnectionMetadataPublication struct {
	Name string
	Data map[string]string
}
