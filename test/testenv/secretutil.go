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
	// "bytes"
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"

	// "strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

//SecretResponse Secret object struct
type SecretResponse struct {
	Data struct {
		HecToken     string `json:"hec_token"`
		IdxcSecret   string `json:"idxc_secret"`
		Pass4SymmKey string `json:"pass4SymmKey"`
		Password     string `json:"password"`
		ShcSecret    string `json:"shc_secret"`
	} `json:"data"`
}

//SecretObject Secret Object structure
var SecretObject = map[string]string{
	"HecToken":         "hec_token",
	"AdminPassword":    "password",
	"IdxcPass4Symmkey": "idxc_secret",
	"ShcPass4Symmkey":  "shc_secret",
	"Pass4SymmKey":     "pass4SymmKey",
}

// DecodeBase64 decodes base64 and returns string
func DecodeBase64(str string) string {
	out, err := b64.StdEncoding.DecodeString(str)
	if err != nil {
		logf.Log.Error(err, "Failed to decode", "string", str)
		return ""
	}
	return string(out)
}

// EncodeBase64 Encodes base64 and returns string
func EncodeBase64(str string) string {
	out := b64.StdEncoding.EncodeToString([]byte(str))
	return out
}

// GetSecretObject Gets the secret object
func GetSecretObject(deployment *Deployment, ns string, secretName string) SecretResponse {
	output, err := exec.Command("kubectl", "get", "secret", secretName, "-n", ns, "-o", "json").Output()
	restResponse := SecretResponse{}
	if err != nil {
		cmd := fmt.Sprintf("kubectl get secret %s -n %s -o json", secretName, ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return restResponse
	}
	// Parse response into response struct
	err = json.Unmarshal([]byte(output), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse response")
		return restResponse
	}
	return restResponse
}

// GetSecretKey Gets the value to specific key from secret object
func GetSecretKey(deployment *Deployment, ns string, key string, secretName string) string {
	restResponse := GetSecretObject(deployment, ns, secretName)
	if restResponse == (SecretResponse{}) {
		return "Not Found"
	}
	logf.Log.Info("Get secret object encoded value", "Secret Name", secretName, "Key", key)
	value := "Invalid Key"
	if key == "hec_token" {
		value = DecodeBase64(restResponse.Data.HecToken)
	}
	if key == "idxc_secret" {
		value = DecodeBase64(restResponse.Data.IdxcSecret)
	}
	if key == "pass4SymmKey" {
		value = DecodeBase64(restResponse.Data.Pass4SymmKey)
	}
	if key == "password" {
		value = DecodeBase64(restResponse.Data.Password)
	}
	if key == "shc_secret" {
		value = DecodeBase64(restResponse.Data.ShcSecret)
	}
	return value
}

//ModifySecretObject Modifies the entire secret object
func ModifySecretObject(deployment *Deployment, data map[string][]byte, ns string, secretName string) bool {
	logf.Log.Info("Modify secret object", "Secret Name", secretName, "Data", data)
	secret := newSecretSpec(ns, secretName, data)
	//Update object using spec
	err := deployment.UpdateCR(secret)
	if err != nil {
		logf.Log.Error(err, "Unable to update secret object")
		return false
	}
	return true
}

//ModifySecretKey Modifies the specific key in secret object
func ModifySecretKey(deployment *Deployment, ns string, key string, value string) bool {
	//Get current config for update
	secretName := fmt.Sprintf(SecretObjectName, ns)
	restResponse := GetSecretObject(deployment, ns, secretName)
	out, err := json.Marshal(restResponse.Data)
	if err != nil {
		logf.Log.Error(err, "Failed to parse response")
		return false
	}
	//Convert object to map for update
	var data map[string][]byte
	err = json.Unmarshal([]byte(out), &data)
	if err != nil {
		logf.Log.Error(err, "Failed to parse response")
		return false
	}
	//Modify data
	data[key] = []byte(value)
	logf.Log.Info("Modify secret object with following: ", "Secret Name", secretName, "Key", key, "Value", value)
	modify := ModifySecretObject(deployment, data, ns, secretName)
	return modify
}

// UpdateSecret Updates the secret object based on SecretResponse Struct
func UpdateSecret(deployment *Deployment, ns string, secretObj SecretResponse) (bool, error) {
	secretName := fmt.Sprintf(SecretObjectName, ns)
	secretDataString, err := json.Marshal(secretObj.Data)
	if err != nil {
		logf.Log.Error(err, "Failed to parse response")
		return false, err
	}
	//Convert object to map for update
	var data map[string][]byte
	err = json.Unmarshal([]byte(secretDataString), &data)
	if err != nil {
		logf.Log.Error(err, "Failed to parse response")
		return false, err
	}
	modify := ModifySecretObject(deployment, data, ns, secretName)
	return modify, err
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
	return string(stdout)
}
