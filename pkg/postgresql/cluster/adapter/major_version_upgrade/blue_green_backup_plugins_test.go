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

package majorupgradeadapter

import (
	"testing"

	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	clustercore "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestAttemptObjectStoreRejectsCollisionAndDeletionWithoutRewriting(t *testing.T) {
	for _, state := range []string{"owned", "other owner", "other attempt", "different policy", "deleting"} {
		t.Run(state, func(t *testing.T) {
			f, _, _ := completionFixture(t)
			c := f.runtime.client
			config := &platformv1alpha1.CNPGBarmanObjectStoreConfig{DestinationPath: "s3://expected"}
			ready, err := ensureAttemptObjectStore(t.Context(), c, f.runtime.owner, "green", f.status().AttemptID, config)
			require.NoError(t, err)
			assert.False(t, ready, "creation is observed on a later pass")
			object := &unstructured.Unstructured{}
			object.SetGroupVersionKind(clustercore.ObjectStoreGVK)
			key := client.ObjectKey{Namespace: f.runtime.owner.Namespace, Name: "green-object-store"}
			require.NoError(t, c.Get(t.Context(), key, object))
			switch state {
			case "other owner":
				owners := object.GetOwnerReferences()
				owners[0].UID = "another-parent"
				object.SetOwnerReferences(owners)
			case "other attempt":
				object.SetLabels(map[string]string{blueGreenAttemptLabel: "another-attempt"})
			case "different policy":
				require.NoError(t, unstructured.SetNestedField(object.Object, "s3://other", "spec", "configuration", "destinationPath"))
			case "deleting":
				object.SetFinalizers([]string{"example.com/hold"})
			}
			require.NoError(t, c.Update(t.Context(), object))
			if state == "deleting" {
				require.NoError(t, c.Delete(t.Context(), object))
			}
			require.NoError(t, c.Get(t.Context(), key, object))
			before := object.DeepCopy()
			ready, err = ensureAttemptObjectStore(t.Context(), c, f.runtime.owner, "green", f.status().AttemptID, config)
			assert.Equal(t, state == "owned", ready)
			if state == "owned" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.NoError(t, c.Get(t.Context(), key, object))
			assert.Equal(t, before.Object, object.Object)
		})
	}
}
