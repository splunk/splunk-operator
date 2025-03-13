// Copyright (c) 2018-2022 Splunk Inc. All rights reserved.

//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// 	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific
//
// Package enterprise provides functionality for integrating with Vault and managing secrets for Splunk Enterprise deployments.
//
// This package includes the following key components:
//
// - SecretData: Represents the structure of a secret's data.
// - Data: Wraps SecretData to represent the data field in a Vault response.
// - Metadata: Contains metadata information about a secret, such as creation time, deletion time, and version.
// - VaultResponse: Represents the structure of a response from a Vault request, including request ID, lease details, data, and metadata.
// - VaultError: Represents the structure of an error response from Vault.
//
// Functions:
//
// - InjectVaultSecret: Adds Vault injection annotations to the StatefulSet Pods deployed by the Splunk Operator. It validates the Vault configuration, constructs the necessary annotations, and applies them to the PodTemplateSpec.
// - CheckAndRestartStatefulSet: Checks if the password version in Vault has changed and restarts the StatefulSet if needed. It authenticates with Vault, retrieves secret metadata, compares versions, and updates the StatefulSet annotations to trigger a rolling restart if any secret version has changed.

package client

import (
	"context"

	//"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/go-resty/resty/v2"
	enterpriseApi "github.com/splunk/splunk-operator/api/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/log"

	// Marshal the splunkConfig to JSON
	"gopkg.in/yaml.v2"
)

var logger = log.Log.WithName("vault_setup")

type SecretData struct {
	Value string `json:"value,omitempty"`
}

// Metadata contains metadata information about a secret, such as creation time,
// deletion time, and version. It also includes custom metadata and a flag indicating
// whether the secret has been destroyed.
type Metadata struct {
	CreatedTime    string      `json:"created_time,omitempty"`
	CustomMetadata interface{} `json:"custom_metadata,omitempty"`
	DeletionTime   string      `json:"deletion_time,omitempty"`
	Destroyed      bool        `json:"destroyed,omitempty"`
	Version        int         `json:"version,omitempty"`
}

type Data struct {
	Data     SecretData `json:"data,omitempty"`
	Metadata Metadata   `json:"metadata,omitempty"`
	Value    string     `json:"value,omitempty"`
}

// VaultResponse represents the structure of a response from a Vault request.
// It includes details such as the request ID, lease ID, lease duration, and
// whether the lease is renewable. It also contains nested Data and Metadata
// structures that hold additional information returned by the Vault.
type VaultResponse struct {
	RequestId     string `json:"request_id,omitempty"`
	LeaseId       string `json:"lease_id,omitempty"`
	Renewable     bool   `json:"renewable,omitempty"`
	LeaseDuration int    `json:"lease_duration,omitempty"`
	Data          Data   `json:"data,omitempty"`
	Value         string `json:"value,omitempty"`
}

type VaultError struct {
	Errors []string `json:"errors,omitempty"`
}

// KVSecretsEngineResponse represents the structure of a response from a Vault request on KV Secrets Engine.
type KVOptions struct {
	Version string `json:"version,omitempty"`
}

type KVData struct {
	Options KVOptions `json:"options,omitempty"`
}

type KVSecretsEngineResponse struct {
	Data KVData `json:"data,omitempty"`
}

func InjectVaultSecret(ctx context.Context, client splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, vaultSpec *enterpriseApi.VaultIntegration) error {
	logger.Info("InjectVaultSecret called", "vaultSpec", vaultSpec)

	latestStatefulSet := &appsv1.StatefulSet{}
	namedNamespace := types.NamespacedName{Name: statefulSet.Name, Namespace: statefulSet.Namespace}
	err := client.Get(ctx, namedNamespace, latestStatefulSet)
	if errors.IsNotFound(err) {
		latestStatefulSet = statefulSet
	} else if err != nil {
		logger.Error(err, "Failed to get the latest StatefulSet", "statefulSet", statefulSet.Name)
		return fmt.Errorf("failed to get the latest StatefulSet: %v", err)
	}

	podTemplateSpec := statefulSet.Spec.Template
	if !vaultSpec.Enable {
		logger.Info("Vault integration is disabled")
		return nil
	}

	// Validate if role and secretPath are provided
	if vaultSpec.Role == "" {
		logger.Error(fmt.Errorf("vault role is required when vault is enabled"), "Missing vault role")
		return fmt.Errorf("vault role is required when vault is enabled")
	}
	if vaultSpec.SecretPath == "" {
		logger.Error(fmt.Errorf("vault secretPath is required when vault is enabled"), "Missing vault secretPath")
		return fmt.Errorf("vault secretPath is required when vault is enabled")
	}

	vaultRole := vaultSpec.Role
	secretKeyToEnv := []string{
		"hec_token",
		"idxc_secret",
		"pass4SymmKey",
		"password",
		"shc_secret",
	}

	secretPath := vaultSpec.SecretPath
	if secretPath[len(secretPath)-1] != '/' {
		secretPath = secretPath + "/"
	}

	// Adding annotations for vault injection
	annotations := map[string]string{
		"vault.hashicorp.com/agent-inject":      "true",
		"vault.hashicorp.com/agent-inject-path": "/mnt/splunk-secrets",
		"vault.hashicorp.com/role":              vaultRole,
	}

	kvVersion, err := GetKVSecretsEngineVersionFromVault(ctx, client, vaultSpec)
	if err != nil {
		logger.Error(err, "Failed to read KV secrets engine version from Vault")
		return fmt.Errorf("failed to read KV secrets engine version from Vault: %v", err)
	}

	splunkConfig := map[string]interface{}{
		"splunk": map[string]interface{}{
			"hec_disabled":  0,
			"hec_enableSSL": 0,
			"hec_token":     fmt.Sprintf(`{{- with secret "%shec_token" -}}{{ .Data.value }}{{- end }}`, secretPath),
			"password":      fmt.Sprintf(`{{- with secret "%spassword" -}}{{ .Data.value }}{{- end }}`, secretPath),
			"pass4SymmKey":  fmt.Sprintf(`{{- with secret "%spass4SymmKey" -}}{{ .Data.value }}{{- end }}`, secretPath),
			"idxc": map[string]interface{}{
				"secret": fmt.Sprintf(`{{- with secret "%sidxc_secret" -}}{{ .Data.value }}{{- end }}`, secretPath),
			},
			"shc": map[string]interface{}{
				"secret": fmt.Sprintf(`{{- with secret "%sshc_secret" -}}{{ .Data.value }}{{- end }}`, secretPath),
			},
		},
	}
	if kvVersion == "2" {
		splunkConfig = map[string]interface{}{
			"splunk": map[string]interface{}{
				"hec_disabled":  0,
				"hec_enableSSL": 0,
				"hec_token":     fmt.Sprintf(`{{- with secret "%shec_token" -}}{{ .Data.data.value }}{{- end }}`, secretPath),
				"password":      fmt.Sprintf(`{{- with secret "%spassword" -}}{{ .Data.data.value }}{{- end }}`, secretPath),
				"pass4SymmKey":  fmt.Sprintf(`{{- with secret "%spass4SymmKey" -}}{{ .Data.data.value }}{{- end }}`, secretPath),
				"idxc": map[string]interface{}{
					"secret": fmt.Sprintf(`{{- with secret "%sidxc_secret" -}}{{ .Data.data.value }}{{- end }}`, secretPath),
				},
				"shc": map[string]interface{}{
					"secret": fmt.Sprintf(`{{- with secret "%sshc_secret" -}}{{ .Data.data.value }}{{- end }}`, secretPath),
				},
			},
		}
	}

	splunkConfigYAML, err := yaml.Marshal(splunkConfig)
	if err != nil {
		return err
	}
	// Convert JSON to string for annotation
	splunkConfigString := string(splunkConfigYAML)

	// Adding annotations to indicate specific secrets to be injected as separate files
	// Adding annotation for default configuration file
	annotations["vault.hashicorp.com/agent-inject-file-defaults"] = "default.yml"
	annotations["vault.hashicorp.com/secret-volume-path-defaults"] = "/mnt/splunk-secrets"
	annotations["vault.hashicorp.com/agent-inject-template-defaults"] = splunkConfigString
	for _, key := range secretKeyToEnv {
		annotationKey := fmt.Sprintf("vault.hashicorp.com/agent-inject-template-%s", key)

		if kvVersion == "1" {
			annotations[annotationKey] = fmt.Sprintf("{{- with secret \"%s%s\" -}}{{ .Data.value }}{{- end }}", secretPath, key)
		} else {
			annotations[annotationKey] = fmt.Sprintf("{{- with secret \"%s%s\" -}}{{ .Data.data.value }}{{- end }}", secretPath, key)
		}
		annotationFile := fmt.Sprintf("vault.hashicorp.com/agent-inject-file-%s", key)
		annotations[annotationFile] = key
		annotationVolumeKey := fmt.Sprintf("vault.hashicorp.com/secret-volume-path-%s", key)
		annotations[annotationVolumeKey] = "/mnt/splunk-secrets"
	}

	// Apply these annotations to the StatefulSet PodTemplateSpec without overwriting existing ones
	if podTemplateSpec.ObjectMeta.Annotations == nil {
		podTemplateSpec.ObjectMeta.Annotations = make(map[string]string)
	}
	for key, value := range annotations {
		if existingValue, exists := latestStatefulSet.Spec.Template.ObjectMeta.Annotations[key]; !exists || existingValue == "" {
			podTemplateSpec.ObjectMeta.Annotations[key] = value
		} else {
			podTemplateSpec.ObjectMeta.Annotations[key] = existingValue
		}
	}

	logger.Info("Vault annotations added to PodTemplateSpec", "annotations", annotations)
	return nil
}

// CheckAndRestartStatefulSet checks the versions of specified secrets in Vault and updates the StatefulSet
// annotations to trigger a rolling restart if any secret version has changed.
//
// Parameters:
//   - ctx: The context for the operation.
//   - kubeClient: The Kubernetes client to interact with the cluster.
//   - statefulSet: The StatefulSet to be checked and potentially updated.
//   - vaultIntegration: The Vault integration configuration containing the Vault address, role, and secret path.
//
// Returns:
//   - error: An error if the operation fails, otherwise nil.
//
// The function performs the following steps:
//  1. Initializes a Vault client and reads the Kubernetes service account token.
//  2. Authenticates with Vault using the Kubernetes auth method.
//  3. Iterates over specified keys to check if any secret version has changed in Vault.
//  4. Updates the StatefulSet annotations to trigger a rolling restart if any secret version has changed.
func CheckAndRestartStatefulSet(ctx context.Context, kubeClient splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, vaultIntegration *enterpriseApi.VaultIntegration) error {

	logger.Info("CheckAndRestartStatefulSet called", "statefulSet", statefulSet.Name, "vaultIntegration", vaultIntegration)

	// Initialize Vault client
	client := resty.New()
	//client.SetDebug(true) //FIXME TODO remove once code complete

	role := vaultIntegration.Role
	if vaultIntegration.OperatorRole != "" {
		role = vaultIntegration.OperatorRole
	}

	// Read the Kubernetes service account token
	tokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		logger.Error(err, "Failed to read service account token")
		return fmt.Errorf("failed to read service account token: %v", err)
	}

	// Authenticate with Vault using the Kubernetes auth method
	data := map[string]interface{}{
		"role": role,
		"jwt":  string(token),
	}
	var authResponse map[string]interface{}
	resp, err := client.R().
		SetBody(data).
		SetResult(&authResponse).
		Post(fmt.Sprintf("%s/v1/auth/kubernetes/login", vaultIntegration.Address))
	if err != nil {
		logger.Error(err, "Failed to authenticate with Vault")
		return fmt.Errorf("failed to authenticate with Vault: %v", err)
	}
	if resp.StatusCode() != 200 {
		logger.Error(fmt.Errorf("failed to authenticate with Vault"), "Vault authentication failed", "response", resp.String())
		return fmt.Errorf("failed to authenticate with Vault: %v", resp.String())
	}

	// Set the client token after successful authentication
	tokenValue := authResponse["auth"].(map[string]interface{})["client_token"].(string)
	logger.Info("Authenticated with Vault", "client_token", tokenValue)

	// Define the keys to be checked.
	keys := []string{"password", "hec_token", "idxc_secret", "pass4SymmKey", "shc_secret"}

	// Iterate over each specified key and check if any version has changed
	for _, key := range keys {
		// Construct the metadata path for each key
		metadataPath := fmt.Sprintf("%s/%s", vaultIntegration.SecretPath, key)
		if vaultIntegration.SecretPath[len(vaultIntegration.SecretPath)-1] == '/' {
			metadataPath = fmt.Sprintf("%s%s", vaultIntegration.SecretPath, key)
		}

		vaultError := &VaultError{}
		// Read the secret metadata from Vault to get the version
		var metadataResponse VaultResponse
		resp, err := client.R().
			SetHeader("X-Vault-Token", tokenValue).
			SetResult(&metadataResponse).
			SetError(vaultError).
			ForceContentType("application/json").
			Get(fmt.Sprintf("%s/v1/%s", vaultIntegration.Address, metadataPath))
		if err != nil {
			logger.Error(err, "Failed to read secret metadata from Vault", "metadataPath", metadataPath)
			return fmt.Errorf("failed to read secret metadata from Vault: %v", err)
		}
		if resp.StatusCode() != 200 {
			logger.Error(fmt.Errorf("failed to read secret metadata from Vault"), "Vault metadata read failed", "response", vaultError)
			return fmt.Errorf("failed to read secret metadata from Vault: %v", vaultError)
		}

		logger.Info(fmt.Sprintf("Vault data for secret %s: %v", key, metadataResponse))

		version := metadataResponse.Data.Metadata.Version

		// Get the current version from the StatefulSet annotations
		annotationKey := fmt.Sprintf("vault-secret-version-%s", key)

		if statefulSet.Spec.Template.Annotations == nil {
			statefulSet.Spec.Template.Annotations = make(map[string]string)
		}
		statefulSet.Spec.Template.Annotations[annotationKey] = strconv.Itoa(int(version))
	}

	return nil
}

// GetSpecificSecretTokenFromVault retrieves a specific secret token's value from a Pod
func GetSpecificSecretTokenFromVault(ctx context.Context, c splcommon.ControllerClient, vaultIntegration *enterpriseApi.VaultIntegration, secretToken string) (string, error) {
	logger.Info("CheckAndRestartStatefulSet called")

	// Initialize Vault client
	client := resty.New()
	//client.SetDebug(true) //FIXME TODO remove once code complete

	role := vaultIntegration.Role
	if vaultIntegration.OperatorRole != "" {
		role = vaultIntegration.OperatorRole
	}

	// Read the Kubernetes service account token
	tokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		logger.Error(err, "Failed to read service account token")
		return "", fmt.Errorf("failed to read service account token: %v", err)
	}

	// Authenticate with Vault using the Kubernetes auth method
	data := map[string]interface{}{
		"role": role,
		"jwt":  string(token),
	}
	var authResponse map[string]interface{}
	resp, err := client.R().
		SetBody(data).
		SetResult(&authResponse).
		Post(fmt.Sprintf("%s/v1/auth/kubernetes/login", vaultIntegration.Address))
	if err != nil {
		logger.Error(err, "Failed to authenticate with Vault")
		return "", fmt.Errorf("failed to authenticate with Vault: %v", err)
	}
	if resp.StatusCode() != 200 {
		logger.Error(fmt.Errorf("failed to authenticate with Vault"), "Vault authentication failed", "response", resp.String())
		return "", fmt.Errorf("failed to authenticate with Vault: %v", resp.String())
	}

	// Set the client token after successful authentication
	tokenValue := authResponse["auth"].(map[string]interface{})["client_token"].(string)
	logger.Info("Authenticated with Vault", "client_token", tokenValue)

	key := secretToken
	// Construct the metadata path for each key
	metadataPath := fmt.Sprintf("%s/%s", vaultIntegration.SecretPath, key)
	if vaultIntegration.SecretPath[len(vaultIntegration.SecretPath)-1] == '/' {
		metadataPath = fmt.Sprintf("%s%s", vaultIntegration.SecretPath, key)
	}
	vaultError := &VaultError{}
	// Read the secret metadata from Vault to get the version
	var metadataResponse VaultResponse
	resp, err = client.R().
		SetHeader("X-Vault-Token", tokenValue).
		SetResult(&metadataResponse).
		SetError(vaultError).
		ForceContentType("application/json").
		Get(fmt.Sprintf("%s/v1/%s", vaultIntegration.Address, metadataPath))
	if err != nil {
		logger.Error(err, "Failed to read secret metadata from Vault", "metadataPath", metadataPath)
		return "", fmt.Errorf("failed to read secret metadata from Vault: %v", err)
	}
	if resp.StatusCode() != 200 {
		logger.Error(fmt.Errorf("failed to read secret metadata from Vault"), "Vault metadata read failed", "response", vaultError)
		return "", fmt.Errorf("failed to read secret metadata from Vault: %v", vaultError)
	}

	kvVersion, err := GetKVSecretsEngineVersionFromVault(ctx, c, vaultIntegration)
	if err != nil {
		logger.Error(err, "Failed to read KV secrets engine version from Vault")
		return "", fmt.Errorf("failed to read KV secrets engine version from Vault: %v", err)
	}

	password := ""
	if kvVersion == "1" {
		password = metadataResponse.Data.Value
	} else if kvVersion == "2" {
		password = metadataResponse.Data.Data.Value
	}

	return password, nil
}

// GetSpecificSecretTokenVersionFromVault retrieves a specific secret token's value from a Pod
func GetSpecificSecretTokenVersionFromVault(ctx context.Context, c splcommon.ControllerClient, vaultIntegration *enterpriseApi.VaultIntegration, secretToken string) (string, error) {
	logger.Info("CheckAndRestartStatefulSet called")

	// Initialize Vault client
	client := resty.New()
	//client.SetDebug(true) //FIXME TODO remove once code complete

	role := vaultIntegration.Role
	if vaultIntegration.OperatorRole != "" {
		role = vaultIntegration.OperatorRole
	}

	// Read the Kubernetes service account token
	tokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		logger.Error(err, "Failed to read service account token")
		return "", fmt.Errorf("failed to read service account token: %v", err)
	}

	// Authenticate with Vault using the Kubernetes auth method
	data := map[string]interface{}{
		"role": role,
		"jwt":  string(token),
	}
	var authResponse map[string]interface{}
	resp, err := client.R().
		SetBody(data).
		SetResult(&authResponse).
		Post(fmt.Sprintf("%s/v1/auth/kubernetes/login", vaultIntegration.Address))
	if err != nil {
		logger.Error(err, "Failed to authenticate with Vault")
		return "", fmt.Errorf("failed to authenticate with Vault: %v", err)
	}
	if resp.StatusCode() != 200 {
		logger.Error(fmt.Errorf("failed to authenticate with Vault"), "Vault authentication failed", "response", resp.String())
		return "", fmt.Errorf("failed to authenticate with Vault: %v", resp.String())
	}

	// Set the client token after successful authentication
	tokenValue := authResponse["auth"].(map[string]interface{})["client_token"].(string)
	logger.Info("Authenticated with Vault", "client_token", tokenValue)

	key := secretToken
	// Construct the metadata path for each key
	metadataPath := fmt.Sprintf("%s/%s", vaultIntegration.SecretPath, key)
	if vaultIntegration.SecretPath[len(vaultIntegration.SecretPath)-1] == '/' {
		metadataPath = fmt.Sprintf("%s%s", vaultIntegration.SecretPath, key)
	}
	vaultError := &VaultError{}
	// Read the secret metadata from Vault to get the version
	var metadataResponse VaultResponse
	resp, err = client.R().
		SetHeader("X-Vault-Token", tokenValue).
		SetResult(&metadataResponse).
		SetError(vaultError).
		ForceContentType("application/json").
		Get(fmt.Sprintf("%s/v1/%s", vaultIntegration.Address, metadataPath))
	if err != nil {
		logger.Error(err, "Failed to read secret metadata from Vault", "metadataPath", metadataPath)
		return "", fmt.Errorf("failed to read secret metadata from Vault: %v", err)
	}
	if resp.StatusCode() != 200 {
		logger.Error(fmt.Errorf("failed to read secret metadata from Vault"), "Vault metadata read failed", "response", vaultError)
		return "", fmt.Errorf("failed to read secret metadata from Vault: %v", vaultError)
	}

	version := metadataResponse.Data.Metadata.Version

	return strconv.Itoa(version), nil
}

// GetSpecificSecretTokenVersionFromVault retrieves a specific secret token's value from a Pod
func GetKVSecretsEngineVersionFromVault(ctx context.Context, c splcommon.ControllerClient, vaultIntegration *enterpriseApi.VaultIntegration) (string, error) {
	// Initialize Vault client
	client := resty.New()

	// Assign role to access Vault setup
	role := vaultIntegration.Role
	if vaultIntegration.OperatorRole != "" {
		role = vaultIntegration.OperatorRole
	}

	// Read the service account token
	tokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		logger.Error(err, "Failed to read service account token")
		return "", fmt.Errorf("failed to read service account token: %v", err)
	}

	// Authenticate with Vault using the Kubernetes auth method
	data := map[string]interface{}{
		"role": role,
		"jwt":  string(token),
	}
	var authResponse map[string]interface{}
	resp, err := client.R().
		SetBody(data).
		SetResult(&authResponse).
		Post(fmt.Sprintf("%s/v1/auth/kubernetes/login", vaultIntegration.Address))
	if err != nil {
		logger.Error(err, "Failed to authenticate with Vault")
		return "", fmt.Errorf("failed to authenticate with Vault: %v", err)
	}
	if resp.StatusCode() != 200 {
		logger.Error(fmt.Errorf("failed to authenticate with Vault"), "Vault authentication failed", "response", resp.String())
		return "", fmt.Errorf("failed to authenticate with Vault: %v", resp.String())
	}

	// Set the client token after successful authentication
	tokenValue := authResponse["auth"].(map[string]interface{})["client_token"].(string)
	logger.Info("Authenticated with Vault", "client_token", tokenValue)

	// Construct the metadata path
	metadataPath := fmt.Sprintf("sys/mounts/%s/tune", vaultIntegration.SecretPath)
	if vaultIntegration.SecretPath[len(vaultIntegration.SecretPath)-1] == '/' {
		metadataPath = fmt.Sprintf("sys/mounts/%stune", vaultIntegration.SecretPath)
	}

	// Read the KV Secrets Engine Version from Vault
	vaultError := &VaultError{}
	var metadataResponse KVSecretsEngineResponse
	resp, err = client.R().
		SetHeader("X-Vault-Token", tokenValue).
		SetResult(&metadataResponse).
		SetError(vaultError).
		ForceContentType("application/json").
		Get(fmt.Sprintf("%s/v1/%s", vaultIntegration.Address, metadataPath))
	if err != nil {
		logger.Error(err, "Failed to read KV Secrets Engine info from Vault", "metadataPath", metadataPath)
		return "", fmt.Errorf("failed to read KV Secrets Engine info from Vault: %v", err)
	}
	if resp.StatusCode() != 200 {
		logger.Error(fmt.Errorf("failed to read KV Secrets Engine info from Vault"), "Vault KV Secrets Engine info read failed", "response", vaultError)
		return "", fmt.Errorf("failed to read KV Secrets Engine info from Vault: %v", vaultError)
	}

	kvVersion := metadataResponse.Data.Options.Version
	if kvVersion == "" {
		kvVersion = "1"
	}

	return kvVersion, nil
}
