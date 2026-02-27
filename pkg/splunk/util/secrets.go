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
// See the License for the specific language governing permissions and
// limitations under the License.

package util

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/splunk/splunk-operator/pkg/logging"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetSpecificSecretTokenFromPod retrieves a specific secret token's value from a Pod
func GetSpecificSecretTokenFromPod(ctx context.Context, c splcommon.ControllerClient, PodName string, namespace string, secretToken string) (string, error) {
	logger := logging.FromContext(ctx).With("func", "GetSpecificSecretTokenFromPod")
	logger.DebugContext(ctx, "Retrieving secret token from pod",
		"pod", PodName,
		"namespace", namespace,
		"token_key", secretToken)

	// Get Pod data
	secret, err := GetSecretFromPod(ctx, c, PodName, namespace)
	if err != nil {
		return "", err
	}

	if secret.Data == nil {
		logger.WarnContext(ctx, "Secret has nil data. Update secret with required data",
			"secret_name", secret.Name,
			"namespace", namespace)
		return "", errors.New(invalidSecretDataError)
	}

	if len(secretToken) == 0 {
		return "", errors.New(emptySecretTokenError)
	}

	if _, ok := secret.Data[secretToken]; !ok {
		logger.WarnContext(ctx, "Secret is missing required field. Update secret with required data",
			"secret_name", secret.Name,
			"namespace", namespace,
			"missing_field", secretToken)
		return "", errors.New(invalidSecretDataError)
	}

	return string(secret.Data[secretToken]), nil
}

var GetSpecificSecretTokenFromPodMock = GetSpecificSecretTokenFromPod

// GetSecretFromPod retrieves secret data from a pod
func GetSecretFromPod(ctx context.Context, c splcommon.ControllerClient, PodName string, namespace string) (*corev1.Secret, error) {
	var currentPod corev1.Pod
	var currentSecret corev1.Secret
	var secretName string

	logger := slog.With("func", "GetSecretFromPod")
	logger.DebugContext(ctx, "Retrieving secret from pod",
		"pod", PodName,
		"namespace", namespace)

	// Get Pod
	namespacedName := types.NamespacedName{Namespace: namespace, Name: PodName}
	err := c.Get(ctx, namespacedName, &currentPod)
	if err != nil {
		logger.WarnContext(ctx, "Pod not found",
			"pod", PodName,
			"namespace", namespace,
			"error", err)
		return nil, errors.New(splcommon.PodNotFoundError)
	}

	// Get Pod Spec Volumes
	podSpecVolumes := currentPod.Spec.Volumes
	if len(podSpecVolumes) == 0 {
		logger.WarnContext(ctx, "Pod has no volumes configured",
			"pod", PodName,
			"namespace", namespace)
		return nil, errors.New("empty pod spec volumes")
	}

	var found bool = false
	for i := range podSpecVolumes {
		if podSpecVolumes[i].Name == "mnt-splunk-secrets" && podSpecVolumes[i].VolumeSource.Size() > 0 {
			secretName = podSpecVolumes[i].VolumeSource.Secret.SecretName
			if len(secretName) > 0 {
				found = true
			}
			break
		}
	}

	// Check if we find the secret
	if !found {
		return nil, errors.New("didn't find secret volume source in any pod volume")
	}

	// Retrieve the secret
	namespacedName = types.NamespacedName{Namespace: namespace, Name: secretName}
	err = c.Get(ctx, namespacedName, &currentSecret)
	if err != nil {
		logger.WarnContext(ctx, "Secret not found for pod. Create secret to proceed",
			"secret_name", secretName,
			"pod", PodName,
			"namespace", namespace)
		return nil, errors.New(splcommon.SecretNotFoundError)
	}

	return &currentSecret, nil
}

// GetSecretLabels gets the labels for a secret
func GetSecretLabels() map[string]string {
	labels, _ := splcommon.GetLabels("versionedSecrets", "", "", "", []string{
		"manager", "component",
	})
	return labels
}

// SetSecretOwnerRef sets owner references for object
func SetSecretOwnerRef(ctx context.Context, client splcommon.ControllerClient, secretObjectName string, cr splcommon.MetaObject) error {
	var err error

	secret, err := GetSecretByName(ctx, client, cr.GetNamespace(), secretObjectName)
	if err != nil {
		return err
	}

	currentOwnerRef := secret.GetOwnerReferences()
	// Check if owner ref exists
	for i := 0; i < len(currentOwnerRef); i++ {
		if reflect.DeepEqual(currentOwnerRef[i].UID, cr.GetUID()) {
			return nil
		}
	}

	// Owner ref doesn't exist, update secret with owner references
	secret.SetOwnerReferences(append(secret.GetOwnerReferences(), splcommon.AsOwner(cr, false)))

	// Update secret if needed
	err = UpdateResource(ctx, client, secret)
	return err
}

// RemoveSecretOwnerRef removes the owner references for an object
func RemoveSecretOwnerRef(ctx context.Context, client splcommon.ControllerClient, secretObjectName string, cr splcommon.MetaObject) (uint, error) {
	var err error
	var refCount uint = 0

	secret, err := GetSecretByName(ctx, client, cr.GetNamespace(), secretObjectName)
	if err != nil {
		return 0, err
	}

	ownerRef := secret.GetOwnerReferences()
	for i := 0; i < len(ownerRef); i++ {
		if reflect.DeepEqual(ownerRef[i], splcommon.AsOwner(cr, false)) {
			ownerRef = append(ownerRef[:i], ownerRef[i+1:]...)
			refCount++
		}
	}

	// Update the modified owner reference list
	if refCount > 0 {
		secret.SetOwnerReferences(ownerRef)
		err = UpdateResource(ctx, client, secret)
		if err != nil {
			return 0, err
		}
	}

	return refCount, nil
}

// RemoveUnwantedSecrets deletes all secrets whose version precedes (latestVersion - MinimumVersionedSecrets)
func RemoveUnwantedSecrets(ctx context.Context, c splcommon.ControllerClient, versionedSecretIdentifier, namespace string) error {
	logger := slog.With("func", "RemoveUnwantedSecrets")
	// retrieve the list of versioned namespace scoped secrets
	_, latestVersion, list := GetExistingLatestVersionedSecret(ctx, c, namespace, versionedSecretIdentifier, true)
	if latestVersion != -1 {
		// Check length of list and bail out
		if len(list) <= splcommon.MinimumVersionedSecrets {
			return nil
		}

		// Atleast one exists
		for version, secret := range list {
			if (latestVersion - version) >= splcommon.MinimumVersionedSecrets {
				// Delete secret
				err := DeleteResource(ctx, c, &secret)
				if err != nil {
					logger.ErrorContext(ctx, "Failed to delete old versioned secret",
						"secret_name", secret.GetName(),
						"version", version,
						"namespace", namespace,
						"error", err)
					return err
				}
				logger.InfoContext(ctx, "Deleted old versioned secret",
					"secret_name", secret.GetName(),
					"version", version,
					"latest_version", latestVersion,
					"namespace", namespace)
			}
		}
	}

	return nil
}

// GetNamespaceScopedSecret retrieves namespace scoped secret
func GetNamespaceScopedSecret(ctx context.Context, c splcommon.ControllerClient, namespace string) (*corev1.Secret, error) {
	var namespaceScopedSecret corev1.Secret

	logger := slog.With("func", "GetNamespaceScopedSecret")
	name := splcommon.GetNamespaceScopedSecretName(namespace)
	logger.DebugContext(ctx, "Retrieving namespace-scoped secret",
		"secret_name", name,
		"namespace", namespace)

	namespacedName := types.NamespacedName{Namespace: namespace, Name: name}
	err := c.Get(ctx, namespacedName, &namespaceScopedSecret)
	if err != nil {
		logger.WarnContext(ctx, "Namespace-scoped secret not found. Create secret to proceed",
			"secret_name", name,
			"namespace", namespace,
			"error", err)
		return nil, err
	}

	return &namespaceScopedSecret, nil
}

// GetVersionedSecretVersion checks if the secretName includes the versionedSecretIdentifier and if so, extracts the version
func GetVersionedSecretVersion(secretName string, versionedSecretIdentifier string) (int, error) {
	// Extracting version from secret's name
	version := strings.TrimPrefix(secretName, splcommon.GetVersionedSecretName(versionedSecretIdentifier, ""))

	// Check if the secretName includes the versionedSecretIdentifier
	if version != secretName {
		// Includes, version extracted, check if version number is valid
		versionInt, err := strconv.Atoi(version)
		if err != nil {
			return -1, errors.New(nonIntegerVersionError)
		}

		// Versions should be > 0
		if versionInt <= 0 {
			return -1, errors.New(lessThanOrEqualToZeroVersionError)
		}

		return versionInt, nil
	}

	// Secret name not matching required criteria
	return -1, fmt.Errorf(nonMatchingStringError, secretName, versionedSecretIdentifier)
}

// GetExistingLatestVersionedSecret retrieves latest EXISTING versionedSecretIdentifier based secret existing currently in the namespace
func GetExistingLatestVersionedSecret(ctx context.Context, c splcommon.ControllerClient, namespace string, versionedSecretIdentifier string, list bool) (*corev1.Secret, int, map[int]corev1.Secret) {
	logger := slog.With("func", "GetExistingLatestVersionedSecret")
	// Get list of secrets in K8S cluster
	secretList := corev1.SecretList{}

	// Retrieve secret labels
	labels := GetSecretLabels()

	// Retrieve only secrets only from namespace
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		client.MatchingLabels(labels),
	}

	err := c.List(ctx, &secretList, listOpts...)
	if err != nil || len(secretList.Items) == 0 {
		logger.DebugContext(ctx, "No versioned secrets found in namespace",
			"versioned_secret_identifier", versionedSecretIdentifier,
			"namespace", namespace)
		return nil, -1, nil
	}

	// existingLatestVersion holds the version number of the latest versionedSecretIdentifier based secret, if atleast one exists(defaults to -1)
	var existingLatestVersion int = -1

	// existingLatestVersionedSecret holds the latest versionedSecretIdentifier based secret, if atleast one exists
	var existingLatestVersionedSecret corev1.Secret

	// map of versionedSecretIdentifier based secrets
	secretListRetr := make(map[int]corev1.Secret)

	// Loop through all secrets in K8S cluster
	for _, secret := range secretList.Items {
		// Check if the secret is based on the versionedSecretIdentifier and extract version
		version, err := GetVersionedSecretVersion(secret.GetName(), versionedSecretIdentifier)
		if err != nil {
			// Secret name not matching required criteria, move onto next one
			continue
		}

		// Append to list of secrets if required
		if list {
			secretListRetr[version] = secret
		}

		// Version extracted successfully, checking for latest version
		if version > existingLatestVersion {
			// Updating latest version
			existingLatestVersion = version
			existingLatestVersionedSecret = secret
		}
	}

	return &existingLatestVersionedSecret, existingLatestVersion, secretListRetr
}

// GetLatestVersionedSecret is used to create/retrieve latest versionedSecretIdentifier based secret, cr is optional for owner references(pass nil if not required)
func GetLatestVersionedSecret(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, namespace string, versionedSecretIdentifier string) (*corev1.Secret, error) {
	logger := slog.With("func", "GetLatestVersionedSecret")
	var latestVersionedSecret *corev1.Secret
	var err error

	// Retrieve namespaced scoped secret data in splunk readable format
	splunkReadableData, err := GetSplunkReadableNamespaceScopedSecretData(ctx, c, namespace)
	if err != nil {
		return nil, err
	}

	// Get the latest versionedSecretIdentifier based secret, if atleast one exists
	existingLatestVersionedSecret, existingLatestVersion, _ := GetExistingLatestVersionedSecret(ctx, c, namespace, versionedSecretIdentifier, false)

	// Check if there is atleast one versionedSecretIdentifier based secret
	if existingLatestVersion == -1 {
		// No secret based on versionedSecretIdentifier, create one with version v1
		logger.InfoContext(ctx, "Creating first version secret",
			"versioned_secret_identifier", versionedSecretIdentifier,
			"namespace", namespace)
		latestVersionedSecret, _ = ApplySplunkSecret(ctx, c, cr, splunkReadableData, splcommon.GetVersionedSecretName(versionedSecretIdentifier, splcommon.FirstVersion), namespace)
	} else {
		// Check if contents of latest versionedSecretIdentifier based secret is different from that of namespace scoped secrets object
		if !reflect.DeepEqual(splunkReadableData, existingLatestVersionedSecret.Data) {
			// Different, create a newer version versionedSecretIdentifier based secret
			latestVersionedSecret, err = ApplySplunkSecret(ctx, c, cr, splunkReadableData, splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(existingLatestVersion+1)), namespace)
			if latestVersionedSecret != nil {
				logger.InfoContext(ctx, "Secret version changed",
					"new_secret_name", latestVersionedSecret.GetName(),
					"new_version", existingLatestVersion+1,
					"old_secret_name", existingLatestVersionedSecret.GetName(),
					"old_version", existingLatestVersion,
					"namespace", namespace)
			}
			return latestVersionedSecret, err
		}

		// Latest versionedSecretIdentifier based secret is the the existing latest versionedSecretIdentifier based secret
		latestVersionedSecret = existingLatestVersionedSecret
	}

	return latestVersionedSecret, nil
}

// GetSplunkReadableNamespaceScopedSecretData retrieves the namespace scoped secret's data and converts it into Splunk readable format if possible
func GetSplunkReadableNamespaceScopedSecretData(ctx context.Context, c splcommon.ControllerClient, namespace string) (map[string][]byte, error) {
	// Get namespace scoped secret ensuring all tokens are present
	namespaceScopedSecret, err := ApplyNamespaceScopedSecretObject(ctx, c, namespace)
	if err != nil {
		return nil, err
	}

	// Create data
	splunkReadableData := make(map[string][]byte)

	// Create individual token type data
	for _, tokenType := range splcommon.GetSplunkSecretTokenTypes() {
		splunkReadableData[tokenType] = namespaceScopedSecret.Data[tokenType]
	}

	// Create default.yml
	splunkReadableData["default.yml"] = []byte(fmt.Sprintf(`
splunk:
    hec_disabled: 0
    hec_enableSSL: 0
    hec_token: "%s"
    password: "%s"
    pass4SymmKey: "%s"
    idxc:
        secret: "%s"
    shc:
        secret: "%s"
`,
		namespaceScopedSecret.Data["hec_token"],
		namespaceScopedSecret.Data["password"],
		namespaceScopedSecret.Data["pass4SymmKey"],
		namespaceScopedSecret.Data["idxc_secret"],
		namespaceScopedSecret.Data["shc_secret"]))

	return splunkReadableData, nil
}

// ApplySplunkSecret creates/updates a secret using secretData(which HAS to be of ansible readable format) or namespace scoped secret data if not specified
func ApplySplunkSecret(ctx context.Context, c splcommon.ControllerClient, cr splcommon.MetaObject, secretData map[string][]byte, secretName string, namespace string) (*corev1.Secret, error) {
	logger := slog.With("func", "ApplySplunkSecret")
	var current corev1.Secret
	var newSecretData map[string][]byte
	var err error

	// Prepare secret data
	if secretData != nil {
		newSecretData = secretData
	} else {
		// If secretData is not specified read from namespace scoped secret
		newSecretData, err = GetSplunkReadableNamespaceScopedSecretData(ctx, c, namespace)
		if err != nil {
			return nil, err
		}
	}

	// Retrieve secret labels
	labels := GetSecretLabels()

	current = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels:    labels,
		},
		Data: newSecretData,
	}

	namespacedName := types.NamespacedName{Namespace: namespace, Name: secretName}
	err = c.Get(ctx, namespacedName, &current)
	if err != nil {
		// Set CR as owner if it is passed as a parameter, else ignore
		if cr != nil {
			current.SetOwnerReferences(append(current.GetOwnerReferences(), splcommon.AsOwner(cr, false)))
		}

		// Didn't find secret, create it
		err = CreateResource(ctx, c, &current)
		if err != nil {
			logger.ErrorContext(ctx, "Failed to create secret",
				"secret_name", secretName,
				"namespace", namespace,
				"error", err)
			return nil, err
		}
	} else {
		if !reflect.DeepEqual(current.Data, newSecretData) {
			// Found the secret, update it
			current.Data = newSecretData
			err = UpdateResource(ctx, c, &current)
			if err != nil {
				logger.ErrorContext(ctx, "Failed to update secret",
					"secret_name", secretName,
					"namespace", namespace,
					"error", err)
				return nil, err
			}
		}
	}

	return &current, nil
}

// ApplyNamespaceScopedSecretObject creates/updates the namespace scoped K8S secret object
func ApplyNamespaceScopedSecretObject(ctx context.Context, client splcommon.ControllerClient, namespace string) (*corev1.Secret, error) {
	var current corev1.Secret

	logger := slog.With("func", "ApplyNamespaceScopedSecretObject")
	name := splcommon.GetNamespaceScopedSecretName(namespace)

	// Check if a namespace scoped K8S secrets object exists
	namespacedName := types.NamespacedName{Namespace: namespace, Name: splcommon.GetNamespaceScopedSecretName(namespace)}
	err := client.Get(ctx, namespacedName, &current)
	if err == nil {
		// Generate values for only missing types of tokens them
		var updateNeeded bool = false
		for _, tokenType := range splcommon.GetSplunkSecretTokenTypes() {
			if _, ok := current.Data[tokenType]; !ok {
				logger.WarnContext(ctx, "Secret is missing required field. Update secret with required data",
					"secret_name", name,
					"namespace", namespace,
					"missing_field", tokenType)
				if current.Data == nil || reflect.ValueOf(current.Data).Kind() != reflect.Map {
					current.Data = make(map[string][]byte)
				}
				// Value for token not found, generate
				if tokenType == "hec_token" {
					current.Data[tokenType] = generateHECToken()
				} else {
					current.Data[tokenType] = splcommon.GenerateSecret(splcommon.SecretBytes, 24)
				}
				updateNeeded = true
			}
		}

		// Updated the secret if needed
		if updateNeeded {
			logger.InfoContext(ctx, "Updating namespace-scoped secret with generated token values",
				"secret_name", name,
				"namespace", namespace)
			err = UpdateResource(ctx, client, &current)
			if err != nil {
				return nil, err
			}
		}

		return &current, nil
	} else if err != nil && !k8serrors.IsNotFound(err) {
		logger.ErrorContext(ctx, "Unexpected API error retrieving namespace-scoped secret",
			"secret_name", name,
			"namespace", namespace,
			"error", err)
		return nil, err
	}

	// Make data
	logger.InfoContext(ctx, "Namespace-scoped secret does not exist, creating with new token values",
		"secret_name", name,
		"namespace", namespace)
	current.Data = make(map[string][]byte)
	// Not found, update data by generating values for all types of tokens
	for _, tokenType := range splcommon.GetSplunkSecretTokenTypes() {
		if tokenType == "hec_token" {
			current.Data[tokenType] = generateHECToken()
		} else {
			current.Data[tokenType] = splcommon.GenerateSecret(splcommon.SecretBytes, 24)
		}
	}

	// Set name and namespace
	current.ObjectMeta = metav1.ObjectMeta{
		Name:      splcommon.GetNamespaceScopedSecretName(namespace),
		Namespace: namespace,
	}

	// Create the secret
	err = CreateResource(ctx, client, &current)
	if err != nil {
		return nil, err
	}

	retryCnt := 0
	gerr := client.Get(ctx, namespacedName, &current)
	for ; gerr != nil; gerr = client.Get(ctx, namespacedName, &current) {
		logger.DebugContext(ctx, "Newly created secret not yet in cache, retrying",
			"secret_name", name,
			"namespace", namespace,
			"error", gerr)
		time.Sleep(10 * time.Microsecond)

		// Avoid infinite loop
		retryCnt++
		if retryCnt > 20 {
			return nil, gerr
		}
	}
	return &current, nil
}

// GetSecretByName retrieves namespace scoped secret object for a given name.
func GetSecretByName(ctx context.Context, c splcommon.ControllerClient, namespace string, name string) (*corev1.Secret, error) {
	var namespaceScopedSecret corev1.Secret

	logger := slog.With("func", "GetSecretByName")
	logger.DebugContext(ctx, "Retrieving secret",
		"secret_name", name,
		"namespace", namespace)

	// Check if a namespace scoped secret exists
	namespacedName := types.NamespacedName{Namespace: namespace, Name: name}
	err := c.Get(ctx, namespacedName, &namespaceScopedSecret)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			logger.WarnContext(ctx, "Secret not found. Create secret to proceed",
				"secret_name", name,
				"namespace", namespace)
		} else {
			logger.ErrorContext(ctx, "Failed to retrieve secret",
				"secret_name", name,
				"namespace", namespace,
				"error", err)
		}
		return nil, err
	}

	logger.DebugContext(ctx, "Secret retrieved successfully",
		"secret_name", name,
		"namespace", namespace,
		"resource_version", namespaceScopedSecret.ResourceVersion)

	return &namespaceScopedSecret, nil
}
