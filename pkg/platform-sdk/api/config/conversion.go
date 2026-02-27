// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//  http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package config

import (
	platformv4 "github.com/splunk/splunk-operator/api/platform/v4"
)

// FromAPIType converts from the public API type to internal SDK type.
// This allows the SDK to remain independent while using the operator-generated API.
func FromAPIType(apiConfig *platformv4.PlatformConfig) *PlatformConfig {
	if apiConfig == nil {
		return nil
	}

	sdkConfig := &PlatformConfig{
		TypeMeta:   apiConfig.TypeMeta,
		ObjectMeta: apiConfig.ObjectMeta,
		Spec: PlatformConfigSpec{
			Secrets: convertSecretsFromAPI(apiConfig.Spec.Secrets),
		},
		Status: PlatformConfigStatus{
			Conditions:         apiConfig.Status.Conditions,
			ObservedGeneration: apiConfig.Status.ObservedGeneration,
		},
	}

	return sdkConfig
}

// ToAPIType converts from internal SDK type to the public API type.
func ToAPIType(sdkConfig *PlatformConfig) *platformv4.PlatformConfig {
	if sdkConfig == nil {
		return nil
	}

	apiConfig := &platformv4.PlatformConfig{
		TypeMeta:   sdkConfig.TypeMeta,
		ObjectMeta: sdkConfig.ObjectMeta,
		Spec: platformv4.PlatformConfigSpec{
			Secrets: convertSecretsToAPI(sdkConfig.Spec.Secrets),
		},
		Status: platformv4.PlatformConfigStatus{
			Conditions:         sdkConfig.Status.Conditions,
			ObservedGeneration: sdkConfig.Status.ObservedGeneration,
		},
	}

	return apiConfig
}

// convertSecretsFromAPI converts API SecretConfig to SDK SecretConfig.
func convertSecretsFromAPI(apiSecrets platformv4.SecretConfig) SecretConfig {
	sdkSecrets := SecretConfig{
		Provider:          apiSecrets.Provider,
		GenerateFallback:  apiSecrets.GenerateFallback,
		VersioningEnabled: apiSecrets.VersioningEnabled,
		VersionsToKeep:    apiSecrets.VersionsToKeep,
	}

	// Convert CSI config if present
	if apiSecrets.CSI != nil {
		sdkSecrets.CSI = &CSISecretConfig{
			Driver:          apiSecrets.CSI.Driver,
			DefaultProvider: apiSecrets.CSI.DefaultProvider,
			MountPath:       apiSecrets.CSI.MountPath,
		}

		// Convert naming config
		if apiSecrets.CSI.Naming != nil {
			sdkSecrets.CSI.Naming = &SecretNamingConfig{
				Pattern: apiSecrets.CSI.Naming.Pattern,
			}
		}

		// Convert Vault config
		if apiSecrets.CSI.Vault != nil {
			sdkSecrets.CSI.Vault = &VaultConfig{
				Address: apiSecrets.CSI.Vault.Address,
				Role:    apiSecrets.CSI.Vault.Role,
			}
		}

		// Convert AWS config
		if apiSecrets.CSI.AWS != nil {
			sdkSecrets.CSI.AWS = &AWSSecretsConfig{
				Region: apiSecrets.CSI.AWS.Region,
			}
		}
	}

	return sdkSecrets
}

// convertSecretsToAPI converts SDK SecretConfig to API SecretConfig.
func convertSecretsToAPI(sdkSecrets SecretConfig) platformv4.SecretConfig {
	apiSecrets := platformv4.SecretConfig{
		Provider:          sdkSecrets.Provider,
		GenerateFallback:  sdkSecrets.GenerateFallback,
		VersioningEnabled: sdkSecrets.VersioningEnabled,
		VersionsToKeep:    sdkSecrets.VersionsToKeep,
	}

	// Convert CSI config if present
	if sdkSecrets.CSI != nil {
		apiSecrets.CSI = &platformv4.CSISecretConfig{
			Driver:          sdkSecrets.CSI.Driver,
			DefaultProvider: sdkSecrets.CSI.DefaultProvider,
			MountPath:       sdkSecrets.CSI.MountPath,
		}

		// Convert naming config
		if sdkSecrets.CSI.Naming != nil {
			apiSecrets.CSI.Naming = &platformv4.SecretNamingConfig{
				Pattern: sdkSecrets.CSI.Naming.Pattern,
			}
		}

		// Convert Vault config
		if sdkSecrets.CSI.Vault != nil {
			apiSecrets.CSI.Vault = &platformv4.VaultConfig{
				Address: sdkSecrets.CSI.Vault.Address,
				Role:    sdkSecrets.CSI.Vault.Role,
			}
		}

		// Convert AWS config
		if sdkSecrets.CSI.AWS != nil {
			apiSecrets.CSI.AWS = &platformv4.AWSSecretsConfig{
				Region: sdkSecrets.CSI.AWS.Region,
			}
		}
	}

	return apiSecrets
}

// TenantConfigFromAPIType converts from the public API TenantConfig type to internal SDK type.
func TenantConfigFromAPIType(apiConfig *platformv4.TenantConfig) *TenantConfig {
	if apiConfig == nil {
		return nil
	}

	sdkConfig := &TenantConfig{
		TypeMeta:   apiConfig.TypeMeta,
		ObjectMeta: apiConfig.ObjectMeta,
		Spec: TenantConfigSpec{
			Secrets:      convertSecretsFromAPI(apiConfig.Spec.Secrets),
			Certificates: convertCertificatesFromAPI(apiConfig.Spec.Certificates),
		},
		Status: TenantConfigStatus{
			Conditions:         apiConfig.Status.Conditions,
			ObservedGeneration: apiConfig.Status.ObservedGeneration,
		},
	}

	return sdkConfig
}

// TenantConfigToAPIType converts from internal SDK TenantConfig type to the public API type.
func TenantConfigToAPIType(sdkConfig *TenantConfig) *platformv4.TenantConfig {
	if sdkConfig == nil {
		return nil
	}

	apiConfig := &platformv4.TenantConfig{
		TypeMeta:   sdkConfig.TypeMeta,
		ObjectMeta: sdkConfig.ObjectMeta,
		Spec: platformv4.TenantConfigSpec{
			Secrets:      convertSecretsToAPI(sdkConfig.Spec.Secrets),
			Certificates: convertCertificatesToAPI(sdkConfig.Spec.Certificates),
		},
		Status: platformv4.TenantConfigStatus{
			Conditions:         sdkConfig.Status.Conditions,
			ObservedGeneration: sdkConfig.Status.ObservedGeneration,
		},
	}

	return apiConfig
}

// convertCertificatesFromAPI converts API CertificatesConfig to SDK CertificateConfig.
func convertCertificatesFromAPI(apiCerts platformv4.CertificatesConfig) CertificateConfig {
	sdkCerts := CertificateConfig{
		Provider: apiCerts.Provider,
	}

	// Note: API has CertManager and UserProvided configs but SDK has IssuerRef and different structure
	// For now, just copy the provider. Full implementation would need more mapping.

	return sdkCerts
}

// convertCertificatesToAPI converts SDK CertificateConfig to API CertificatesConfig.
func convertCertificatesToAPI(sdkCerts CertificateConfig) platformv4.CertificatesConfig {
	apiCerts := platformv4.CertificatesConfig{
		Provider: sdkCerts.Provider,
	}

	// Note: SDK has IssuerRef but API has CertManager config.
	// For now, just copy the provider. Full implementation would need more mapping.

	return apiCerts
}
