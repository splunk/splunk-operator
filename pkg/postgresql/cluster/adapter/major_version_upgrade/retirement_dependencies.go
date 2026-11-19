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
	"context"
	"fmt"
	"slices"

	snapshotv1 "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	pgreconcile "github.com/splunk/splunk-operator/pkg/postgresql/shared/reconcile"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func retirementOwner(owner metav1.OwnerReference, environment corev1.ObjectReference) bool {
	return owner.UID == environment.UID && owner.Name == environment.Name && owner.Kind == "Cluster" && owner.APIVersion == environment.APIVersion
}

// Preserve every Cluster-owned data, WAL and tablespace snapshot before GC.
func preserveClusterSnapshots(ctx context.Context, c client.Client, reader client.Reader, environment corev1.ObjectReference, verify func(context.Context) error) (bool, error) {
	var snapshots snapshotv1.VolumeSnapshotList
	if err := reader.List(ctx, &snapshots, client.InNamespace(environment.Namespace)); err != nil {
		if meta.IsNoMatchError(err) {
			return true, nil
		}
		return false, err
	}
	for i := range snapshots.Items {
		snapshot := &snapshots.Items[i]
		original := snapshot.DeepCopy()
		snapshot.OwnerReferences = slices.DeleteFunc(snapshot.OwnerReferences, func(owner metav1.OwnerReference) bool { return retirementOwner(owner, environment) })
		if len(snapshot.OwnerReferences) == len(original.OwnerReferences) {
			continue
		}
		if snapshot.DeletionTimestamp != nil {
			return false, fmt.Errorf("recovery snapshot %s is already deleting", snapshot.Name)
		}
		if err := verify(ctx); err != nil {
			return false, err
		}
		return false, pgreconcile.PatchLocked(ctx, c, original, snapshot)
	}
	return true, nil
}
