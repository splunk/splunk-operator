// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

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

package appframework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
)

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	got := shellQuote("admin'password")
	want := `'admin'\''password'`
	if got != want {
		t.Errorf("shellQuote() = %q; want %q", got, want)
	}
}

func TestValidateAppFrameworkSpec(t *testing.T) {
	var err error
	ctx := context.TODO()

	// Point the resolved download path at a unique, not-yet-created directory so the
	// mount-existence check below can be reliably driven between missing and present.
	resolvedVolume := filepath.Join(t.TempDir(), "appdownload")
	defaultVol := operatorResourceTracker.storage.resolvedAppDownloadVolume
	operatorResourceTracker.storage.resolvedAppDownloadVolume = resolvedVolume
	defer func() {
		operatorResourceTracker.storage.resolvedAppDownloadVolume = defaultVol
	}()

	// Valid app framework config
	AppFramework := enterpriseApi.AppFrameworkSpec{
		VolList: []enterpriseApi.VolumeSpec{
			{Name: "msos_s2s3_vol", Endpoint: "https://s3-eu-west-2.amazonaws.com", Path: "testbucket-rs-london", SecretRef: "s3-secret", Type: "s3", Provider: "aws"},
		},
		AppSources: []enterpriseApi.AppSourceSpec{
			{Name: "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "securityApps",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "authenticationApps",
				Location: "authenticationAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}

	appFrameworkContext := enterpriseApi.AppDeploymentContext{
		AppsRepoStatusPollInterval: 60,
	}

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil {
		t.Errorf("App Framework configuration should have returned error as we have not mounted app download volume: %v", err)
	}

	// to pass the validation stage, add the directory to download apps
	err = os.MkdirAll(resolvedVolume, 0755)
	if err != nil {
		t.Errorf("Unable to create download directory for apps :%s", resolvedVolume)
	}

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")

	if err != nil {
		t.Errorf("Valid App Framework configuration should not cause error: %v", err)
	}

	AppFramework.VolList[0].SecretRef = ""
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("Missing Secret Object reference is a valid config that should not cause error: %v", err)
	}
	AppFramework.VolList[0].SecretRef = "s3-secret"

	// App Framework config with missing App Source name
	AppFramework.AppSources[0].Name = ""

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "app Source name is missing for AppSource at:") {

		t.Errorf("Should not accept an app source with missing name ")
	}

	//App Framework config app source config with missing location(withot default location) should errro out
	AppFramework.AppSources[0].Name = "adminApps"
	AppFramework.AppSources[0].Location = ""
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "app Source location is missing for AppSource") {
		t.Errorf("An App Source with missing location should cause an error, when there is no default location configured")
	}
	AppFramework.AppSources[0].Location = "adminAppsRepo"

	// Having defaults volume and location should not complain an app source missing the volume and remote location info.
	AppFramework.Defaults.Scope = enterpriseApi.ScopeCluster
	AppFramework.Defaults.VolName = "msos_s2s3_vol"
	AppFramework.AppSources[0].Scope = ""

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("Should accept an App Source with missing scope, when default scope is configured. But, got the error: %v", err)
	}
	AppFramework.AppSources[0].Location = "adminAppsRepo"
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeLocal

	// Empty App Repo config should not cause an error
	err = ValidateAppFrameworkSpec(ctx, nil, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("App Repo config is optional, should not cause an error. But, got the error: %v", err)
	}

	// Configuring indexes without volume config should return error
	AppFrameworkWithoutVolumeSpec := enterpriseApi.AppFrameworkSpec{
		AppSources: []enterpriseApi.AppSourceSpec{
			{Name: "adminApps",
				Location: "adminAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeCluster},
			},
			{Name: "securityApps",
				Location: "securityAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
			{Name: "authenticationApps",
				Location: "authenticationAppsRepo",
				AppSourceDefaultSpec: enterpriseApi.AppSourceDefaultSpec{
					VolName: "msos_s2s3_vol",
					Scope:   enterpriseApi.ScopeLocal},
			},
		},
	}

	err = ValidateAppFrameworkSpec(ctx, &AppFrameworkWithoutVolumeSpec, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "invalid Volume Name for App Source") {
		t.Errorf("App Repo config without volume details should return error")
	}

	// Defaults with invalid volume reference should return error
	tmpVolume := AppFramework.Defaults.VolName
	AppFramework.Defaults.VolName = "UnknownVolume"

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "invalid Volume Name for Defaults") {
		t.Errorf("Volume referred in the defaults should be a valid volume")
	}
	AppFramework.Defaults.VolName = tmpVolume

	//Duplicate App Source locations should return an error
	tmpVolume = AppFramework.AppSources[1].VolName
	tmpLocation := AppFramework.AppSources[1].Location

	AppFramework.AppSources[1].VolName = AppFramework.AppSources[0].VolName
	AppFramework.AppSources[1].Location = AppFramework.AppSources[0].Location

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "duplicate App Source configured") {
		t.Errorf("Duplicate app sources should return an error")
	}

	// Duplicate App Source locations across different scopes should not return an error
	tmpScope := AppFramework.AppSources[1].Scope
	AppFramework.AppSources[1].Scope = enterpriseApi.ScopeCluster
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("App Sources with different app scopes can have duplicate paths, but failed with error: %v", err)
	}

	AppFramework.AppSources[1].VolName = tmpVolume
	AppFramework.AppSources[1].Location = tmpLocation
	AppFramework.AppSources[1].Scope = tmpScope

	// Duplicate app sources names should cause an error
	tmpAppSourceName := AppFramework.AppSources[1].Name
	AppFramework.AppSources[1].Name = AppFramework.AppSources[0].Name

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "multiple app sources with the name adminApps is not allowed") {
		t.Errorf("Failed to detect duplicate app source names")
	}
	AppFramework.AppSources[1].Name = tmpAppSourceName

	// If the default volume is not configured, then each index should be configured
	// with an explicit volume info. If not, should return an error
	AppFramework.AppSources[0].VolName = ""
	AppFramework.Defaults.VolName = ""

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "volumeName is missing for App Source") {
		t.Errorf("If no default volume, App Source with missing volume info should return an error")
	}

	// If the AppSource doesn't have VolName, and if the defaults have it, shouldn't cause an error
	AppFramework.Defaults.VolName = "msos_s2s3_vol"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("If default volume, App Source with missing volume should not return an error, but got error %v", err)
	}

	// Volume referenced from an index must be a valid volume
	AppFramework.AppSources[0].VolName = "UnknownVolume"

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "invalid Volume Name for App Source") {
		t.Errorf("Index with an invalid volume name should return error")
	}
	AppFramework.AppSources[0].VolName = "msos_s2s3_vol"

	// if the CR supports only local apps, and if the app source scope is not local, should return error
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeCluster
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, true, "")
	if err == nil || !strings.HasPrefix(err.Error(), "invalid scope for App Source") {
		t.Errorf("When called with App scope local, any app sources with the cluster scope should return an error")
	}

	// If the app scope value other than "local" or "cluster" should return an error
	AppFramework.AppSources[0].Scope = "unknown"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("scope for App Source: %s should be either %s or %s or %s", AppFramework.AppSources[0].Name, enterpriseApi.ScopeLocal, enterpriseApi.ScopeCluster, enterpriseApi.ScopePremiumApps)) {
		t.Errorf("Unsupported app scope should be cause error, but failed to detect")
	}

	// If the CR supports only local apps, and default is configured with "cluster" scope, that should be detected
	AppFramework.AppSources[0].Scope, AppFramework.AppSources[1].Scope, AppFramework.AppSources[2].Scope = enterpriseApi.ScopeLocal, enterpriseApi.ScopeLocal, enterpriseApi.ScopeLocal

	AppFramework.Defaults.Scope = enterpriseApi.ScopeCluster

	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, true, "")
	if err == nil || !strings.HasPrefix(err.Error(), "invalid scope for defaults config. Only local scope is supported for this kind of CR") {
		t.Errorf("When called with App scope local, defaults with the cluster scope should return an error")
	}
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeLocal

	// Default scope should be either "local" OR "cluster"
	AppFramework.Defaults.Scope = "unknown"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "scope for defaults should be either local") {
		t.Errorf("Unsupported default scope should be cause error, but failed to detect")
	}
	AppFramework.Defaults.Scope = enterpriseApi.ScopeCluster

	// Missing scope, if the default scope is not specified should return error
	AppFramework.Defaults.Scope = ""
	AppFramework.AppSources[0].Scope = ""
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "app Source scope is missing for") {
		t.Errorf("Missing scope should be detected, but failed")
	}
	AppFramework.Defaults.Scope = enterpriseApi.ScopeLocal
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeLocal

	// Scope clusteWithPreConfig should not return an error

	AppFramework.Defaults.Scope = ""
	AppFramework.AppSources[0].Scope = "clusterWithPreConfig"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("Valid scope clusterWithPreConfig should not cause an error")
	}

	AppFramework.Defaults.Scope = enterpriseApi.ScopeLocal
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopeLocal

	// AppsRepoPollInterval should be in between the minAppsRepoPollInterval and maxAppsRepoPollInterval
	// Default Poll interval
	if splcommon.DefaultAppsRepoPollInterval < splcommon.MinAppsRepoPollInterval || splcommon.DefaultAppsRepoPollInterval > splcommon.MaxAppsRepoPollInterval {
		t.Errorf("defaultAppsRepoPollInterval should be within the range [%d - %d]", splcommon.MinAppsRepoPollInterval, splcommon.MaxAppsRepoPollInterval)
	}

	AppFramework.AppsRepoPollInterval = 0
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("Got error on valid App Framework configuration. Error: %v", err)
	}

	// Check for minAppsRepoPollInterval
	AppFramework.AppsRepoPollInterval = splcommon.MinAppsRepoPollInterval - 1
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("Got error on valid App Framework configuration. Error: %v", err)
	} else if appFrameworkContext.AppsRepoStatusPollInterval != splcommon.MinAppsRepoPollInterval {
		t.Errorf("Spec validation is not able to set the the AppsRepoPollInterval to minAppsRepoPollInterval. AppsRepoStatusPollInterval=%d, expected=%d", appFrameworkContext.AppsRepoStatusPollInterval, splcommon.MinAppsRepoPollInterval)
	}

	// Check for maxAppsRepoPollInterval
	AppFramework.AppsRepoPollInterval = splcommon.MaxAppsRepoPollInterval + 1
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("Got error on valid App Framework configuration. Error: %v", err)
	} else if appFrameworkContext.AppsRepoStatusPollInterval != splcommon.MaxAppsRepoPollInterval {
		t.Errorf("Spec validation is not able to set the the AppsRepoPollInterval to maxAppsRepoPollInterval. AppsRepoStatusPollInterval=%d, expected=%d", appFrameworkContext.AppsRepoStatusPollInterval, splcommon.MaxAppsRepoPollInterval)
	}

	// Invalid volume name in defaults should return an error
	AppFramework.Defaults.VolName = "unknownVolume"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.HasPrefix(err.Error(), "invalid Volume Name for Defaults") {
		t.Errorf("Configuring Defaults with invalid volume name should return an error, but failed to detect")
	}

	AppFramework.Defaults.VolName = "msos_s2s3_vol"
	// Invalid remote volume type should return error.
	AppFramework.VolList[0].Type = "s4"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.Contains(err.Error(), "storageType 's4' is invalid. Valid values are 's3', 'gcs' and 'blob'") {
		t.Errorf("ValidateAppFrameworkSpec with invalid remote volume type should have returned error.")
	}

	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "invalid-provider"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.Contains(err.Error(), "provider 'invalid-provider' is invalid. Valid values are 'aws', 'minio', 'gcp' and 'azure'") {
		t.Errorf("ValidateAppFrameworkSpec with invalid provider should have returned error.")
	}

	// Validate s3 and azure are not right combination
	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "azure"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.Contains(err.Error(), "storageType 's3' cannot be used with provider 'azure'. Valid combinations are (s3,aws), (s3,minio), (gcs,gcp) and (blob,azure)") {
		t.Errorf("ValidateAppFrameworkSpec with s3 and azure combination should have returned error.")
	}

	// Validate blob and azure are right combination
	AppFramework.VolList[0].Type = "blob"
	AppFramework.VolList[0].Provider = "azure"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("ValidateAppFrameworkSpec with blob and azure combination should not have returned error.")
	}

	// Validate s3 and aws are right combination
	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "aws"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("ValidateAppFrameworkSpec with s3 and aws combination should not have returned error.")
	}

	// Validate s3 and aws are right combination
	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "minio"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("ValidateAppFrameworkSpec with s3 and minio combination should not have returned error.")
	}

	// Validate gcs and gcp are right combination
	AppFramework.VolList[0].Type = "gcs"
	AppFramework.VolList[0].Provider = "gcp"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err != nil {
		t.Errorf("ValidateAppFrameworkSpec with gcs and gcp combination should not have returned error.")
	}
	// Validate blob and aws are not right combination
	AppFramework.VolList[0].Type = "blob"
	AppFramework.VolList[0].Provider = "aws"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.Contains(err.Error(), "storageType 'blob' cannot be used with provider 'aws'. Valid combinations are (s3,aws), (s3,minio), (gcs,gcp) and (blob,azure)") {
		t.Errorf("ValidateAppFrameworkSpec with blob and aws combination should have returned error.")
	}

	// Validate blob and minio are not right combination
	AppFramework.VolList[0].Type = "blob"
	AppFramework.VolList[0].Provider = "minio"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, false, "")
	if err == nil || !strings.Contains(err.Error(), "storageType 'blob' cannot be used with provider 'minio'. Valid combinations are (s3,aws), (s3,minio), (gcs,gcp) and (blob,azure)") {
		t.Errorf("ValidateAppFrameworkSpec with blob and minio combination should have returned error.")
	}

	//
	// Start of tests for premiumApps input validations
	//
	// Scope premiumApps should not retrun an error

	AppFramework.VolList[0].Type = "s3"
	AppFramework.VolList[0].Provider = "aws"

	AppFramework.Defaults.Scope = ""
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopePremiumApps
	AppFramework.AppSources[0].PremiumAppsProps.Type = enterpriseApi.PremiumAppsTypeEs
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, true, "")
	if err != nil {
		t.Errorf("Valid scope premiumApps should not cause an error")
	}

	// Scope premiumApps should not retrun an error for a valid ssl enablement value
	AppFramework.Defaults.Scope = ""
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopePremiumApps
	AppFramework.AppSources[0].PremiumAppsProps.Type = enterpriseApi.PremiumAppsTypeEs
	AppFramework.AppSources[0].PremiumAppsProps.EsDefaults.SslEnablement = enterpriseApi.SslEnablementAuto
	AppFramework.AppSources[1].Scope = enterpriseApi.ScopePremiumApps
	AppFramework.AppSources[1].PremiumAppsProps.Type = enterpriseApi.PremiumAppsTypeEs
	AppFramework.AppSources[1].PremiumAppsProps.EsDefaults.SslEnablement = enterpriseApi.SslEnablementStrict
	AppFramework.AppSources[2].Scope = enterpriseApi.ScopePremiumApps
	AppFramework.AppSources[2].PremiumAppsProps.Type = enterpriseApi.PremiumAppsTypeEs
	AppFramework.AppSources[2].PremiumAppsProps.EsDefaults.SslEnablement = enterpriseApi.SslEnablementIgnore
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, true, "")
	if err != nil {
		t.Errorf("Valid SslEnablement flags should not cause an error")
	}

	// unknown premiumApp type
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopePremiumApps
	AppFramework.AppSources[0].PremiumAppsProps.Type = "unknowndPremiumType"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, true, "")
	if err == nil || !strings.HasPrefix(err.Error(), "invalid PremiumAppsProps. Valid value is enterpriseSecurity") {
		t.Errorf("invalid premium app type should be detected, but failed")
	}

	//invalid ssl flag should throws error
	AppFramework.AppSources[0].Scope = enterpriseApi.ScopePremiumApps
	AppFramework.AppSources[0].PremiumAppsProps.Type = enterpriseApi.PremiumAppsTypeEs
	AppFramework.AppSources[0].PremiumAppsProps.EsDefaults.SslEnablement = "invalidflag"
	err = ValidateAppFrameworkSpec(ctx, &AppFramework, &appFrameworkContext, true, "")
	if err == nil || !strings.HasPrefix(err.Error(), "invalid sslEnablement. Valid values") {
		t.Errorf("invalid sslEnablement flag should be detected, but failed")
	}

	//
	// End of tests for premiumApps input validations
	//

}
