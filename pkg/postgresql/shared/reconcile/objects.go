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

package reconcile

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ReadExactObject checks existence and UID. Callers validate kind, scope and ownership.
func ReadExactObject(ctx context.Context, reader client.Reader, ref corev1.ObjectReference, object client.Object) (bool, error) {
	if ref.Name == "" || ref.Namespace == "" || ref.UID == "" {
		return false, fmt.Errorf("resource reference has incomplete identity")
	}
	if err := reader.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, object); err != nil {
		return false, client.IgnoreNotFound(err)
	}
	if object.GetUID() != ref.UID {
		return false, fmt.Errorf("%s %s was replaced", ref.Kind, ref.Name)
	}
	return true, nil
}

// PatchLocked rejects concurrent writes to a mutation target.
func PatchLocked(ctx context.Context, c client.Client, before, object client.Object) error {
	return c.Patch(ctx, object, client.MergeFromWithOptions(before, client.MergeFromWithOptimisticLock{}))
}

// DeleteExact binds deletion to the observed UID and resource version.
func DeleteExact(ctx context.Context, c client.Client, object client.Object, options ...client.DeleteOption) error {
	uid, version := object.GetUID(), object.GetResourceVersion()
	options = append(options, client.Preconditions{UID: &uid, ResourceVersion: &version})
	return client.IgnoreNotFound(c.Delete(ctx, object, options...))
}
