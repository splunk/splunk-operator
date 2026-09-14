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
	"errors"
	"fmt"
	"strconv"
	"strings"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	mvutypes "github.com/splunk/splunk-operator/pkg/postgresql/cluster/core/types/major_version_upgrade"
	clusterCnpg "github.com/splunk/splunk-operator/pkg/postgresql/cluster/infrastructure/cnpg"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type PostgresImageForVersion func(string) (string, error)

type PgUpgradeDriver struct {
	client           client.Client
	key              client.ObjectKey
	targetPgVersion  string
	imageForVersion  PostgresImageForVersion
	imagePullSecrets []cnpgv1.LocalObjectReference
}

func NewPgUpgradeDriver(client client.Client, key client.ObjectKey, targetPgVersion string) *PgUpgradeDriver {
	return &PgUpgradeDriver{
		client:          client,
		key:             key,
		targetPgVersion: targetPgVersion,
		imageForVersion: defaultPostgresImageForVersion,
	}
}

func (d *PgUpgradeDriver) WithImageForVersion(imageForVersion PostgresImageForVersion) *PgUpgradeDriver {
	if imageForVersion != nil {
		d.imageForVersion = imageForVersion
	}
	return d
}

func (d *PgUpgradeDriver) WithImagePullSecrets(imagePullSecrets []cnpgv1.LocalObjectReference) *PgUpgradeDriver {
	d.imagePullSecrets = imagePullSecrets
	return d
}

func (d *PgUpgradeDriver) ApplyTargetImage(ctx context.Context) error {
	cluster, targetImage, err := d.targetImage(ctx)
	if err != nil {
		return err
	}
	if cluster.Spec.ImageName == targetImage && equality.Semantic.DeepEqual(cluster.Spec.ImagePullSecrets, d.imagePullSecrets) {
		return nil
	}

	original := cluster.DeepCopy()
	cluster.Spec.ImageName = targetImage
	cluster.Spec.ImagePullSecrets = d.imagePullSecrets
	return d.client.Patch(ctx, cluster, client.MergeFrom(original))
}

func (d *PgUpgradeDriver) UpgradeComplete(ctx context.Context) (bool, error) {
	cluster, targetImage, err := d.targetImage(ctx)
	if err != nil {
		return false, err
	}
	targetMajor, err := d.targetMajorVersion()
	if err != nil {
		return false, err
	}

	// Distinguish whether the failure occured
	// either post or pre pgData conversion for clarity
	if cluster.Status.Phase == cnpgv1.PhaseUnrecoverable {
		if cluster.Status.PGDataImageInfo != nil &&
			cluster.Status.PGDataImageInfo.MajorVersion == targetMajor {
			return false, fmt.Errorf("%w: %s",
				mvutypes.ErrUpgradeUnrecoverablePostConversion, cluster.Status.PhaseReason)
		}
		return false, fmt.Errorf("%w: %s",
			mvutypes.ErrUpgradeUnrecoverablePreConversion, cluster.Status.PhaseReason)
	}

	if err := clusterCnpg.ClusterBlockingError(cluster); err != nil {
		return false, errors.Join(mvutypes.ErrUpgradeFlowFailed, err)
	}
	if cluster.Spec.ImageName != targetImage {
		return false, nil
	}
	return cnpgUpgradeConverged(cluster, targetMajor), nil
}

func (d *PgUpgradeDriver) VerifyUpgrade(ctx context.Context) (bool, error) {
	cluster, targetImage, err := d.targetImage(ctx)
	if err != nil {
		return false, err
	}
	targetMajor, err := d.targetMajorVersion()
	if err != nil {
		return false, err
	}
	if err := clusterCnpg.ClusterBlockingError(cluster); err != nil {
		return false, errors.Join(mvutypes.ErrUpgradeFlowFailed, err)
	}
	if cluster.Spec.ImageName != targetImage {
		return false, fmt.Errorf("cnpg cluster image %q does not match target image %q", cluster.Spec.ImageName, targetImage)
	}
	if !cnpgUpgradeConverted(cluster, targetMajor) {
		observedMajor := 0
		if cluster.Status.PGDataImageInfo != nil {
			observedMajor = cluster.Status.PGDataImageInfo.MajorVersion
		}
		return false, fmt.Errorf("cnpg cluster has inconsistent conversion state after pg_upgrade: targetMajor=%d observedPGDataMajor=%d conversionPending=%t",
			targetMajor,
			observedMajor,
			cluster.Status.TargetPGDataImageInfo != nil)
	}
	if !clusterCnpg.PrimaryReady(cluster) {
		return false, nil
	}
	return true, nil
}

func cnpgUpgradeConverged(cluster *cnpgv1.Cluster, targetMajor int) bool {
	return cnpgUpgradeConverted(cluster, targetMajor) && clusterCnpg.PrimaryReady(cluster)
}

func cnpgUpgradeConverted(cluster *cnpgv1.Cluster, targetMajor int) bool {
	return cluster != nil &&
		cluster.Status.PGDataImageInfo != nil &&
		cluster.Status.PGDataImageInfo.MajorVersion == targetMajor &&
		cluster.Status.TargetPGDataImageInfo == nil
}

func (d *PgUpgradeDriver) targetMajorVersion() (int, error) {
	major, _, _ := strings.Cut(d.targetPgVersion, ".")
	parsed, err := strconv.Atoi(major)
	if err != nil || parsed <= 0 {
		return 0, errors.Join(
			mvutypes.ErrUpgradeFlowFailed,
			fmt.Errorf("invalid PostgreSQL target version %q", d.targetPgVersion),
		)
	}
	return parsed, nil
}

func (d *PgUpgradeDriver) targetImage(ctx context.Context) (*cnpgv1.Cluster, string, error) {
	if d == nil || d.client == nil {
		return nil, "", errors.Join(mvutypes.ErrUpgradeFlowFailed, fmt.Errorf("pg_upgrade cnpg client is not configured"))
	}
	if d.targetPgVersion == "" {
		return nil, "", errors.Join(mvutypes.ErrUpgradeFlowFailed, fmt.Errorf("pg_upgrade target PostgreSQL version is not configured"))
	}

	targetImage, err := d.imageForVersion(d.targetPgVersion)
	if err != nil {
		return nil, "", errors.Join(mvutypes.ErrUpgradeFlowFailed, err)
	}
	if targetImage == "" {
		return nil, "", errors.Join(mvutypes.ErrUpgradeFlowFailed, fmt.Errorf("pg_upgrade target image is empty for PostgreSQL version %q", d.targetPgVersion))
	}

	cluster := &cnpgv1.Cluster{}
	if err := d.client.Get(ctx, d.key, cluster); err != nil {
		return nil, "", err
	}
	return cluster, targetImage, nil
}

func defaultPostgresImageForVersion(version string) (string, error) {
	return clusterCnpg.PostgresImageName(version), nil
}
