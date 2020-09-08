package enterprise

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
)

// GetNamespaceScopedSecret retreives namespace scoped secret
func GetNamespaceScopedSecret(c splcommon.ControllerClient, namespace string) (*corev1.Secret, error) {
	var namespaceScopedSecret corev1.Secret

	// Check if a namespace scoped secret exists
	namespacedName := types.NamespacedName{Namespace: namespace, Name: GetNamespaceScopedSecretName(namespace)}
	err := c.Get(context.TODO(), namespacedName, &namespaceScopedSecret)
	if err != nil {
		// Didn't find it
		return nil, err
	}

	return &namespaceScopedSecret, nil
}

// GetVersionedSecretVersion checks if the secretName includes the versionedSecretIdentifier and if so, extracts the version
func GetVersionedSecretVersion(secretName string, versionedSecretIdentifier string) (int, error) {
	// Extracting version from secret's name
	version := strings.TrimPrefix(secretName, GetVersionedSecretName(versionedSecretIdentifier, ""))

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

// GetExistingLatestVersionedSecret retreives latest EXISTING versionedSecretIdentifier based secret existing currently in the namespace
func GetExistingLatestVersionedSecret(c splcommon.ControllerClient, namespace string, versionedSecretIdentifier string) (*corev1.Secret, int) {
	scopedLog := log.WithName("GetExistingLatestVersionedSecret").WithValues(
		"versionedSecretIdentifier", versionedSecretIdentifier,
		"namespace", namespace)

	// Get list of secrets in K8S cluster
	secretList := corev1.SecretList{}

	// Retreive only secrets only from namespace
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
	}

	err := c.List(context.TODO(), &secretList, listOpts...)
	if err != nil || len(secretList.Items) == 0 {
		scopedLog.Error(err, "Secrets not found in namespace")
		return nil, -1
	}

	// existingLatestVersion holds the version number of the latest versionedSecretIdentifier based secret, if atleast one exists(defaults to -1)
	var existingLatestVersion int = -1

	// existingLatestVersionedSecret holds the latest versionedSecretIdentifier based secret, if atleast one exists
	var existingLatestVersionedSecret corev1.Secret

	// Loop through all secrets in K8S cluster
	for _, secret := range secretList.Items {
		// Check if the secret is based on the versionedSecretIdentifier and extract version
		version, err := GetVersionedSecretVersion(secret.GetName(), versionedSecretIdentifier)
		if err != nil {
			// Secret name not matching required criteria, move onto next one
			continue
		}

		// Version extracted successfully, checking for latest version
		if version > existingLatestVersion {
			// Updating latest version
			existingLatestVersion = version
			existingLatestVersionedSecret = secret
		}
	}

	return &existingLatestVersionedSecret, existingLatestVersion
}

// GetLatestVersionedSecret is used to create/retreive latest versionedSecretIdentifier based secret, cr is optional for owner references(pass nil if not required)
func GetLatestVersionedSecret(c splcommon.ControllerClient, cr splcommon.MetaObject, namespace string, versionedSecretIdentifier string) (*corev1.Secret, error) {
	var latestVersionedSecret *corev1.Secret
	var err error

	// Retreive namespaced scoped secret data in splunk readable format
	splunkReadableData, err := GetSplunkReadableNamespaceScopedSecretData(c, namespace)
	if err != nil {
		return nil, err
	}

	// Get the latest versionedSecretIdentifier based secret, if atleast one exists
	existingLatestVersionedSecret, existingLatestVersion := GetExistingLatestVersionedSecret(c, namespace, versionedSecretIdentifier)

	// Check if there is atleast one versionedSecretIdentifier based secret
	if existingLatestVersion == -1 {
		// No secret based on versionedSecretIdentifier, create one with version v1
		latestVersionedSecret, err = ApplySplunkSecret(c, cr, splunkReadableData, GetVersionedSecretName(versionedSecretIdentifier, firstVersion), namespace)
	} else {
		// Check if contents of latest versionedSecretIdentifier based secret is different from that of namespace scoped secrets object
		if !reflect.DeepEqual(splunkReadableData, existingLatestVersionedSecret.Data) {
			// Different, create a newer version versionedSecretIdentifier based secret
			latestVersionedSecret, err = ApplySplunkSecret(c, cr, splunkReadableData, GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(existingLatestVersion+1)), namespace)
			return latestVersionedSecret, err
		}
		// Latest versionedSecretIdentifier based secret is the the existing latest versionedSecretIdentifier based secret
		latestVersionedSecret = existingLatestVersionedSecret
	}

	return latestVersionedSecret, nil
}

// GetSplunkReadableNamespaceScopedSecretData retreives the namespace scoped secret's data and converts it into Splunk readable format if possible
func GetSplunkReadableNamespaceScopedSecretData(c splcommon.ControllerClient, namespace string) (map[string][]byte, error) {
	// Get namespace scoped secret
	namespaceScopedSecret, err := GetNamespaceScopedSecret(c, namespace)
	if err != nil {
		return nil, err
	}

	for _, tokenType := range GetSplunkSecretTokenTypes() {
		if _, ok := namespaceScopedSecret.Data[tokenType]; !ok {
			return nil, fmt.Errorf(missingTokenError, tokenType)
		}
	}

	// Create data
	splunkReadableData := make(map[string][]byte)

	// Create individual token type data
	for _, tokenType := range GetSplunkSecretTokenTypes() {
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
func ApplySplunkSecret(c splcommon.ControllerClient, cr splcommon.MetaObject, secretData map[string][]byte, secretName string, namespace string) (*corev1.Secret, error) {
	var current corev1.Secret
	var newSecretData map[string][]byte
	var err error

	// Prepare secret data
	if secretData != nil {
		newSecretData = secretData
	} else {
		// If secretData is not specified read from namespace scoped secret
		newSecretData, err = GetSplunkReadableNamespaceScopedSecretData(c, namespace)
		if err != nil {
			return nil, err
		}
	}

	current = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: newSecretData,
	}

	namespacedName := types.NamespacedName{Namespace: namespace, Name: secretName}
	err = c.Get(context.TODO(), namespacedName, &current)
	if err != nil {
		// Set CR as owner if it is passed as a parameter, else ignore
		if cr != nil {
			current.SetOwnerReferences(append(current.GetOwnerReferences(), splcommon.AsOwner(cr)))
		}

		// Didn't find secret, create it
		err = splctrl.CreateResource(c, &current)
		if err != nil {
			return nil, err
		}
	} else {
		if !reflect.DeepEqual(current.Data, newSecretData) {
			// Found the secret, update it
			current.Data = newSecretData
			err = splctrl.UpdateResource(c, &current)
			if err != nil {
				return nil, err
			}
		}
	}

	return &current, nil
}

// ApplyNamespaceScopedSecretObject creates/updates the namespace scoped "splunk-secrets" K8S secret object
func ApplyNamespaceScopedSecretObject(client splcommon.ControllerClient, namespace string) (*corev1.Secret, error) {
	var current corev1.Secret

	// Make data
	current.Data = make(map[string][]byte)

	// Check if a K8S secrets object "splunk-secrets" exists in the namespace
	namespacedName := types.NamespacedName{Namespace: namespace, Name: GetNamespaceScopedSecretName(namespace)}
	err := client.Get(context.TODO(), namespacedName, &current)
	if err == nil {
		// Generate values for only missing types of tokens them
		var updateNeeded bool = false
		for _, tokenType := range GetSplunkSecretTokenTypes() {
			if _, ok := current.Data[tokenType]; !ok {
				// Value for token not found, generate
				if tokenType == "hec_token" {
					current.Data[tokenType] = generateHECToken()
				} else {
					current.Data[tokenType] = splcommon.GenerateSecret(secretBytes, 24)
				}
				updateNeeded = true
			}
		}

		// Updated the secret if needed
		if updateNeeded {
			err = splctrl.UpdateResource(client, &current)
			if err != nil {
				return nil, err
			}
		}

		return &current, nil
	}

	// Not found, update data by generating values for all types of tokens
	for _, tokenType := range GetSplunkSecretTokenTypes() {
		if tokenType == "hec_token" {
			current.Data[tokenType] = generateHECToken()
		} else {
			current.Data[tokenType] = splcommon.GenerateSecret(secretBytes, 24)
		}
	}

	// Set name and namespace
	current.ObjectMeta = metav1.ObjectMeta{
		Name:      GetNamespaceScopedSecretName(namespace),
		Namespace: namespace,
	}

	// Create the secret
	err = splctrl.CreateResource(client, &current)
	if err != nil {
		return nil, err
	}

	return &current, nil
}
