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

package majorupgradeadapter

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Named selectors keep environment and desired access state distinct at call sites.
type environmentSide uint8

const (
	sideBlue environmentSide = iota
	sideGreen
)

type fenceState uint8

const (
	fenceOpen fenceState = iota
	fenceApplied
)

func (f fenceState) opposite() fenceState {
	if f == fenceOpen {
		return fenceApplied
	}
	return fenceOpen
}

// accessSQL owns effective PostgreSQL checks. Declarative CNPG state alone is
// insufficient: its instance managers may not yet have loaded role/HBA changes.
type accessSQL interface {
	PrepareControl(context.Context, *cnpgv1.Cluster, []declaredDatabase, cutoverIdentity) error
	VerifyControl(context.Context, *cnpgv1.Cluster, cutoverIdentity) error
	VerifyFence(context.Context, *cnpgv1.Cluster, cutoverIdentity, []string) (bool, error)
	VerifyRetainedSourceFence(context.Context, *cnpgv1.Cluster, cutoverIdentity, *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error)
	VerifyOpen(context.Context, *cnpgv1.Cluster, cutoverIdentity, []platformv1alpha1.BlueGreenRoleLoginStatus) (bool, error)
}

type blueGreenAccess struct {
	client client.Client
	reader client.Reader
	key    types.NamespacedName
	owner  *platformv1alpha1.PostgresCluster
	sql    accessSQL
}

func newBlueGreenAccess(c client.Client, reader client.Reader, key types.NamespacedName, owner *platformv1alpha1.PostgresCluster, sql accessSQL) (*blueGreenAccess, error) {
	if c == nil || reader == nil || sql == nil || owner == nil || owner.UID == "" || key.Name == "" || key.Namespace == "" || key.Name != owner.Name || key.Namespace != owner.Namespace {
		return nil, fmt.Errorf("blue/green access requires clients, SQL operations, and a matching persisted parent identity")
	}
	return &blueGreenAccess{client: c, reader: reader, key: key, owner: owner, sql: sql}, nil
}

// Snapshot only records an immutable recovery baseline and an owned credential.
// The caller persists this status before PrepareControl or ApplyFence may run.
func (a *blueGreenAccess) Snapshot(ctx context.Context, blue, green *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	blue, err := a.liveEnvironment(ctx, blue, status, sideBlue)
	if err != nil {
		return err
	}
	green, err = a.liveEnvironment(ctx, green, status, sideGreen)
	if err != nil {
		return err
	}
	switchover := status.Switchover
	if switchover.OriginalHBA != nil || switchover.GreenOriginalHBA != nil || switchover.ControlIdentity != nil {
		if switchover.OriginalHBA == nil || switchover.GreenOriginalHBA == nil || switchover.ControlIdentity == nil {
			return accessError("switchover has an incomplete access recovery snapshot")
		}
		_, err := a.Credentials(ctx, status)
		return err
	}
	blueRoles, err := snapshotRoleLogins(blue, databases, status.AttemptID)
	if err != nil {
		return err
	}
	greenRoles, err := snapshotRoleLogins(green, databases, status.AttemptID)
	if err != nil {
		return err
	}
	greenConfiguration, err := candidateConfigurationFingerprint(green)
	if err != nil {
		return err
	}
	secret, err := a.controlSecret(ctx, status.AttemptID)
	if err != nil {
		return err
	}
	// A pointer to a nil slice becomes JSON null and loses the captured-state
	// distinction after an API roundtrip. Persist an explicit empty array.
	switchover.OriginalHBA = new(append([]string{}, blue.Spec.PostgresConfiguration.PgHBA...))
	switchover.GreenOriginalHBA = new(append([]string{}, green.Spec.PostgresConfiguration.PgHBA...))
	switchover.OriginalRoleLogins, switchover.GreenOriginalRoleLogins = blueRoles, greenRoles
	switchover.GreenConfigurationFingerprint = new(greenConfiguration)
	switchover.ControlIdentity = &platformv1alpha1.BlueGreenControlIdentityStatus{
		RoleName:  cutoverRoleName(status.AttemptID),
		SecretRef: corev1.ObjectReference{APIVersion: "v1", Kind: "Secret", Namespace: secret.Namespace, Name: secret.Name, UID: secret.UID},
	}
	return nil
}

func (a *blueGreenAccess) Credentials(ctx context.Context, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (cutoverIdentity, error) {
	if err := a.validate(status); err != nil {
		return cutoverIdentity{}, err
	}
	identity := status.Switchover.ControlIdentity
	if identity == nil {
		return cutoverIdentity{}, accessError("operator control identity has no durable reference")
	}
	ref := identity.SecretRef
	if ref.APIVersion != "v1" || ref.Kind != "Secret" || ref.Namespace != a.key.Namespace || ref.Name != controlSecretName(a.key.Name, status.AttemptID) || ref.UID == "" || identity.RoleName != cutoverRoleName(status.AttemptID) {
		return cutoverIdentity{}, accessError("operator control identity reference differs from this attempt")
	}
	secret := &corev1.Secret{}
	if err := a.reader.Get(ctx, types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, secret); err != nil {
		return cutoverIdentity{}, fmt.Errorf("reading recorded operator control Secret: %w", err)
	}
	if secret.UID != ref.UID {
		return cutoverIdentity{}, accessError("operator control Secret was replaced")
	}
	if err := a.validateControlSecret(secret, status.AttemptID); err != nil {
		return cutoverIdentity{}, err
	}
	return cutoverIdentity{Role: identity.RoleName, Password: string(secret.Data[replicationSecretPasswordKey]), AttemptID: status.AttemptID}, nil
}

func (a *blueGreenAccess) PrepareControl(ctx context.Context, blue, green *cnpgv1.Cluster, databases []declaredDatabase, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	identity, err := a.Credentials(ctx, status)
	if err != nil {
		return err
	}
	if status.Switchover.OriginalHBA == nil || status.Switchover.GreenOriginalHBA == nil {
		return accessError("operator control access cannot precede its recovery snapshot")
	}
	for _, environment := range []struct {
		cluster *cnpgv1.Cluster
		side    environmentSide
	}{{blue, sideBlue}, {green, sideGreen}} {
		live, err := a.liveEnvironment(ctx, environment.cluster, status, environment.side)
		if err != nil {
			return err
		}
		if _, err := NormalizeFence(live, status, environment.side); err != nil {
			return err
		}
		if status.Switchover.ControlIdentity.PreparedAt == nil {
			if err := a.sql.PrepareControl(ctx, live, databases, identity); err != nil {
				return err
			}
		}
		if err := a.sql.VerifyControl(ctx, live, identity); err != nil {
			return err
		}
	}
	if status.Switchover.ControlIdentity.PreparedAt == nil {
		status.Switchover.ControlIdentity.PreparedAt = new(metav1.Now())
	}
	return nil
}

func (a *blueGreenAccess) ApplyFence(ctx context.Context, blue, green *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if _, err := a.Credentials(ctx, status); err != nil {
		return err
	}
	if status.Switchover.ControlIdentity.PreparedAt == nil {
		return accessError("operator control access was not verified before fencing")
	}
	if err := a.updateAccess(ctx, blue, status, sideBlue, fenceApplied); err != nil {
		return err
	}
	return a.updateAccess(ctx, green, status, sideGreen, fenceApplied)
}

// FenceBlue preserves source isolation during fail-forward recovery without
// touching green, which may already accept application writes after a crash.
func (a *blueGreenAccess) FenceBlue(ctx context.Context, blue *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	identity, err := a.Credentials(ctx, status)
	if err != nil {
		return false, err
	}
	if status.Switchover.ControlIdentity.PreparedAt == nil {
		return false, accessError("operator control access was not verified before fencing")
	}
	live, err := a.liveEnvironment(ctx, blue, status, sideBlue)
	if err != nil {
		return false, err
	}
	if !accessMatches(live, status, sideBlue, fenceApplied) {
		return false, accessError("blue CNPG access fence differs from the owned overlay")
	}
	// A changed postcommit overlay could already have admitted source writes.
	// Require the persisted fence, rather than silently repairing that history.
	return a.sql.VerifyRetainedSourceFence(ctx, live, identity, status)
}

func (a *blueGreenAccess) VerifyFences(ctx context.Context, blue, green *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	identity, err := a.Credentials(ctx, status)
	if err != nil {
		return false, err
	}
	for _, environment := range []struct {
		cluster *cnpgv1.Cluster
		side    environmentSide
	}{{blue, sideBlue}, {green, sideGreen}} {
		live, err := a.liveEnvironment(ctx, environment.cluster, status, environment.side)
		if err != nil {
			return false, err
		}
		if !accessMatches(live, status, environment.side, fenceApplied) {
			return false, accessError("CNPG access fence differs from the owned overlay")
		}
		roles := roleNames(accessOriginalRoles(status, environment.side))
		verified, err := a.sql.VerifyFence(ctx, live, identity, roles)
		if err != nil || !verified {
			return false, err
		}
	}
	return true, nil
}

// RestoreBlue removes only an exact owned overlay. Green remains fenced until
// the publication barrier permits OpenGreen.
func (a *blueGreenAccess) RestoreBlue(ctx context.Context, blue *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return a.open(ctx, blue, status, sideBlue)
}

func (a *blueGreenAccess) OpenGreen(ctx context.Context, green *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) (bool, error) {
	return a.open(ctx, green, status, sideGreen)
}

func (a *blueGreenAccess) open(ctx context.Context, cluster *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, side environmentSide) (bool, error) {
	identity, err := a.Credentials(ctx, status)
	if err != nil {
		return false, err
	}
	if err := a.updateAccess(ctx, cluster, status, side, fenceOpen); err != nil {
		return false, err
	}
	live, err := a.liveEnvironment(ctx, cluster, status, side)
	if err != nil {
		return false, err
	}
	if !accessMatches(live, status, side, fenceOpen) {
		return false, accessError("CNPG access restoration differs from its snapshot")
	}
	return a.sql.VerifyOpen(ctx, live, identity, accessOriginalRoles(status, side))
}

// NormalizeFence removes only the exact recorded overlay from a copy. The
// frozen configuration fingerprint still detects every other spec mutation.
func NormalizeFence(cluster *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, side environmentSide) (*cnpgv1.Cluster, error) {
	if cluster == nil || status == nil || status.Switchover == nil || status.Switchover.ControlIdentity == nil || accessOriginalHBA(status, side) == nil {
		return nil, accessError("access normalization has no complete recovery snapshot")
	}
	if !accessMatches(cluster, status, side, fenceApplied) && !accessMatches(cluster, status, side, fenceOpen) {
		return nil, accessError("access state differs from both baseline and owned fence")
	}
	result := cluster.DeepCopy()
	setAccess(result, status, side, fenceOpen)
	return result, nil
}

func (a *blueGreenAccess) updateAccess(ctx context.Context, cluster *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, side environmentSide, fence fenceState) error {
	live, err := a.liveEnvironment(ctx, cluster, status, side)
	if err != nil {
		return err
	}
	if accessOriginalHBA(status, side) == nil {
		return accessError("access mutation has no durable recovery snapshot")
	}
	if accessMatches(live, status, side, fence) {
		return nil
	}
	if !accessMatches(live, status, side, fence.opposite()) {
		return accessError("refusing to overwrite access state outside the owned fence")
	}
	setAccess(live, status, side, fence)
	// Update carries the API-server resourceVersion and UID from the uncached
	// read. A concurrent change is retried by the outer reconcile after review.
	if err := a.client.Update(ctx, live); err != nil {
		return fmt.Errorf("updating CNPG access overlay: %w", err)
	}
	return nil
}

func (a *blueGreenAccess) liveEnvironment(ctx context.Context, cluster *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, side environmentSide) (*cnpgv1.Cluster, error) {
	if err := a.validate(status); err != nil {
		return nil, err
	}
	environment := status.Green
	if side == sideBlue {
		environment = status.Blue
	}
	if err := a.validateEnvironment(cluster, environment); err != nil {
		return nil, err
	}
	live := &cnpgv1.Cluster{}
	if err := a.reader.Get(ctx, client.ObjectKeyFromObject(cluster), live); err != nil {
		return nil, fmt.Errorf("reading CNPG access state: %w", err)
	}
	if err := a.validateEnvironment(live, environment); err != nil {
		return nil, err
	}
	if side == sideGreen && live.Labels[blueGreenAttemptLabel] != status.AttemptID {
		return nil, accessError("green CNPG environment no longer belongs to this attempt")
	}
	return live, nil
}

func (a *blueGreenAccess) validate(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus) error {
	if a == nil || a.client == nil || a.reader == nil || a.sql == nil || a.owner == nil || a.owner.UID == "" || a.key.Name != a.owner.Name || a.key.Namespace != a.owner.Namespace || status == nil || status.AttemptID == "" || status.Switchover == nil {
		return accessError("blue/green access wiring or attempt identity is incomplete")
	}
	return nil
}

func (a *blueGreenAccess) validateEnvironment(cluster *cnpgv1.Cluster, environment *platformv1alpha1.BlueGreenEnvironmentStatus) error {
	if cluster == nil || environment == nil || (environment.Ref.Kind != "" && environment.Ref.Kind != "Cluster") || (environment.Ref.APIVersion != "" && environment.Ref.APIVersion != cnpgv1.SchemeGroupVersion.String()) || environment.Ref.Namespace != a.key.Namespace || environment.Ref.Name != cluster.Name || environment.Ref.UID == "" || environment.Ref.UID != cluster.UID || cluster.Namespace != a.key.Namespace || cluster.DeletionTimestamp != nil || !accessOwnedBy(cluster, a.owner) {
		return accessError("CNPG environment differs from the attempt-owned durable reference")
	}
	return nil
}

func (a *blueGreenAccess) controlSecret(ctx context.Context, attemptID string) (*corev1.Secret, error) {
	name := controlSecretName(a.key.Name, attemptID)
	secret := &corev1.Secret{}
	err := a.reader.Get(ctx, types.NamespacedName{Namespace: a.key.Namespace, Name: name}, secret)
	if apierrors.IsNotFound(err) {
		password := make([]byte, 32)
		if _, err := rand.Read(password); err != nil {
			return nil, fmt.Errorf("generating operator control credential: %w", err)
		}
		secret = &corev1.Secret{
			Name: name, Namespace: a.key.Namespace, Labels: map[string]string{blueGreenAttemptLabel: attemptID}, OwnerReferences: []metav1.OwnerReference{*metav1.NewControllerRef(a.owner, platformv1alpha1.GroupVersion.WithKind("PostgresCluster"))},
			Type: corev1.SecretTypeBasicAuth, Immutable: new(true),
			Data: map[string][]byte{replicationSecretUsernameKey: []byte(cutoverRoleName(attemptID)), replicationSecretPasswordKey: []byte(base64.RawURLEncoding.EncodeToString(password))},
		}
		if err := a.client.Create(ctx, secret); err != nil {
			return nil, fmt.Errorf("creating operator control Secret: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("reading operator control Secret: %w", err)
	}
	if err := a.validateControlSecret(secret, attemptID); err != nil {
		return nil, err
	}
	return secret, nil
}

func (a *blueGreenAccess) validateControlSecret(secret *corev1.Secret, attemptID string) error {
	if secret.Name != controlSecretName(a.key.Name, attemptID) || secret.Namespace != a.key.Namespace || secret.UID == "" || secret.DeletionTimestamp != nil || !accessOwnedBy(secret, a.owner) || secret.Labels[blueGreenAttemptLabel] != attemptID || secret.Type != corev1.SecretTypeBasicAuth || secret.Immutable == nil || !*secret.Immutable || string(secret.Data[replicationSecretUsernameKey]) != cutoverRoleName(attemptID) || len(secret.Data[replicationSecretPasswordKey]) == 0 {
		return accessError("operator control Secret is incomplete or not owned by this attempt")
	}
	return nil
}

func accessOwnedBy(object metav1.Object, owner *platformv1alpha1.PostgresCluster) bool {
	controller := metav1.GetControllerOf(object)
	return controller != nil && controller.UID == owner.UID && controller.Name == owner.Name && controller.Kind == "PostgresCluster" && controller.APIVersion == platformv1alpha1.GroupVersion.String()
}

func snapshotRoleLogins(cluster *cnpgv1.Cluster, databases []declaredDatabase, attemptID string) ([]platformv1alpha1.BlueGreenRoleLoginStatus, error) {
	declared := declaredRoleNames(databases, "")
	roles := managedRoles(cluster)
	result := make([]platformv1alpha1.BlueGreenRoleLoginStatus, 0, len(roles))
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if _, duplicate := seen[role.Name]; duplicate {
			return nil, accessError("CNPG contains duplicate managed role declarations")
		}
		seen[role.Name] = struct{}{}
		if _, found := declared[role.Name]; !found || role.Name == replicationRoleName(attemptID) || role.Name == cutoverRoleName(attemptID) {
			return nil, accessError("CNPG contains a managed role outside the frozen application inventory")
		}
		if role.Ensure == cnpgv1.EnsureAbsent {
			return nil, accessError("CNPG application role is declared absent")
		}
		result = append(result, platformv1alpha1.BlueGreenRoleLoginStatus{RoleName: role.Name, Login: role.Login})
	}
	if len(seen) != len(declared) {
		return nil, accessError("CNPG managed roles do not cover the frozen application inventory")
	}
	slices.SortFunc(result, func(left, right platformv1alpha1.BlueGreenRoleLoginStatus) int {
		return strings.Compare(left.RoleName, right.RoleName)
	})
	return result, nil
}

func managedRoles(cluster *cnpgv1.Cluster) []cnpgv1.RoleConfiguration {
	if cluster.Spec.Managed == nil {
		return nil
	}
	return cluster.Spec.Managed.Roles
}

func accessMatches(cluster *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, side environmentSide, fence fenceState) bool {
	hba := accessOriginalHBA(status, side)
	if hba == nil {
		return false
	}
	wantedHBA := *hba
	if fence == fenceApplied {
		wantedHBA = fencedHBA(status, *hba)
	}
	if !slices.Equal(cluster.Spec.PostgresConfiguration.PgHBA, wantedHBA) {
		return false
	}
	original := accessOriginalRoles(status, side)
	roles := managedRoles(cluster)
	if len(roles) != len(original) {
		return false
	}
	seen := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		if _, duplicate := seen[role.Name]; duplicate {
			return false
		}
		seen[role.Name] = struct{}{}
		index := slices.IndexFunc(original, func(item platformv1alpha1.BlueGreenRoleLoginStatus) bool { return item.RoleName == role.Name })
		if index < 0 || role.Login != (fence == fenceOpen && original[index].Login) {
			return false
		}
	}
	return true
}

func setAccess(cluster *cnpgv1.Cluster, status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, side environmentSide, fence fenceState) {
	cluster.Spec.PostgresConfiguration.PgHBA = slices.Clone(*accessOriginalHBA(status, side))
	if fence == fenceApplied {
		cluster.Spec.PostgresConfiguration.PgHBA = fencedHBA(status, cluster.Spec.PostgresConfiguration.PgHBA)
	}
	original := accessOriginalRoles(status, side)
	for index := range managedRoles(cluster) {
		role := &cluster.Spec.Managed.Roles[index]
		baseline := slices.IndexFunc(original, func(item platformv1alpha1.BlueGreenRoleLoginStatus) bool { return item.RoleName == role.Name })
		role.Login = fence == fenceOpen && original[baseline].Login
	}
}

func fencedHBA(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, original []string) []string {
	rules := cutoverFenceRules(cutoverRoleName(status.AttemptID), replicationRoleName(status.AttemptID))
	result := make([]string, 0, len(rules)+len(original))
	for _, rule := range rules {
		result = append(result, rule.configLine())
	}
	return append(result, original...)
}

func accessOriginalHBA(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, side environmentSide) *[]string {
	if side == sideBlue {
		return status.Switchover.OriginalHBA
	}
	return status.Switchover.GreenOriginalHBA
}

func accessOriginalRoles(status *platformv1alpha1.PostgresBlueGreenUpgradeStatus, side environmentSide) []platformv1alpha1.BlueGreenRoleLoginStatus {
	if side == sideBlue {
		return status.Switchover.OriginalRoleLogins
	}
	return status.Switchover.GreenOriginalRoleLogins
}

func roleNames(roles []platformv1alpha1.BlueGreenRoleLoginStatus) []string {
	result := make([]string, 0, len(roles))
	for _, role := range roles {
		result = append(result, role.RoleName)
	}
	return result
}

func controlSecretName(clusterName, attemptID string) string {
	digest := sha256.Sum256([]byte(attemptID))
	suffix := fmt.Sprintf("-bgc-%x", digest[:4])
	return strings.TrimRight(clusterName[:min(len(clusterName), 63-len(suffix))], "-") + suffix
}

func cutoverRoleName(attemptID string) string {
	digest := sha256.Sum256([]byte(attemptID))
	return fmt.Sprintf("sok_bgc_%x", digest[:8])
}

func accessError(message string) error {
	return fmt.Errorf("%w: %s", mvutypes.ErrUpgradeFlowFailed, message)
}
