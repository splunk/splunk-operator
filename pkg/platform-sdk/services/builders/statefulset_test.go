// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package builders

import (
	"testing"

	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/certificate"
	"github.com/splunk/splunk-operator/pkg/platform-sdk/api/secret"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestStatefulSetBuilder_Build(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*StatefulSetBuilder)
		wantErr   bool
		errMsg    string
		validate  func(*testing.T, *StatefulSetBuilder)
	}{
		{
			name: "valid basic statefulset",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithName("test-sts").
					WithImage("splunk/splunk:9.1.0").
					WithReplicas(3)
			},
			wantErr: false,
			validate: func(t *testing.T, b *StatefulSetBuilder) {
				sts, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if sts.Name != "test-sts" {
					t.Errorf("Name = %v, want test-sts", sts.Name)
				}
				if *sts.Spec.Replicas != 3 {
					t.Errorf("Replicas = %v, want 3", *sts.Spec.Replicas)
				}
			},
		},
		{
			name: "missing name",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithImage("splunk/splunk:9.1.0")
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing image",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithName("test-sts")
			},
			wantErr: true,
			errMsg:  "image is required",
		},
		{
			name: "with certificate",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithName("test-sts").
					WithImage("splunk/splunk:9.1.0").
					WithCertificate(&certificate.Ref{
						SecretName: "my-tls",
						Ready:      true,
					})
			},
			wantErr: false,
			validate: func(t *testing.T, b *StatefulSetBuilder) {
				sts, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				// Check volume created
				found := false
				for _, vol := range sts.Spec.Template.Spec.Volumes {
					if vol.Name == "my-tls" {
						found = true
						break
					}
				}
				if !found {
					t.Error("Certificate volume not created")
				}
				// Check volume mount created
				found = false
				for _, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
					if mount.Name == "my-tls" {
						found = true
						break
					}
				}
				if !found {
					t.Error("Certificate volume mount not created")
				}
			},
		},
		{
			name: "with secret",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithName("test-sts").
					WithImage("splunk/splunk:9.1.0").
					WithSecret(&secret.Ref{
						SecretName: "my-secret",
						Ready:      true,
					})
			},
			wantErr: false,
			validate: func(t *testing.T, b *StatefulSetBuilder) {
				sts, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				// Check volume created
				found := false
				for _, vol := range sts.Spec.Template.Spec.Volumes {
					if vol.Name == "my-secret" {
						found = true
						break
					}
				}
				if !found {
					t.Error("Secret volume not created")
				}
			},
		},
		{
			name: "with labels and annotations",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithName("test-sts").
					WithImage("splunk/splunk:9.1.0").
					WithLabels(map[string]string{
						"custom": "label",
					}).
					WithAnnotations(map[string]string{
						"custom": "annotation",
					})
			},
			wantErr: false,
			validate: func(t *testing.T, b *StatefulSetBuilder) {
				sts, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if sts.Labels["custom"] != "label" {
					t.Error("Custom label not set")
				}
				if sts.Annotations["custom"] != "annotation" {
					t.Error("Custom annotation not set")
				}
			},
		},
		{
			name: "with resources",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithName("test-sts").
					WithImage("splunk/splunk:9.1.0").
					WithResources(corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("2Gi"),
						},
					})
			},
			wantErr: false,
			validate: func(t *testing.T, b *StatefulSetBuilder) {
				sts, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				cpu := sts.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
				if cpu.String() != "500m" {
					t.Errorf("CPU request = %v, want 500m", cpu.String())
				}
			},
		},
		{
			name: "with env vars",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithName("test-sts").
					WithImage("splunk/splunk:9.1.0").
					WithEnv(corev1.EnvVar{Name: "FOO", Value: "bar"}).
					WithEnv(corev1.EnvVar{Name: "BAZ", Value: "qux"})
			},
			wantErr: false,
			validate: func(t *testing.T, b *StatefulSetBuilder) {
				sts, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				env := sts.Spec.Template.Spec.Containers[0].Env
				if len(env) != 2 {
					t.Errorf("Env count = %v, want 2", len(env))
				}
			},
		},
		{
			name: "standard labels applied",
			setupFunc: func(b *StatefulSetBuilder) {
				b.WithName("test-sts").
					WithImage("splunk/splunk:9.1.0")
			},
			wantErr: false,
			validate: func(t *testing.T, b *StatefulSetBuilder) {
				sts, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if sts.Labels["app.kubernetes.io/name"] != "test-sts" {
					t.Error("Standard name label not set")
				}
				if sts.Labels["app.kubernetes.io/managed-by"] != "splunk-operator" {
					t.Error("Standard managed-by label not set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewStatefulSetBuilder("default", "test-owner", nil)
			tt.setupFunc(builder)

			if tt.wantErr {
				_, err := builder.Build()
				if err == nil {
					t.Error("Build() expected error but got none")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Build() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else if tt.validate != nil {
				tt.validate(t, builder)
			}
		})
	}
}

func TestStatefulSetBuilder_Defaults(t *testing.T) {
	builder := NewStatefulSetBuilder("test-ns", "test-owner", nil)

	// Check defaults
	if builder.namespace != "test-ns" {
		t.Errorf("namespace = %v, want test-ns", builder.namespace)
	}
	if builder.ownerName != "test-owner" {
		t.Errorf("ownerName = %v, want test-owner", builder.ownerName)
	}
	if *builder.replicas != 1 {
		t.Errorf("default replicas = %v, want 1", *builder.replicas)
	}
}

func TestStatefulSetBuilder_ChainedCalls(t *testing.T) {
	builder := NewStatefulSetBuilder("default", "test", nil)

	// Test method chaining
	result := builder.
		WithName("test").
		WithImage("test:latest").
		WithReplicas(5).
		WithLabels(map[string]string{"key": "value"})

	if result != builder {
		t.Error("Methods should return builder for chaining")
	}

	sts, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	if sts.Name != "test" {
		t.Errorf("Name not set correctly")
	}
	if *sts.Spec.Replicas != 5 {
		t.Errorf("Replicas not set correctly")
	}
}

// TestStatefulSetBuilder_WithK8sSecret tests K8s Secret volume creation
func TestStatefulSetBuilder_WithK8sSecret(t *testing.T) {
	builder := NewStatefulSetBuilder("default", "test-owner", nil)

	// Add a K8s secret (no CSI field)
	k8sSecret := &secret.Ref{
		SecretName: "postgres-credentials",
		Namespace:  "default",
		Keys:       []string{"username", "password"},
		Ready:      true,
		Provider:   "kubernetes",
	}

	builder.WithName("test-sts").
		WithImage("splunk/splunk:9.1.0").
		WithSecret(k8sSecret)

	sts, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Verify K8s Secret volume was created
	var secretVolume *corev1.Volume
	for i, vol := range sts.Spec.Template.Spec.Volumes {
		if vol.Name == "postgres-credentials" {
			secretVolume = &sts.Spec.Template.Spec.Volumes[i]
			break
		}
	}

	if secretVolume == nil {
		t.Fatal("K8s Secret volume not created")
	}

	// Verify it's a Secret volume (not CSI)
	if secretVolume.Secret == nil {
		t.Error("Volume should have Secret source")
	}

	if secretVolume.Secret.SecretName != "postgres-credentials" {
		t.Errorf("Secret name = %v, want postgres-credentials", secretVolume.Secret.SecretName)
	}

	if secretVolume.CSI != nil {
		t.Error("Volume should not have CSI source for K8s secret")
	}
}

// TestStatefulSetBuilder_WithCSISecret tests CSI Secret volume creation
func TestStatefulSetBuilder_WithCSISecret(t *testing.T) {
	builder := NewStatefulSetBuilder("default", "test-owner", nil)

	// Add a CSI secret
	csiSecret := &secret.Ref{
		SecretName: "vault-secrets",
		Namespace:  "default",
		Keys:       []string{"api-key", "token"},
		Ready:      true,
		Provider:   "csi-vault",
		CSI: &secret.CSIInfo{
			ProviderClass: "standalone-my-splunk-secrets",
			Driver:        "secrets-store.csi.k8s.io",
			MountPath:     "/mnt/secrets/vault",
			Files:         []string{"api-key", "token"},
		},
	}

	builder.WithName("test-sts").
		WithImage("splunk/splunk:9.1.0").
		WithSecret(csiSecret)

	sts, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Verify CSI volume was created
	var csiVolume *corev1.Volume
	for i, vol := range sts.Spec.Template.Spec.Volumes {
		if vol.Name == "vault-secrets" {
			csiVolume = &sts.Spec.Template.Spec.Volumes[i]
			break
		}
	}

	if csiVolume == nil {
		t.Fatal("CSI volume not created")
	}

	// Verify it's a CSI volume (not Secret)
	if csiVolume.CSI == nil {
		t.Error("Volume should have CSI source")
	}

	if csiVolume.CSI.Driver != "secrets-store.csi.k8s.io" {
		t.Errorf("CSI driver = %v, want secrets-store.csi.k8s.io", csiVolume.CSI.Driver)
	}

	if csiVolume.CSI.VolumeAttributes["secretProviderClass"] != "standalone-my-splunk-secrets" {
		t.Errorf("SecretProviderClass = %v, want standalone-my-splunk-secrets",
			csiVolume.CSI.VolumeAttributes["secretProviderClass"])
	}

	if csiVolume.Secret != nil {
		t.Error("Volume should not have Secret source for CSI secret")
	}

	// Verify CSI volume mount was created with correct path
	var csiMount *corev1.VolumeMount
	for i, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == "vault-secrets" {
			csiMount = &sts.Spec.Template.Spec.Containers[0].VolumeMounts[i]
			break
		}
	}

	if csiMount == nil {
		t.Fatal("CSI volume mount not created")
	}

	if csiMount.MountPath != "/mnt/secrets/vault" {
		t.Errorf("Mount path = %v, want /mnt/secrets/vault", csiMount.MountPath)
	}

	if !csiMount.ReadOnly {
		t.Error("CSI mount should be read-only")
	}
}

// TestStatefulSetBuilder_WithHybridSecrets tests both K8s and CSI secrets
func TestStatefulSetBuilder_WithHybridSecrets(t *testing.T) {
	builder := NewStatefulSetBuilder("default", "test-owner", nil)

	// Add both K8s secret and CSI secret
	k8sSecret := &secret.Ref{
		SecretName: "k8s-secret",
		Namespace:  "default",
		Keys:       []string{"password"},
		Ready:      true,
		Provider:   "kubernetes",
	}

	csiSecret := &secret.Ref{
		SecretName: "csi-secret",
		Namespace:  "default",
		Keys:       []string{"api-key"},
		Ready:      true,
		Provider:   "csi-vault",
		CSI: &secret.CSIInfo{
			ProviderClass: "vault-secrets",
			Driver:        "secrets-store.csi.k8s.io",
			MountPath:     "/mnt/secrets",
			Files:         []string{"api-key"},
		},
	}

	builder.WithName("test-sts").
		WithImage("splunk/splunk:9.1.0").
		WithSecret(k8sSecret).
		WithSecret(csiSecret)

	sts, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Should have 2 volumes
	if len(sts.Spec.Template.Spec.Volumes) != 2 {
		t.Errorf("Volume count = %v, want 2", len(sts.Spec.Template.Spec.Volumes))
	}

	// Verify K8s Secret volume exists
	foundK8s := false
	for _, vol := range sts.Spec.Template.Spec.Volumes {
		if vol.Name == "k8s-secret" && vol.Secret != nil {
			foundK8s = true
			break
		}
	}
	if !foundK8s {
		t.Error("K8s Secret volume not found")
	}

	// Verify CSI volume exists
	foundCSI := false
	for _, vol := range sts.Spec.Template.Spec.Volumes {
		if vol.Name == "csi-secret" && vol.CSI != nil {
			foundCSI = true
			break
		}
	}
	if !foundCSI {
		t.Error("CSI volume not found")
	}

	// Should have 1 volume mount (only for CSI)
	if len(sts.Spec.Template.Spec.Containers[0].VolumeMounts) != 1 {
		t.Errorf("VolumeMount count = %v, want 1 (only CSI)", len(sts.Spec.Template.Spec.Containers[0].VolumeMounts))
	}

	// Verify CSI mount exists
	foundCSIMount := false
	for _, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == "csi-secret" {
			foundCSIMount = true
			break
		}
	}
	if !foundCSIMount {
		t.Error("CSI volume mount not found")
	}
}

// TestStatefulSetBuilder_WithCertificate_CustomMountPath tests certificate with custom mount path
func TestStatefulSetBuilder_WithCertificate_CustomMountPath(t *testing.T) {
	builder := NewStatefulSetBuilder("default", "test-owner", nil)

	// Add certificate with custom mount path (/mnt/tls for cert-manager compatibility)
	cert := &certificate.Ref{
		SecretName: "splunk-tls",
		Namespace:  "default",
		Ready:      true,
		Provider:   "cert-manager",
		MountPath:  "/mnt/tls",
	}

	builder.WithName("test-sts").
		WithImage("splunk/splunk:9.1.0").
		WithCertificate(cert)

	sts, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Verify certificate volume was created
	var certVolume *corev1.Volume
	for i, vol := range sts.Spec.Template.Spec.Volumes {
		if vol.Name == "splunk-tls" {
			certVolume = &sts.Spec.Template.Spec.Volumes[i]
			break
		}
	}

	if certVolume == nil {
		t.Fatal("Certificate volume not created")
	}

	if certVolume.Secret == nil {
		t.Error("Volume should have Secret source")
	}

	if certVolume.Secret.SecretName != "splunk-tls" {
		t.Errorf("Secret name = %v, want splunk-tls", certVolume.Secret.SecretName)
	}

	// Verify certificate volume mount with custom path
	var certMount *corev1.VolumeMount
	for i, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == "splunk-tls" {
			certMount = &sts.Spec.Template.Spec.Containers[0].VolumeMounts[i]
			break
		}
	}

	if certMount == nil {
		t.Fatal("Certificate volume mount not created")
	}

	if certMount.MountPath != "/mnt/tls" {
		t.Errorf("MountPath = %v, want /mnt/tls", certMount.MountPath)
	}

	if !certMount.ReadOnly {
		t.Error("Certificate mount should be read-only")
	}
}

// TestStatefulSetBuilder_WithCertificate_DefaultMountPath tests certificate with default mount path
func TestStatefulSetBuilder_WithCertificate_DefaultMountPath(t *testing.T) {
	builder := NewStatefulSetBuilder("default", "test-owner", nil)

	// Add certificate without custom mount path (should use default)
	cert := &certificate.Ref{
		SecretName: "splunk-ca-bundle",
		Namespace:  "default",
		Ready:      true,
		Provider:   "cert-manager",
		// MountPath not specified - should default to /etc/certs/splunk-ca-bundle
	}

	builder.WithName("test-sts").
		WithImage("splunk/splunk:9.1.0").
		WithCertificate(cert)

	sts, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Verify certificate volume mount uses default path
	var certMount *corev1.VolumeMount
	for i, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == "splunk-ca-bundle" {
			certMount = &sts.Spec.Template.Spec.Containers[0].VolumeMounts[i]
			break
		}
	}

	if certMount == nil {
		t.Fatal("Certificate volume mount not created")
	}

	expectedPath := "/etc/certs/splunk-ca-bundle"
	if certMount.MountPath != expectedPath {
		t.Errorf("MountPath = %v, want %v", certMount.MountPath, expectedPath)
	}
}

// TestStatefulSetBuilder_WithMultipleCertificates tests multiple certificates with different paths
func TestStatefulSetBuilder_WithMultipleCertificates(t *testing.T) {
	builder := NewStatefulSetBuilder("default", "test-owner", nil)

	// Add TLS certificate
	tlsCert := &certificate.Ref{
		SecretName: "splunk-tls",
		Namespace:  "default",
		Ready:      true,
		Provider:   "cert-manager",
		MountPath:  "/mnt/tls",
	}

	// Add CA bundle certificate
	caCert := &certificate.Ref{
		SecretName: "splunk-ca-bundle",
		Namespace:  "default",
		Ready:      true,
		Provider:   "cert-manager",
		MountPath:  "/mnt/ca-bundles",
	}

	builder.WithName("test-sts").
		WithImage("splunk/splunk:9.1.0").
		WithCertificate(tlsCert).
		WithCertificate(caCert)

	sts, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() failed: %v", err)
	}

	// Should have 2 volumes
	if len(sts.Spec.Template.Spec.Volumes) != 2 {
		t.Errorf("Volume count = %v, want 2", len(sts.Spec.Template.Spec.Volumes))
	}

	// Should have 2 volume mounts
	if len(sts.Spec.Template.Spec.Containers[0].VolumeMounts) != 2 {
		t.Errorf("VolumeMount count = %v, want 2", len(sts.Spec.Template.Spec.Containers[0].VolumeMounts))
	}

	// Verify TLS certificate mount
	var tlsMount *corev1.VolumeMount
	for i, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == "splunk-tls" {
			tlsMount = &sts.Spec.Template.Spec.Containers[0].VolumeMounts[i]
			break
		}
	}

	if tlsMount == nil {
		t.Fatal("TLS certificate mount not found")
	}

	if tlsMount.MountPath != "/mnt/tls" {
		t.Errorf("TLS MountPath = %v, want /mnt/tls", tlsMount.MountPath)
	}

	// Verify CA bundle mount
	var caMount *corev1.VolumeMount
	for i, mount := range sts.Spec.Template.Spec.Containers[0].VolumeMounts {
		if mount.Name == "splunk-ca-bundle" {
			caMount = &sts.Spec.Template.Spec.Containers[0].VolumeMounts[i]
			break
		}
	}

	if caMount == nil {
		t.Fatal("CA bundle mount not found")
	}

	if caMount.MountPath != "/mnt/ca-bundles" {
		t.Errorf("CA MountPath = %v, want /mnt/ca-bundles", caMount.MountPath)
	}
}
