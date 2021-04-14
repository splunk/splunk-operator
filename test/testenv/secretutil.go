// Copyright (c) 2018-2021 Splunk Inc. All rights reserved.
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

package testenv

import (
	"fmt"
	"os/exec"
	"strings"

	corev1 "k8s.io/api/core/v1"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// SecretKeytoServerConfStanza map to convert secretName to server conf stanza
var SecretKeytoServerConfStanza = map[string]string{
	"shc_secret":   "shclustering",
	"idxc_secret":  "clustering",
	"pass4SymmKey": "general",
	"hec_token":    "http://splunk_hec_token",
}

// SecretAPIRequests map to convert secretName to respective api requests
var SecretAPIRequests = map[string]string{
	"password":  "curl -iks -u %s:%s https://%s:%d/servicesNS/admin/splunk_httpinput/data/inputs/http?output_mode=json",
	"hec_token": "curl -ik -H 'Authorization: %s %s' https://%s:%d/services/collector -d '{\"event\": \"data\", \"sourcetype\": \"manual\"}'",
}

// GetSecretStruct Gets the secret struct for a given k8 secret name.
func GetSecretStruct(deployment *Deployment, ns string, secretName string) (*corev1.Secret, error) {
	secretObject := &corev1.Secret{}
	err := deployment.GetInstance(secretName, secretObject)
	if err != nil {
		deployment.testenv.Log.Error(err, "Unable to get secret object", "Secret Name", secretName, "Namespace", ns)
	}
	return secretObject, err
}

//ModifySecretObject Modifies the secret object with given data
func ModifySecretObject(deployment *Deployment, ns string, secretName string, data map[string][]byte) error {
	logf.Log.Info("Modify secret object", "Secret Name", secretName, "Data", data)
	secret := newSecretSpec(ns, secretName, data)
	err := deployment.UpdateCR(secret)
	if err != nil {
		logf.Log.Error(err, "Unable to update secret object")
	}
	return err
}

//DeleteSecretObject Deletes the entire secret object
func DeleteSecretObject(deployment *Deployment, ns string, secretName string) error {
	logf.Log.Info("Delete secret object", "Secret Name", secretName, "Namespace", ns)
	secret := newSecretSpec(ns, secretName, map[string][]byte{})
	err := deployment.DeleteCR(secret)
	if err != nil {
		logf.Log.Error(err, "Unable to delete secret object")
		return err
	}
	return nil
}

//GetMountedKey Gets the key mounted on pod
func GetMountedKey(deployment *Deployment, podName string, key string) string {
	stdin := fmt.Sprintf("cat /mnt/splunk-secrets/%s", key)
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command)
		return ""
	}
	logf.Log.Info("Key found on pod", "Pod Name", podName, "stdout", stdout, "stderr", stderr)
	return stdout
}

// GetRandomeHECToken generates a random HEC token
func GetRandomeHECToken() string {
	return fmt.Sprintf("%s-%s-%s-%s-%s", strings.ToUpper(RandomDNSName(8)), strings.ToUpper(RandomDNSName(4)), strings.ToUpper(RandomDNSName(4)), strings.ToUpper(RandomDNSName(4)), strings.ToUpper(RandomDNSName(12)))
}

// GetSecretFromServerConf gets give secret from server under given stanza
func GetSecretFromServerConf(deployment *Deployment, podName string, ns string, configName string, stanza string) (string, string, error) {
	filePath := "/opt/splunk/etc/system/local/server.conf"
	confline, err := GetConfLineFromPod(podName, filePath, ns, configName, stanza, true)
	if err != nil {
		logf.Log.Error(err, "Failed to get secret from pod", "Pod Name", podName, "Secret Name", configName)
		return "", "", err
	}

	secretList := strings.Split(confline, "=")
	key := strings.TrimSpace(secretList[0])
	value := DecryptSplunkEncodedSecret(deployment, podName, ns, strings.TrimSpace(secretList[1]))
	return key, value, nil
}

// DecryptSplunkEncodedSecret Decrypt Splunk Secret like pass4SymmKey On Given Pod
func DecryptSplunkEncodedSecret(deployment *Deployment, podName string, ns string, secretValue string) string {
	stdin := fmt.Sprintf("/opt/splunk/bin/splunk show-decrypted --value '%s'", secretValue)
	command := []string{"/bin/sh"}
	stdout, stderr, err := deployment.PodExecCommand(podName, command, stdin, false)
	if err != nil {
		logf.Log.Error(err, "Failed to execute command on pod", "pod", podName, "command", command, "stdin", stdin)
		return "Failed"
	}
	logf.Log.Info("Command executed on pod", "pod", podName, "command", command, "stdin", stdin, "stdout", stdout, "stderr", stderr)

	logf.Log.Info("Decrypted Key Value", "Decrypted Key", stdout)
	return strings.TrimSuffix(stdout, "\n")
}

// GetKeysToMatch retuns slice of secrets in server conf based on pod name
func GetKeysToMatch(podName string) []string {
	var keysToMatch []string
	if strings.Contains(podName, "standalone") || strings.Contains(podName, "license-master") || strings.Contains(podName, "monitoring-console") {
		keysToMatch = []string{"pass4SymmKey"}
	} else if strings.Contains(podName, "indexer") || strings.Contains(podName, "cluster-master") {
		keysToMatch = []string{"pass4SymmKey", "idxc_secret"}
	} else if strings.Contains(podName, "search-head") || strings.Contains(podName, "-deployer-") {
		keysToMatch = []string{"pass4SymmKey", "shc_secret"}
	}
	return keysToMatch
}

// GetVersionedSecretNames retuns list of versioned secrets of given namespace and version
func GetVersionedSecretNames(ns string, version int) []string {
	output, err := exec.Command("kubectl", "get", "secrets", "-n", ns).Output()
	var splunkSecrets []string
	suffix := fmt.Sprintf("v%d", version)
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return nil
	}
	for _, line := range strings.Split(string(output), "\n") {
		logf.Log.Info(line)
		if strings.HasPrefix(line, "splunk") {
			secretName := strings.Fields(line)[0]
			if strings.HasSuffix(secretName, suffix) {
				splunkSecrets = append(splunkSecrets, strings.Fields(line)[0])

			}
		}
	}
	logf.Log.Info("Versioned Secret Objects Found in Namespace", "NameSpace", ns, "Versioned Secrets", splunkSecrets)
	return splunkSecrets
}

// GetSecretDataMap return the map with given secret values
func GetSecretDataMap(hecToken string, password string, pass4SymmKey string, idxcSecret string, shcSecret string) map[string][]byte {
	updatedSecretData := map[string][]byte{
		"hec_token":    []byte(hecToken),
		"password":     []byte(password),
		"pass4SymmKey": []byte(pass4SymmKey),
		"idxc_secret":  []byte(idxcSecret),
		"shc_secret":   []byte(shcSecret),
	}
	return updatedSecretData
}

// CheckSecretViaAPI check if secret (hec token or password) can be used to access api
func CheckSecretViaAPI(deployment *Deployment, podName string, secretName string, secret string) bool {
	var cmd string
	if secretName == "password" {
		cmd = fmt.Sprintf(SecretAPIRequests[secretName], "admin", secret, "localhost", 8089)
	} else if secretName == "hec_token" {
		cmd = fmt.Sprintf(SecretAPIRequests[secretName], "Splunk", secret, "localhost", 8088)
	}
	output, err := ExecuteCommandOnPod(deployment, podName, cmd)
	if err != nil {
		return false
	}
	response := strings.Split(string(output), "\n")
	if strings.Contains(response[0], HTTPCodes["Ok"]) {
		logf.Log.Info("200 OK response found", "pod", podName)
		return true
	}
	return false
}

// GetSecretFromInputsConf gets give secret from server under given stanza
func GetSecretFromInputsConf(deployment *Deployment, podName string, ns string, configName string, stanza string) (string, string, error) {
	filePath := "/opt/splunk/etc/apps/splunk_httpinput/local/inputs.conf"
	confline, err := GetConfLineFromPod(podName, filePath, ns, configName, stanza, true)
	if err != nil {
		logf.Log.Error(err, "Failed to get secret from pod", "Pod Name", podName, "Secret Name", configName)
		return "", "", err
	}
	secretList := strings.Split(confline, "=")
	key := strings.TrimSpace(secretList[0])
	value := strings.TrimSpace(secretList[1])
	return key, value, nil
}
