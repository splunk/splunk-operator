package enterprise

import (
	"fmt"
	"reflect"
	"strconv"
	"testing"

	enterprisev1 "github.com/splunk/splunk-operator/pkg/apis/enterprise/v1alpha3"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splctrl "github.com/splunk/splunk-operator/pkg/splunk/controller"
	spltest "github.com/splunk/splunk-operator/pkg/splunk/test"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

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
	retreivedSecret, err := GetNamespaceScopedSecret(c, "test")
	if err != nil {
		t.Errorf("Failed to retreive secret")
	}

	if !reflect.DeepEqual(namespacescopedsecret, retreivedSecret) {
		t.Errorf("Retreived secret %+v is different from the namespace scoped secret %+v \n", retreivedSecret, retreivedSecret)
	}

	// Negative testing - look for secret in "random" namespace(doesn't exist)
	retreivedSecret, err = GetNamespaceScopedSecret(c, "random")
	if err.Error() != "NotFound" {
		t.Errorf("Failed to detect secret in random namespace")
	}
}

func TestGetVersionedSecretVersion(t *testing.T) {
	var versionedSecretIdentifier, testSecretName string
	versionedSecretIdentifier = "splunk-test"

	// Test v1-10
	for testVersion := 1; testVersion < 10; testVersion++ {
		testSecretName = GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(testVersion))
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
		testSecretName = GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(testVersion))
		_, err := GetVersionedSecretVersion(testSecretName, versionedSecretIdentifier)
		if err.Error() != lessThanOrEqualToZeroVersionError {
			t.Errorf("Failed to detect incorrect versioning")
		}
	}

	// Negative testing with non-integer version
	for testVersion := 0; testVersion < 10; testVersion++ {
		testSecretName = GetVersionedSecretName(versionedSecretIdentifier, string('A'-1+testVersion))
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
	newversion, err := (strconv.Atoi(firstVersion))
	if err != nil {
		t.Errorf(err.Error())
	}
	newversion++

	// Create secret version v1
	secretv1 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetVersionedSecretName(versionedSecretIdentifier, firstVersion),
			Namespace: "test",
		},
		Data: secretData,
	}
	err = splctrl.CreateResource(c, &secretv1)
	if err != nil {
		t.Errorf(err.Error())
	}

	// Create secret v2
	secretv2 := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(newversion)),
			Namespace: "test",
		},
		Data: secretData,
	}
	err = splctrl.CreateResource(c, &secretv2)
	if err != nil {
		t.Errorf(err.Error())
	}

	// List objects for mock client to pick up
	c.ListObj = &corev1.SecretList{
		Items: []corev1.Secret{
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      GetVersionedSecretName(versionedSecretIdentifier, firstVersion),
					Namespace: "test",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{
					Name:      GetVersionedSecretName(versionedSecretIdentifier, strconv.Itoa(newversion)),
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

	latestVersionSecret, latestVersion := GetExistingLatestVersionedSecret(c, "test", versionedSecretIdentifier)
	if latestVersion == -1 {
		t.Errorf("Didn't find secret correctly %d", latestVersion)
	}

	if latestVersion != newversion {
		t.Errorf("Latest version not found correctly got %d want 2", latestVersion)
	}

	if !reflect.DeepEqual(latestVersionSecret, &secretv2) {
		t.Errorf("Retreive secret not matching latest secret")
	}

	// Negative testing - no secrets in namespace
	newc := spltest.NewMockClient()
	latestVersionSecret, latestVersion = GetExistingLatestVersionedSecret(newc, "test", versionedSecretIdentifier)
	if latestVersion != -1 || latestVersionSecret != nil {
		t.Errorf("Didn't detect zero secrets in namespace condition")
	}
}

func TestGetLatestVersionedSecret(t *testing.T) {
	versionedSecretIdentifier := "splunk-test"

	c := spltest.NewMockClient()

	// Get newer version
	newversion, err := (strconv.Atoi(firstVersion))
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
					Name:      GetVersionedSecretName(versionedSecretIdentifier, firstVersion),
					Namespace: "test",
				},
				Data: v1Secret.Data,
			},
		},
	}

	// Retreives v1 as there is no change in namespace scoped secret data
	v1SecretRetr, err := GetLatestVersionedSecret(c, nil, "test", versionedSecretIdentifier)
	if err != nil {
		t.Errorf(err.Error())
	}

	if v1SecretRetr.GetName() != "splunk-test-secret-v1" {
		t.Errorf("Incorrect version secret retreived got %s want %s", v1Secret.GetName(), "splunk-test-secret-v1")
	}

	if !reflect.DeepEqual(v1SecretRetr.Data, v1Secret.Data) {
		t.Errorf("Incorrect data in secret got %+v want %+v", v1SecretRetr.Data, v1Secret.Data)
	}

	// Update namespace scoped secret with new admin password
	namespacescopedsecret.Data["password"] = splcommon.GenerateSecret(secretBytes, 24)
	err = splctrl.UpdateResource(c, namespacescopedsecret)
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

	// Negative testing - wrong namespace
	_, err = GetLatestVersionedSecret(c, nil, "random", versionedSecretIdentifier)
	if err.Error() != "NotFound" {
		t.Errorf(err.Error())
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

	for _, tokenType := range GetSplunkSecretTokenTypes() {
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
	for _, tokenType := range GetSplunkSecretTokenTypes() {
		if tokenType != "hec_token" {
			secretData[tokenType] = splcommon.GenerateSecret(secretBytes, 24)
		}
	}

	namespacescopedsecret.Data = secretData
	err = splctrl.UpdateResource(c, namespacescopedsecret)
	if err != nil {
		t.Errorf("Failed to create namespace scoped secret")
	}

	// Check for missing hec_token error
	splunkReadableData, err = GetSplunkReadableNamespaceScopedSecretData(c, "test")
	if err.Error() != fmt.Sprintf(missingTokenError, "hec_token") {
		t.Errorf("Failed to detect missing tokenType hec_token")
	}
}

func TestApplySplunkSecret(t *testing.T) {
	c := spltest.NewMockClient()

	versionedSecretIdentifier := "splunk-test"
	secretName := GetVersionedSecretName(versionedSecretIdentifier, firstVersion)

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

	// Ignore owner reference, provide secret data
	funcCalls := []spltest.MockFuncCall{
		{MetaName: fmt.Sprintf("*v1.Secret-test-%s", secretName)},
	}
	createCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls := map[string][]spltest.MockFuncCall{"Get": funcCalls}

	reconcile := func(c *spltest.MockClient, cr interface{}) error {
		_, err := ApplySplunkSecret(c, nil, namespacescopedsecretData, secretName, "test")
		return err
	}
	spltest.ReconcileTester(t, "TestApplySplunkSecret", nil, nil, createCalls, updateCalls, reconcile, false, namespacescopedsecret)

	// Ignore owner reference and secret data
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
	spltest.ReconcileTester(t, "TestApplySplunkSecret", nil, nil, createCalls, updateCalls, reconcile, false, namespacescopedsecret)

	// Ignore owner reference, avoid secret data, create a v1 secret to test update
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
	spltest.ReconcileTester(t, "TestApplySplunkSecret", nil, nil, createCalls, updateCalls, reconcile, false, namespacescopedsecret, &v1Secret)

	// Provide owner reference and secret data
	funcCalls = []spltest.MockFuncCall{
		{MetaName: fmt.Sprintf("*v1.Secret-test-%s", secretName)},
	}
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls, "Create": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	searchHeadCR := enterprisev1.SearchHeadCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "SearcHead",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "stack1",
			Namespace: "test",
		},
	}

	reconcile = func(c *spltest.MockClient, cr interface{}) error {
		obj := cr.(*enterprisev1.SearchHeadCluster)
		_, err := ApplySplunkSecret(c, obj, namespacescopedsecret.Data, secretName, "test")
		return err
	}
	spltest.ReconcileTester(t, "TestApplySplunkSecret", &searchHeadCR, &searchHeadCR, createCalls, updateCalls, reconcile, false, namespacescopedsecret)

	// Manul Testing - Test a scenario with namespace scoped secret data missing required secret tokens
	namespacescopedsecret.Data = make(map[string][]byte)
	err = splctrl.UpdateResource(c, namespacescopedsecret)
	if err != nil {
		t.Errorf(err.Error())
	}

	_, err = ApplySplunkSecret(c, nil, nil, secretName, "test")
	if err.Error() != fmt.Sprintf(missingTokenError, "hec_token") {
		t.Errorf("Didn't identify missing token namespace scoped secret")
	}
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
			Name:      GetNamespaceScopedSecretName("test"),
			Namespace: "test",
		},
		Data: map[string][]byte{
			"password":     splcommon.GenerateSecret(secretBytes, 24),
			"pass4Symmkey": splcommon.GenerateSecret(secretBytes, 24),
		},
	}
	spltest.ReconcileTester(t, "TestApplyNamespaceScopedSecretObject", "test", "test", createCalls, updateCalls, reconcile, false, &secret)

	// Fully baked splunk-secrets object
	createCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}
	updateCalls = map[string][]spltest.MockFuncCall{"Get": funcCalls}

	secret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GetNamespaceScopedSecretName("test"),
			Namespace: "test",
		},
		Data: map[string][]byte{
			"hec_token":    generateHECToken(),
			"password":     splcommon.GenerateSecret(secretBytes, 24),
			"pass4SymmKey": splcommon.GenerateSecret(secretBytes, 24),
			"idxc_secret":  splcommon.GenerateSecret(secretBytes, 24),
			"shc_secret":   splcommon.GenerateSecret(secretBytes, 24),
		},
	}
	spltest.ReconcileTester(t, "TestApplyNamespaceScopedSecretObject", "test", "test", createCalls, updateCalls, reconcile, false, &secret)
}
