/*
Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package core

import (
	"context"
	"fmt"
	"slices"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgcconstants "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/constants"
	pgendpointprotocol "github.com/splunk/splunk-operator/pkg/postgresql/shared/endpointprotocol"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// observeAvailableUpgradeTarget supplies the live green object to the role and
// metrics components after Target Available releases their database freeze.
// The provisioner remains blocked so observing this contract never reconciles
// away blue/green bootstrap, replication or access configuration.
func observeAvailableUpgradeTarget(ctx context.Context, reader client.Reader, parent *platformv1alpha1.PostgresCluster, contracts *reconcileContracts, attemptID string) error {
	if parent == nil || len(parent.Status.PostgresMajorUpgradeStatus) == 0 {
		return nil
	}
	previous := parent.Status.PostgresMajorUpgradeStatus[len(parent.Status.PostgresMajorUpgradeStatus)-1].BlueGreen
	if previous == nil || previous.Switchover == nil || previous.Switchover.TargetAvailableAt == nil {
		return nil
	}
	if reader == nil || contracts == nil {
		return fmt.Errorf("available upgrade target requires an uncached API reader and reconcile contracts")
	}
	if parent.DeletionTimestamp != nil {
		return fmt.Errorf("available upgrade parent identity changed")
	}
	bg := parent.Status.PostgresMajorUpgradeStatus[len(parent.Status.PostgresMajorUpgradeStatus)-1].BlueGreen
	if bg == nil || bg.AttemptID != attemptID || bg.Switchover == nil || bg.Switchover.TargetAvailableAt == nil || !pgendpointprotocol.DurableCommitStatusMatches(parent, bg) {
		return fmt.Errorf("available upgrade target has no matching durable commit proof")
	}
	ref := bg.Green.Ref
	authority := contracts.Authority.Authoritative.Identity
	if ref.Namespace != parent.Namespace || ref.Name == "" || ref.UID == "" || authority.Namespace != ref.Namespace || authority.Name != ref.Name || authority.UID != ref.UID {
		return fmt.Errorf("available upgrade target differs from resolved cluster authority")
	}
	green := new(cnpgv1.Cluster)
	if err := reader.Get(ctx, client.ObjectKey{Namespace: ref.Namespace, Name: ref.Name}, green); err != nil {
		return fmt.Errorf("reading available upgrade target: %w", err)
	}
	owner := metav1.GetControllerOf(green)
	if green.UID != ref.UID || green.DeletionTimestamp != nil || owner == nil || owner.UID != parent.UID || owner.Name != parent.Name || owner.Kind != "PostgresCluster" || owner.APIVersion != platformv1alpha1.GroupVersion.String() {
		return fmt.Errorf("available upgrade target is replaced, deleting or not controlled by its parent")
	}
	secret := new(corev1.Secret)
	if err := reader.Get(ctx, client.ObjectKey{Namespace: green.Namespace, Name: green.GetSuperuserSecretName()}, secret); err != nil {
		return fmt.Errorf("reading available upgrade target credential: %w", err)
	}
	if secret.DeletionTimestamp != nil || len(secret.Data[secretKeyPassword]) == 0 {
		return fmt.Errorf("available upgrade target credential is deleting or has no password")
	}
	contracts.CNPGCluster = green
	contracts.Secret = secret
	return nil
}

// availableUpgradeTargetHealth observes the proven target without invoking the
// provisioner's mutation path. Parent readiness can recover during the handoff,
// but only while green's current topology and primary are healthy.
func availableUpgradeTargetHealth(parent *platformv1alpha1.PostgresCluster, contracts *reconcileContracts) (componentHealth, error) {
	green := contracts.CNPGCluster
	if green == nil {
		return newPendingHealth(clusterReady, reasonCNPGProvisioning, "Waiting for the committed green environment."), nil
	}
	observer := &clusterModel{cluster: parent, cnpgCluster: green, contracts: contracts,
		mergedConfig: &MergedConfig{Spec: &platformv1alpha1.PostgresClusterSpec{Instances: new(int32(green.Spec.Instances))}}}
	health, err := observer.computeHealth(nil)
	if err != nil || health.State != pgcconstants.Ready {
		return health, err
	}
	healthy := green.Status.InstancesStatus[cnpgv1.PodHealthy]
	if green.Spec.Instances <= 0 || green.Status.CurrentPrimary == "" ||
		green.Status.TargetPrimary != "" && green.Status.TargetPrimary != green.Status.CurrentPrimary ||
		len(healthy) != green.Spec.Instances || !slices.Contains(healthy, green.Status.CurrentPrimary) {
		return newProvisioningHealth(clusterReady, reasonCNPGProvisioning, "Waiting for the committed green topology and primary to become healthy."), nil
	}
	return health, nil
}
