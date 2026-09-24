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

package predicates_test

import (
	"testing"

	"github.com/splunk/splunk-operator/pkg/postgresql/shared/predicates"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

func TestExternalSecret(t *testing.T) {
	t.Parallel()
	p := predicates.ExternalSecret()

	t.Run("Create always fires", func(t *testing.T) {
		assert.True(t, p.Create(event.CreateEvent{Object: &corev1.Secret{}}))
	})

	t.Run("Delete always fires", func(t *testing.T) {
		assert.True(t, p.Delete(event.DeleteEvent{Object: &corev1.Secret{}}))
	})

	t.Run("Generic never fires", func(t *testing.T) {
		assert.False(t, p.Generic(event.GenericEvent{Object: &corev1.Secret{}}))
	})

	t.Run("Update fires when .data changes", func(t *testing.T) {
		old := &corev1.Secret{Data: map[string][]byte{"password": []byte("a")}}
		updated := &corev1.Secret{Data: map[string][]byte{"password": []byte("b")}}
		assert.True(t, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))
	})

	t.Run("Update fires when .labels change", func(t *testing.T) {
		old := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"cnpg.io/reload": "true"}}}
		updated := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{}}}
		assert.True(t, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))
	})

	t.Run("Update suppresses pure resourceVersion churn", func(t *testing.T) {
		old := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{ResourceVersion: "1"},
			Data:       map[string][]byte{"password": []byte("p")},
		}
		updated := old.DeepCopy()
		updated.ResourceVersion = "2"
		assert.False(t, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))
	})

	t.Run("Update suppresses annotation-only churn", func(t *testing.T) {
		old := &corev1.Secret{Data: map[string][]byte{"password": []byte("p")}}
		updated := old.DeepCopy()
		updated.Annotations = map[string]string{"kubectl.kubernetes.io/last-applied": "..."}
		assert.False(t, p.Update(event.UpdateEvent{ObjectOld: old, ObjectNew: updated}))
	})

	t.Run("Update on non-Secret types returns false", func(t *testing.T) {
		assert.False(t, p.Update(event.UpdateEvent{
			ObjectOld: &corev1.ConfigMap{}, ObjectNew: &corev1.ConfigMap{},
		}))
	})
}
