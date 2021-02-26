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
	b64 "encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

//SecretResponse Secret object struct
type SecretResponse struct {
	HecToken     string `json:"hec_token"`
	IdxcSecret   string `json:"idxc_secret"`
	Pass4SymmKey string `json:"pass4SymmKey"`
	Password     string `json:"password"`
	ShcSecret    string `json:"shc_secret"`
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
func GetSecretObject(deployment *Deployment, ns string) *SecretResponse {
	secretObjectName := fmt.Sprintf(SecretObject, ns)
	output, err := exec.Command("kubectl", "get", "secret", secretObjectName, "-n", ns, "-o", "jsonpath='{.data}'").Output()
	if err != nil {
		cmd := fmt.Sprintf("kubectl get pods -n %s", ns)
		logf.Log.Error(err, "Failed to execute command", "command", cmd)
		return nil
	}
	// Parse response into response struct
	restResponse := SecretResponse{}
	err = json.Unmarshal([]byte(strings.Trim(string(output), "'")), &restResponse)
	if err != nil {
		logf.Log.Error(err, "Failed to parse response")
		return nil
	}
	return &restResponse
}

// GetSecretKey Gets the value to specific key from secret object
func GetSecretKey(deployment *Deployment, ns string, key string) string {
	restResponse := GetSecretObject(deployment, ns)
	//return key based on request
	switch key {
	case "hec_token":
		key := DecodeBase64(restResponse.HecToken)
		return key
	case "idxc_secret":
		key := DecodeBase64(restResponse.IdxcSecret)
		return key
	case "pass4SymmKey":
		key := DecodeBase64(restResponse.Pass4SymmKey)
		return key
	case "password":
		key := DecodeBase64(restResponse.Password)
		return key
	case "shc_secret":
		key := DecodeBase64(restResponse.ShcSecret)
		return key
	default:
		return "Invalid Key"
	}
}

//ModifySecretObject Modifies the entire secret object
func ModifySecretObject(deployment *Deployment, data map[string][]byte, ns string) bool {
	secretName := fmt.Sprintf(SecretObject, ns)
	secret := newSecretSpec(ns, secretName, data)
	//Update object using spec
	err := deployment.updateCR(secret)
	if err != nil {
		logf.Log.Error(err, "Unable to update secret object")
		return false
	}
	return true

}

//ModifySecretKey Modifies the specific key in secret object
func ModifySecretKey(deployment *Deployment, ns string, key string, value string) bool {
	//Get current config for update
	restResponse := GetSecretObject(deployment, ns)
	out, err := json.Marshal(restResponse)
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
	data[key] = []byte(EncodeBase64(value))
	modify := ModifySecretObject(deployment, data, ns)
	return modify

}
