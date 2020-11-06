// Copyright (c) 2018-2020 Splunk Inc. All rights reserved.
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
	"fmt"
	"reflect"
	"strconv"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
)

func TestGetSpecificSecretTokenFromPod(t *testing.T) {
	c := spltest.NewMockClient()

	wantData := []byte{'1'}
	// Create secret
	current := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"key1": wantData,
		},
	}
	err := CreateResource(c, &current)
	if err != nil {
		t.Errorf("Failed to create secret %s", current.GetName())
	}

	// Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					VolumeMounts: []corev1.VolumeMount{
						{
							MountPath: "/mnt/splunk-secrets",
							Name:      "mnt-splunk-secrets",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "mnt-splunk-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secret",
						},
					},
				},
			},
		},
	}
	err = CreateResource(c, pod)
	if err != nil {
		t.Errorf("Failed to create secret %s", current.GetName())
	}

	// Retrieve secret data from Pod
	gotData, err := GetSpecificSecretTokenFromPod(c, pod.GetName(), "test", "key1")
	if err != nil {
		t.Errorf("Couldn't get secret data from pod %s", pod.GetName())
	}

	// Check data
	if !reflect.DeepEqual(gotData, string(wantData)) {
		t.Errorf("Incorrect secret data from pod %s got %+v want %+v", pod.GetName(), gotData, current.Data)
	}

	// Retrieve secret data with empty secret token
	gotData, err = GetSpecificSecretTokenFromPod(c, pod.GetName(), "test", "")
	if err.Error() != emptySecretTokenError {
		t.Errorf("Didn't recognize empty secret token")
	}

	// Retrieve secret data with invalid secret token
	gotData, err = GetSpecificSecretTokenFromPod(c, pod.GetName(), "test", "random")
	if err.Error() != invalidSecretDataError {
		t.Errorf("Didn't recognize invalid secret token")
	}

	// Empty data - Negative testing
	current.Data = nil
	err = UpdateResource(c, &current)
	if err != nil {
		t.Errorf("Couldn't update secret %s", current.GetName())
	}
	// Retrieve secret data from non-existing pod
	gotData, err = GetSpecificSecretTokenFromPod(c, pod.GetName(), "test", "key1")
	if err.Error() != invalidSecretDataError {
		t.Errorf("Didn't recognize nil secret data")
	}
}

func TestGetSecretFromPod(t *testing.T) {
	c := spltest.NewMockClient()

	// Create secret
	current := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"key1": {'1', '2', '3'},
		},
	}
	err := CreateResource(c, &current)
	if err != nil {
		t.Errorf("Failed to create secret %s", current.GetName())
	}

	// Create pod
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "splunk-stack1-0",
			Namespace: "test",
			Labels: map[string]string{
				"controller-revision-hash": "v0",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					VolumeMounts: []corev1.VolumeMount{
						{
							MountPath: "/mnt/splunk-secrets",
							Name:      "mnt-splunk-secrets",
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "mnt-splunk-secrets",
					VolumeSource: corev1.VolumeSource{
						Secret: &corev1.SecretVolumeSource{
							SecretName: "test-secret",
						},
					},
				},
			},
		},
	}
	err = CreateResource(c, pod)
	if err != nil {
		t.Errorf("Failed to create pod %s", pod.GetName())
	}

	// Retrieve secret data from Pod
	gotSecret, err := GetSecretFromPod(c, pod.GetName(), "test")
	if err != nil {
		t.Errorf("Couldn't get secret data from pod %s", pod.GetName())
	}

	// Check data
	if !reflect.DeepEqual(gotSecret.Data, current.Data) {
		t.Errorf("Incorrect secret data from pod %s got %+v want %+v", pod.GetName(), gotSecret.Data, current.Data)
	}

	// Retrieve secret data from non-existing pod
	gotSecret, err = GetSecretFromPod(c, "random", "test")
	if err.Error() != splcommon.PodNotFoundError {
		t.Errorf("Didn't recognize non-existing pod %s", "random")
	}

	// Delete secret
	err = DeleteResource(c, &current)
	if err != nil {
		t.Errorf("Couldn't delete secret %s", current.GetName())
	}

	// Non-existing secret data from non-existing pod
	gotSecret, err = GetSecretFromPod(c, pod.GetName(), "test")
	if err.Error() != splcommon.SecretNotFoundError {
		t.Errorf("Didn't recognize non-existing secret %s", "test")
	}
}

func TestGetSecretLabels(t *testing.T) {
	gotLables := GetSecretLabels()
	wantLables := map[string]string{
		"app.kubernetes.io/managed-by": "splunk-operator",
		"app.kubernetes.io/component":  "versionedSecrets",
	}

	if !reflect.DeepEqual(gotLables, wantLables) {
		t.Errorf("Incorrect labels got %s want %s", gotLables, wantLables)
	}
}

func TestSetSecretOwnerRef(t *testing.T) {
	cr := TestResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()
	current := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test",
		},
		Data: map[string][]byte{
			"key1": {'1', '2', '3'},
		},
	}

	// Negative testing non-existent secret
	err := SetSecretOwnerRef(c, current.GetName(), &cr)
	if err.Error() != "NotFound" {
		t.Errorf("Couldn't detect missing secret %s", current.GetName())
	}

	// Create secret
	err = CreateResource(c, &current)
	if err != nil {
		t.Errorf("Failed to create secret %s", current.GetName())
	}

	// Reset secret owner reference
	//	current.OwnerReferences = nil

	// Test existing secret
	err = SetSecretOwnerRef(c, current.GetName(), &cr)
	if err != nil {
		t.Errorf("Couldn't set owner ref for secret %s", current.GetName())
	}

	removedReferralCount, err := RemoveSecretOwnerRef(c, current.GetName(), &cr)

	if removedReferralCount == 0 || err != nil {
		t.Errorf("Didn't remove owner reference properly. %v", err)
	}
}

func TestRemoveUnwantedSecrets(t *testing.T) {
	var current corev1.Secret
	var err error
	var secretList corev1.SecretList

	versionedSecretIdentifier := "vsi"
	c := spltest.NewMockClient()
	for i := 1; i <= splcommon.MinimumVersionedSecrets; i++ {
		current = corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(i)),
				Namespace: "test",
			},
			Data: make(map[string][]byte),
		}

		err = CreateResource(c, &current)
		if err != nil {
			t.Errorf("Failed to create secret %s", current.GetName())
		}

		// Add to secret list to provide to mock client
		secretList.Items = append(secretList.Items, current)
	}

	// List objects for mock client to pick up
	c.ListObj = &secretList

	// Remove unwanted secrets(length <= MinimumVersionedSecrets), no-op
	err = RemoveUnwantedSecrets(c, versionedSecretIdentifier, "test")
	if err != nil {
		t.Errorf("Failed to remove unwanted secrets")
	}

	// Check that no secret is deleted
	for i := 1; i <= splcommon.MinimumVersionedSecrets; i++ {
		secretName := splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(i))
		namespacedName := types.NamespacedName{Namespace: "test", Name: secretName}
		err := c.Get(context.TODO(), namespacedName, &current)
		if err != nil {
			t.Errorf("Didn't find secret %s, deleted incorrectly", secretName)
		}
	}

	// Add secret of version MinimumVersionedSecrets+1 to enforce delete
	current = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(splcommon.MinimumVersionedSecrets+1)),
			Namespace: "test",
		},
		Data: make(map[string][]byte),
	}

	err = CreateResource(c, &current)
	if err != nil {
		t.Errorf("Failed to create secret %s", current.GetName())
	}
	secretList.Items = append(secretList.Items, current)

	// Add an invalid secret to test error leg
	current = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "random",
			Namespace: "test",
		},
		Data: make(map[string][]byte),
	}

	err = CreateResource(c, &current)
	if err != nil {
		t.Errorf("Failed to create secret %s", current.GetName())
	}
	secretList.Items = append(secretList.Items, current)

	// Remove unwanted secrets, removes v1
	err = RemoveUnwantedSecrets(c, versionedSecretIdentifier, "test")
	if err != nil {
		t.Errorf("Failed to remove unwanted secrets")
	}

	// Check that v1 is deleted & rest are intact. Ignores invalid object
	for i := 1; i <= splcommon.MinimumVersionedSecrets+1; i++ {
		secretName := splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(i))
		namespacedName := types.NamespacedName{Namespace: "test", Name: secretName}
		err := c.Get(context.TODO(), namespacedName, &current)
		if i == 1 {
			if err == nil {
				t.Errorf("Found secret %s, didn't delete unwanted secret", secretName)
			}
		} else if err != nil {
			t.Errorf("Didn't find secret %s, deleted incorrectly", secretName)
		}
	}
}

func TestGetNamespaceScopedSecret(t *testing.T) {
	// Create namespace scoped secret
	c := spltest.NewMockClient()

	// Create namespace scoped secret
	namespacescopedsecret, err := ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	// Reconcile tester
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}

	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := GetNamespaceScopedSecret(c, "test")
		return err
	}
	spltest.ReconcileTester(t, "TestGetNamespaceScopedSecret", nil, nil, createCalls, updateCalls, reconcile, false, namespacescopedsecret)

	// Look for secret in "test" namespace
	retrievedSecret, err := GetNamespaceScopedSecret(c, "test")
	if err != nil {
		t.Errorf("Failed to retrieve secret")
	}

	if !reflect.DeepEqual(namespacescopedsecret, retrievedSecret) {
		t.Errorf("retrieved secret %+v is different from the namespace scoped secret %+v \n", retrievedSecret, retrievedSecret)
	}

	// Negative testing - look for secret in "random" namespace(doesn't exist)
	retrievedSecret, err = GetNamespaceScopedSecret(c, "random")
	if err.Error() != "NotFound" {
		t.Errorf("Failed to detect secret in random namespace")
	}
}

func TestGetVersionedSecretVersion(t *testing.T) {
	var versionedSecretIdentifier, testSecretName string
	versionedSecretIdentifier = "splunk-test"

	// Test v1-10
	for testVersion := 1; testVersion < 10; testVersion++ {
		testSecretName = splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(testVersion))
		version, err := GetVersionedSecretVersion(testSecretName, versionedSecretIdentifier)
		if err != nil {
			t.Errorf("Failed to get versioned Secret for secret %s versionedSecretIdentifier %s", testSecretName, versionedSecretIdentifier)
		}

		if version != testVersion {
			t.Errorf("Incorrect version, got %d, want %d", version, testVersion)
		}
	}

	// Negative testing with version <= 0
	for testVersion := -10; testVersion < 0; testVersion++ {
		testSecretName = splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(testVersion))
		_, err := GetVersionedSecretVersion(testSecretName, versionedSecretIdentifier)
		if err.Error() != lessThanOrEqualToZeroVersionError {
			t.Errorf("Failed to detect incorrect versioning")
		}
	}

	// Negative testing with non-integer version
	for testVersion := 0; testVersion < 10; testVersion++ {
		testSecretName = splcommon.GetVersionedSecretName(versionedSecretIdentifier, string(rune('A'-1+testVersion)))
		_, err := GetVersionedSecretVersion(testSecretName, versionedSecretIdentifier)
		if err.Error() != nonIntegerVersionError {
			t.Errorf("Failed to detect incorrect versioning")
		}
	}

	// Negative testing for non-matching string
	testSecretName = "random_string"
	_, err := GetVersionedSecretVersion(testSecretName, versionedSecretIdentifier)
	if err.Error() != fmt.Sprintf(nonMatchingStringError, testSecretName, versionedSecretIdentifier) {
		t.Errorf("Failed to detect non matching string")
	}
}

func TestGetExistingLatestVersionedSecret(t *testing.T) {
	var secretData map[string][]byte
	versionedSecretIdentifier := "splunk-test"

	c := spltest.NewMockClient()

	// Get newer version
	newversion, err := (strconv.Atoi(splcommon.FirstVersion))
	if err != nil {
		t.Errorf(err.Error())
	}
	newversion++

	// Create secret version v1
	secretv1 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      splcommon.GetVersionedSecretName(versionedSecretIdentifier, splcommon.FirstVersion),
			Namespace: "test",
		},
		Data: secretData,
	}
	err = CreateResource(c, &secretv1)
	if err != nil {
		t.Errorf(err.Error())
	}

	// Create secret v2
	secretv2 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(newversion)),
			Namespace: "test",
		},
		Data: secretData,
	}
	err = CreateResource(c, &secretv2)
	if err != nil {
		t.Errorf(err.Error())
	}

	// List objects for mock client to pick up
	c.ListObj = &corev1.SecretList{
		Items: []corev1.Secret{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      splcommon.GetVersionedSecretName(versionedSecretIdentifier, splcommon.FirstVersion),
					Namespace: "test",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      splcommon.GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(newversion)),
					Namespace: "test",
				},
			},
			{
				// Negative testing - mismatched versionedSecretIdentifier
				ObjectMeta: metav1.ObjectMeta{
					Name:      "random-secret",
					Namespace: "test",
				},
			},
		},
	}

	latestVersionSecret, latestVersion, _ := GetExistingLatestVersionedSecret(c, "test", versionedSecretIdentifier, false)
	if latestVersion == -1 {
		t.Errorf("Didn't find secret correctly %d", latestVersion)
	}

	if latestVersion != newversion {
		t.Errorf("Latest version not found correctly got %d want 2", latestVersion)
	}

	if !reflect.DeepEqual(latestVersionSecret, &secretv2) {
		t.Errorf("Retrieve secret not matching latest secret")
	}

	// Negative testing - no secrets in namespace
	newc := spltest.NewMockClient()
	latestVersionSecret, latestVersion, _ = GetExistingLatestVersionedSecret(newc, "test", versionedSecretIdentifier, false)
	if latestVersion != -1 || latestVersionSecret != nil {
		t.Errorf("Didn't detect zero secrets in namespace condition")
	}
}

func TestGetLatestVersionedSecret(t *testing.T) {
	versionedSecretIdentifier := "splunk-test"

	c := spltest.NewMockClient()

	// Get newer version
	newversion, err := (strconv.Atoi(splcommon.FirstVersion))
	if err != nil {
		t.Errorf(err.Error())
	}
	newversion++

	// Create namespace scoped secret
	namespacescopedsecret, err := ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	// Creates v1
	v1Secret, err := GetLatestVersionedSecret(c, nil, "test", versionedSecretIdentifier)
	if err != nil {
		t.Errorf(err.Error())
	}

	if v1Secret.GetName() != "splunk-test-secret-v1" {
		t.Errorf("Wrong version secret, got %s want %s", v1Secret.GetName(), "splunk-test-secret-v1")
	}

	// List objects for mock client
	c.ListObj = &corev1.SecretList{
		Items: []corev1.Secret{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      splcommon.GetVersionedSecretName(versionedSecretIdentifier, splcommon.FirstVersion),
					Namespace: "test",
				},
				Data: v1Secret.Data,
			},
		},
	}

	// Retrieves v1 as there is no change in namespace scoped secret data
	v1SecretRetr, err := GetLatestVersionedSecret(c, nil, "test", versionedSecretIdentifier)
	if err != nil {
		t.Errorf(err.Error())
	}

	if v1SecretRetr.GetName() != "splunk-test-secret-v1" {
		t.Errorf("Incorrect version secret retrieved got %s want %s", v1Secret.GetName(), "splunk-test-secret-v1")
	}

	if !reflect.DeepEqual(v1SecretRetr.Data, v1Secret.Data) {
		t.Errorf("Incorrect data in secret got %+v want %+v", v1SecretRetr.Data, v1Secret.Data)
	}

	// Update namespace scoped secret with new admin password
	namespacescopedsecret.Data["password"] = splcommon.GenerateSecret(splcommon.SecretBytes, 24)
	err = UpdateResource(c, namespacescopedsecret)
	if err != nil {
		t.Errorf(err.Error())
	}

	// Creates v2, due to change in namespace scoped secret data
	v2Secret, err := GetLatestVersionedSecret(c, nil, "test", versionedSecretIdentifier)
	if err != nil {
		t.Errorf(err.Error())
	}

	if v2Secret.GetName() != "splunk-test-secret-v2" {
		t.Errorf("Wrong version secret got %s want %s", v1Secret.GetName(), "splunk-test-secret-v2")
	}
}

func TestGetSplunkReadableNamespaceScopedSecretData(t *testing.T) {
	c := spltest.NewMockClient()

	// Create a fully filled namespace scoped secrets object
	namespacescopedsecret, err := ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	splunkReadableData, err := GetSplunkReadableNamespaceScopedSecretData(c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	for _, tokenType := range splcommon.GetSplunkSecretTokenTypes() {
		if !reflect.DeepEqual(splunkReadableData[tokenType], namespacescopedsecret.Data[tokenType]) {
			t.Errorf("Incorrect data for tokenType %s, got %s, want %s", tokenType, splunkReadableData[tokenType], namespacescopedsecret.Data[tokenType])
		}
	}

	// Re-concile tester
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}

	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := GetSplunkReadableNamespaceScopedSecretData(c, "test")
		return err
	}

	spltest.ReconcileTester(t, "TestGetSplunkReadableNamespaceScopedSecretData", nil, nil, createCalls, updateCalls, reconcile, false, namespacescopedsecret)

	// Negative testing - Update namespace scoped secrets object with data which has hec_token missing
	secretData := make(map[string][]byte)
	for _, tokenType := range splcommon.GetSplunkSecretTokenTypes() {
		if tokenType != "hec_token" {
			secretData[tokenType] = splcommon.GenerateSecret(splcommon.SecretBytes, 24)
		}
	}

	namespacescopedsecret.Data = secretData
	err = UpdateResource(c, namespacescopedsecret)
	if err != nil {
		t.Errorf("Failed to create namespace scoped secret")
	}
}

func TestApplySplunkSecret(t *testing.T) {
	c := spltest.NewMockClient()

	cr := TestResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	versionedSecretIdentifier := "splunk-test"
	secretName := splcommon.GetVersionedSecretName(versionedSecretIdentifier, splcommon.FirstVersion)

	// Create a fully filled namespace scoped secrets object
	namespacescopedsecret, err := ApplyNamespaceScopedSecretObject(c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	// Get namespaced scoped secret data in splunk readable format
	namespacescopedsecretData, err := GetSplunkReadableNamespaceScopedSecretData(c, "test")
	if err != nil {
		t.Errorf(err.Error())
	}

	// Provide secret data
	funcCalls := []spltest.MockFuncCall{
		{MetaName: fmt.Sprintf("*v1.Secret-test-%s", secretName)},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}

	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySplunkSecret(c, cr.(*TestResource), namespacescopedsecretData, secretName, "test")
		return err
	}
	spltest.ReconcileTester(t, "TestApplySplunkSecret", &cr, &cr, createCalls, updateCalls, reconcile, false, namespacescopedsecret)

	// Ignore secret data
	funcCalls = []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: fmt.Sprintf("*v1.Secret-test-%s", secretName)},
	}
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": {funcCalls[1]}}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	reconcile = func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySplunkSecret(c, nil, nil, secretName, "test")
		return err
	}
	spltest.ReconcileTester(t, "TestApplySplunkSecret", &cr, &cr, createCalls, updateCalls, reconcile, false, namespacescopedsecret)

	// Avoid secret data, create a v1 secret to test update
	v1Secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: "test",
		},
		Data: make(map[string][]byte),
	}

	funcCalls = []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
		{MetaName: fmt.Sprintf("*v1.Secret-test-%s", secretName)},
	}
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": {funcCalls[1]}}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	reconcile = func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySplunkSecret(c, nil, nil, secretName, "test")
		return err
	}
	spltest.ReconcileTester(t, "TestApplySplunkSecret", &cr, &cr, createCalls, updateCalls, reconcile, false, namespacescopedsecret, &v1Secret)
}

func TestApplyNamespaceScopedSecretObject(t *testing.T) {
	funcCalls := []spltest.MockFuncCall{
		{MetaName: "*v1.Secret-test-splunk-test-secret"},
	}

	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplyNamespaceScopedSecretObject(c, "test")
		return err
	}

	// "splunk-secrets" object doesn't exist
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}

	spltest.ReconcileTester(t, "TestApplyNamespaceScopedSecretObject", "test", "test", createCalls, updateCalls, reconcile, false)

	// Partially baked "splunk-secrets" object(applies to empty as well)
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Update": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      splcommon.GetNamespaceScopedSecretName("test"),
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":     splcommon.GenerateSecret(splcommon.SecretBytes, 24),
			"pass4Symmkey": splcommon.GenerateSecret(splcommon.SecretBytes, 24),
		},
	}
	spltest.ReconcileTester(t, "TestApplyNamespaceScopedSecretObject", "test", "test", createCalls, updateCalls, reconcile, false, &secret)

	// Fully baked splunk-secrets object
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	secret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      splcommon.GetNamespaceScopedSecretName("test"),
			Namespace: "test",
		},
		Data: map[string][]byte{
			"hec_token":    generateHECToken(),
			"password":     splcommon.GenerateSecret(splcommon.SecretBytes, 24),
			"pass4SymmKey": splcommon.GenerateSecret(splcommon.SecretBytes, 24),
			"idxc_secret":  splcommon.GenerateSecret(splcommon.SecretBytes, 24),
			"shc_secret":   splcommon.GenerateSecret(splcommon.SecretBytes, 24),
		},
	}
	spltest.ReconcileTester(t, "TestApplyNamespaceScopedSecretObject", "test", "test", createCalls, updateCalls, reconcile, false, &secret)
}

func TestGetNamespaceScopedSecretByName(t *testing.T) {
	cr := TestResource{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	c := spltest.NewMockClient()

	_, err := ApplyNamespaceScopedSecretObject(c, "test")
	secretName := splcommon.GetNamespaceScopedSecretName("test")

	secret, err := GetSecretByName(c, &cr, secretName)
	if secret == nil || err != nil {
		t.Errorf(err.Error())
	}
}
