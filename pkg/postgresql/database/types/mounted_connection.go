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

import pgconninfo "github.com/splunk/splunk-operator/pkg/postgresql/shared/connectioninfo"

// Mounted connection layout. A workload that mounts a database's connection
// gets one directory per consumer, containing each MountedConnectionKeys entry
// as a file plus the password and CA files below. Consumers read these names,
// so changing them breaks every mounted consumer.
const (
	MountedRWPasswordFile    = "RW_PASSWORD"
	MountedAdminPasswordFile = "ADMIN_PASSWORD"
	MountedCAFile            = "ca.crt"
)

// MountedConnectionKeys are the published connection ConfigMap keys that a
// mounted consumer receives as files. Read-only and pooler endpoints are not
// mounted. Treat this slice as read-only.
var MountedConnectionKeys = []string{
	pgconninfo.KeyClusterRWEndpoint, pgconninfo.KeyDefaultClusterPort,
	ConnectionKeyDatabaseName, ConnectionKeyAdminUser, ConnectionKeyRWUser,
}
