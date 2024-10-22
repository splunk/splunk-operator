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
	Data SecretData `json:"data,omitempty"`
	Metadata      Metadata `json:"metadata,omitempty"`
}
// VaultResponse represents the structure of a response from a Vault request.
// It includes details such as the request ID, lease ID, lease duration, and
// whether the lease is renewable. It also contains nested Data and Metadata
// structures that hold additional information returned by the Vault.
type VaultResponse struct {
	RequestId     string   `json:"request_id,omitempty"`
	LeaseId       string   `json:"lease_id,omitempty"`
	Renewable     bool     `json:"renewable,omitempty"`
	LeaseDuration int      `json:"lease_duration,omitempty"`
	Data          Data     `json:"data,omitempty"`
}

type VaultError struct {
	Errors []string `json:"errors,omitempty"`
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

	secretPath := vaultSpec.SecretPath
	vaultRole := vaultSpec.Role
	secretKeyToEnv := []string{
		"hec_token",
		"idxc_secret",
		"pass4SymmKey",
		"password",
		"shc_secret",
	}

	// Adding annotations for vault injection
	annotations := map[string]string{
		"vault.hashicorp.com/agent-inject":      "true",
		"vault.hashicorp.com/agent-inject-path": "/mnt/splunk-secrets",
		"vault.hashicorp.com/role":              vaultRole,
	}

	splunkConfig := map[string]interface{}{
		"splunk": map[string]interface{}{
			"hec_disabled":  0,
			"hec_enableSSL": 0,
			"hec_token":     `{{- with secret "secret/data/splunk/hec_token" -}}{{ .Data.data.value }}{{- end }}`,
			"password":      `{{- with secret "secret/data/splunk/password" -}}{{ .Data.data.value }}{{- end }}`,
			"pass4SymmKey":  `{{- with secret "secret/data/splunk/pass4SymmKey" -}}{{ .Data.data.value }}{{- end }}`,
			"idxc": map[string]interface{}{
				"secret": `{{- with secret "secret/data/splunk/idxc_secret" -}}{{ .Data.data.value }}{{- end }}`,
			},
			"shc": map[string]interface{}{
				"secret": `{{- with secret "secret/data/splunk/shc_secret" -}}{{ .Data.data.value }}{{- end }}`,
			},
		},
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
		annotationKey := fmt.Sprintf("vault.hashicorp.com/agent-inject-secret-%s", key)
		annotations[annotationKey] = fmt.Sprintf("%s/%s", secretPath, key)
		annotationFile := fmt.Sprintf("vault.hashicorp.com/agent-inject-file-%s", key)
		annotations[annotationFile] = key
		annotationVolumeKey := fmt.Sprintf("vault.hashicorp.com/secret-volume-path-%s", key)
		annotations[annotationVolumeKey] = fmt.Sprintf("/mnt/splunk-secrets/%s", key)
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
func CheckAndRestartStatefulSet(ctx context.Context, kubeClient splcommon.ControllerClient, statefulSet *appsv1.StatefulSet, vaultIntegration *enterpriseApi.VaultIntegration) (error) {

	logger.Info("CheckAndRestartStatefulSet called", "statefulSet", statefulSet.Name, "vaultIntegration", vaultIntegration)

	// Initialize Vault client
	client := resty.New()
	client.SetDebug(true) //FIXME TODO remove once code complete

	// Read the Kubernetes service account token
	tokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		logger.Error(err, "Failed to read service account token")
		return fmt.Errorf("failed to read service account token: %v", err)
	}

	// Authenticate with Vault using the Kubernetes auth method
	data := map[string]interface{}{
		"role": vaultIntegration.Role,
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
			metadataPath = fmt.Sprintf("%smetadata/%s", vaultIntegration.SecretPath, key)
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
	client.SetDebug(true) //FIXME TODO remove once code complete

	// Read the Kubernetes service account token
	tokenFile := "/var/run/secrets/kubernetes.io/serviceaccount/token"
	token, err := os.ReadFile(tokenFile)
	if err != nil {
		logger.Error(err, "Failed to read service account token")
		return "", fmt.Errorf("failed to read service account token: %v", err)
	}

	// Authenticate with Vault using the Kubernetes auth method
	data := map[string]interface{}{
		"role": vaultIntegration.Role,
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
		metadataPath = fmt.Sprintf("%smetadata/%s", vaultIntegration.SecretPath, key)
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

	password := metadataResponse.Data.Data.Value

	return password, nil
}
