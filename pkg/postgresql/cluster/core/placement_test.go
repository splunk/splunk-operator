package core

import (
	"testing"

	cnpgv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
	platformv1alpha1 "github.com/splunk/splunk-operator/api/platform/v1alpha1"
	pgtestfixture "github.com/splunk/splunk-operator/internal/testutil/postgres"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestClusterBuilderPropagatesClassStorageAndPlacement(t *testing.T) {
	class := &platformv1alpha1.PostgresClusterClass{Spec: platformv1alpha1.PostgresClusterClassSpec{
		Config: &platformv1alpha1.PostgresClusterClassConfig{
			Instances: new(int32(3)), PostgresVersion: new("17"), Storage: new(resource.MustParse("100Gi")),
		},
		CNPG: &platformv1alpha1.CNPGConfig{StorageClass: new("encrypted"), Affinity: &platformv1alpha1.CNPGAffinityConfig{
			PodAntiAffinityType: "required", NodeSelector: map[string]string{"pool": "database"},
		}},
	}}
	cluster := &platformv1alpha1.PostgresCluster{Spec: platformv1alpha1.PostgresClusterSpec{Images: pgtestfixture.Images()}}
	config := GetMergedConfig(class, cluster)
	actual := mustBuildCNPGClusterSpec(t, cnpgv1.ClusterSpec{}, config, "shared", "superuser", true)
	assert.Equal(t, "100Gi", actual.StorageConfiguration.Size)
	require.NotNil(t, actual.StorageConfiguration.StorageClass)
	assert.Equal(t, "encrypted", *actual.StorageConfiguration.StorageClass)
	assert.Equal(t, "required", actual.Affinity.PodAntiAffinityType)
	assert.Equal(t, "database", actual.Affinity.NodeSelector["pool"])
	assert.Equal(t, 3, actual.Instances)
}

func TestClassPlacementConvergesWithoutOverwritingIndependentAffinity(t *testing.T) {
	config := &platformv1alpha1.CNPGConfig{
		StorageClass: new("encrypted-ssd"),
		Affinity: &platformv1alpha1.CNPGAffinityConfig{
			EnablePodAntiAffinity: new(true), PodAntiAffinityType: "required",
			TopologyKey: "kubernetes.io/hostname", NodeSelector: map[string]string{"pool": "database"},
			Tolerations: []corev1.Toleration{{Key: "database", Operator: corev1.TolerationOpExists, Effect: corev1.TaintEffectNoExecute, TolerationSeconds: new(int64(60))}},
		},
	}
	live := cnpgv1.ClusterSpec{}
	live.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	live.StorageConfiguration.Size = "100Gi"
	before := normalizeCNPGClusterSpec(live)
	applyCNPGPlacement(&live, config)
	assert.True(t, isAnyClusterSpecDrift(before, normalizeCNPGClusterSpec(live)))
	require.NotNil(t, live.StorageConfiguration.StorageClass)
	assert.Equal(t, "encrypted-ssd", *live.StorageConfiguration.StorageClass)
	assert.Equal(t, "required", live.Affinity.PodAntiAffinityType)
	assert.Equal(t, "database", live.Affinity.NodeSelector["pool"])
	assert.NotNil(t, live.Affinity.NodeAffinity, "placement must preserve fields the class does not own")
	first := normalizeCNPGClusterSpec(live)
	applyCNPGPlacement(&live, config)
	assert.False(t, isAnyClusterSpecDrift(first, normalizeCNPGClusterSpec(live)), "a second reconcile must converge")
	live.Affinity.NodeSelector["pool"] = "other"
	*live.Affinity.Tolerations[0].TolerationSeconds = 5
	assert.Equal(t, "database", config.Affinity.NodeSelector["pool"])
	assert.Equal(t, int64(60), *config.Affinity.Tolerations[0].TolerationSeconds)
	applyCNPGPlacement(&live, config)
	assert.False(t, isAnyClusterSpecDrift(first, normalizeCNPGClusterSpec(live)), "reconciliation must repair placement drift")
}

func TestOmittedClassPlacementPreservesExistingDedicatedCluster(t *testing.T) {
	for _, cnpg := range []*platformv1alpha1.CNPGConfig{nil, {}} {
		class := &platformv1alpha1.PostgresClusterClass{Spec: platformv1alpha1.PostgresClusterClassSpec{
			CNPG: cnpg, Config: &platformv1alpha1.PostgresClusterClassConfig{
				Instances: new(int32(3)), PostgresVersion: new("17"), Storage: new(resource.MustParse("100Gi")),
			},
		}}
		cluster := &platformv1alpha1.PostgresCluster{Spec: platformv1alpha1.PostgresClusterSpec{Images: pgtestfixture.Images()}}
		cfg := GetMergedConfig(class, cluster)
		live := cnpgv1.ClusterSpec{}
		live.StorageConfiguration.StorageClass = new("existing-storage")
		live.StorageConfiguration.ResizeInUseVolumes = new(false)
		live.StorageConfiguration.PersistentVolumeClaimTemplate = &corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}}
		live.Affinity.NodeSelector = map[string]string{"pool": "existing"}
		live.Affinity.NodeAffinity = &corev1.NodeAffinity{}
		live.Affinity.Tolerations = []corev1.Toleration{{Key: "database", Operator: corev1.TolerationOpExists}}
		before := live.DeepCopy()
		first := mustBuildCNPGClusterSpec(t, live, cfg, "existing", "superuser", true)
		before.StorageConfiguration.Size = cfg.Spec.Storage.String()
		assert.Equal(t, before.StorageConfiguration, first.StorageConfiguration)
		assert.Equal(t, before.Affinity, first.Affinity)
		// CNPG defaults must not trigger another update after the first build.
		first.Affinity.EnablePodAntiAffinity = new(true)
		first.Affinity.PodAntiAffinityType = "preferred"
		first.Affinity.TopologyKey = "kubernetes.io/hostname"
		second := mustBuildCNPGClusterSpec(t, *first.DeepCopy(), cfg, "existing", "superuser", true)
		assert.Equal(t, first, second)
		assert.False(t, isAnyClusterSpecDrift(normalizeCNPGClusterSpec(first), normalizeCNPGClusterSpec(second)))
	}
}

func TestExplicitEmptyClassPlacementClearsOwnedFields(t *testing.T) {
	live := cnpgv1.ClusterSpec{}
	live.StorageConfiguration.StorageClass = new("existing")
	live.Affinity.NodeSelector = map[string]string{"pool": "existing"}
	live.Affinity.Tolerations = []corev1.Toleration{{Operator: corev1.TolerationOpExists}}
	live.Affinity.NodeAffinity = &corev1.NodeAffinity{}
	applyCNPGPlacement(&live, &platformv1alpha1.CNPGConfig{StorageClass: new(""), Affinity: &platformv1alpha1.CNPGAffinityConfig{}})
	require.NotNil(t, live.StorageConfiguration.StorageClass)
	assert.Empty(t, *live.StorageConfiguration.StorageClass)
	assert.Empty(t, live.Affinity.NodeSelector)
	assert.Empty(t, live.Affinity.Tolerations)
	assert.NotNil(t, live.Affinity.NodeAffinity)
	assert.True(t, *live.Affinity.EnablePodAntiAffinity)
	assert.Equal(t, "preferred", live.Affinity.PodAntiAffinityType)
	assert.Equal(t, "kubernetes.io/hostname", live.Affinity.TopologyKey)
}

func TestCNPGPlacementDefaultsDoNotCauseRepeatedDrift(t *testing.T) {
	live := cnpgv1.ClusterSpec{}
	before := normalizeCNPGClusterSpec(live)
	applyCNPGPlacement(&live, &platformv1alpha1.CNPGConfig{Affinity: &platformv1alpha1.CNPGAffinityConfig{}})
	assert.False(t, isAnyClusterSpecDrift(before, normalizeCNPGClusterSpec(live)))
	live.Affinity.EnablePodAntiAffinity = new(false)
	assert.True(t, isAnyClusterSpecDrift(before, normalizeCNPGClusterSpec(live)))
}
