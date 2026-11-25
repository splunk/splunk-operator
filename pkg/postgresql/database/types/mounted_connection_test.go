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

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Sidecar consumers read these names. A change here is a breaking layout change.
func TestMountedConnectionLayout(t *testing.T) {
	assert.Equal(t, []string{"CLUSTER_RW_ENDPOINT", "DEFAULT_CLUSTER_PORT", "DATABASE_NAME", "ADMIN_USER_NAME", "RW_USER_NAME"}, MountedConnectionKeys)
	assert.Equal(t, "RW_PASSWORD", MountedRWPasswordFile)
	assert.Equal(t, "ADMIN_PASSWORD", MountedAdminPasswordFile)
	assert.Equal(t, "ca.crt", MountedCAFile)
}
