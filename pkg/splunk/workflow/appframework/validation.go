// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package appframework

import (
	"context"
	"errors"
	"fmt"
	"os"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/logging"
	splcommon "github.com/splunk/splunk-operator/pkg/splunk/common"
	splutil "github.com/splunk/splunk-operator/pkg/splunk/util"
)

// getAppSrcScope returns the scope of a given appSource.
func getAppSrcScope(ctx context.Context, appFrameworkConf *enterpriseApi.AppFrameworkSpec, appSrcName string) string {
	for _, appSrc := range appFrameworkConf.AppSources {
		if appSrc.Name == appSrcName {
			if appSrc.Scope != "" {
				return appSrc.Scope
			}

			break
		}
	}

	return appFrameworkConf.Defaults.Scope
}

// getAppSrcSpec returns AppSourceSpec from the app source name.
func getAppSrcSpec(appSources []enterpriseApi.AppSourceSpec, appSrcName string) (*enterpriseApi.AppSourceSpec, error) {
	var err error

	for _, appSrc := range appSources {
		if appSrc.Name == appSrcName {
			return &appSrc, err
		}
	}

	err = fmt.Errorf("unable to find app source spec for app source: %s", appSrcName)
	return nil, err
}

// CheckIfAppSrcExistsInConfig returns if the given appSource is available in the configuration or not.
func CheckIfAppSrcExistsInConfig(appFrameworkConf *enterpriseApi.AppFrameworkSpec, appSrcName string) bool {
	for _, appSrc := range appFrameworkConf.AppSources {
		if appSrc.Name == appSrcName {
			return true
		}
	}
	return false
}

func isAppSourceScopeValid(scope string) bool {
	return scope == enterpriseApi.ScopeLocal || scope == enterpriseApi.ScopeCluster || scope == enterpriseApi.ScopePremiumApps || scope == enterpriseApi.ScopeClusterWithPreConfig
}

// validateSplunkAppSources validates the App source config in App Framework spec.
func validateSplunkAppSources(appFramework *enterpriseApi.AppFrameworkSpec, localOrPremScope bool, crKind string) error {
	duplicateAppSourceStorageChecker := make(map[string]map[string]bool)
	duplicateAppSourceStorageChecker[enterpriseApi.ScopeLocal] = make(map[string]bool)
	duplicateAppSourceStorageChecker[enterpriseApi.ScopePremiumApps] = make(map[string]bool)

	// CSPL-2574 - Assign just in case invalid scope is passed through.
	duplicateAppSourceStorageChecker[enterpriseApi.ScopeCluster] = make(map[string]bool)
	duplicateAppSourceStorageChecker[enterpriseApi.ScopeClusterWithPreConfig] = make(map[string]bool)

	duplicateAppSourceNameChecker := make(map[string]bool)

	var vol string
	for i, appSrc := range appFramework.AppSources {
		if appSrc.Name == "" {
			return fmt.Errorf("app Source name is missing for AppSource at: %d", i)
		}

		if _, ok := duplicateAppSourceNameChecker[appSrc.Name]; ok {
			return fmt.Errorf("multiple app sources with the name %s is not allowed", appSrc.Name)
		}
		duplicateAppSourceNameChecker[appSrc.Name] = true

		if appSrc.Location == "" {
			return fmt.Errorf("app Source location is missing for AppSource: %s", appSrc.Name)
		}

		if appSrc.VolName != "" {
			_, err := splutil.CheckIfVolumeExists(appFramework.VolList, appSrc.VolName)
			if err != nil {
				return fmt.Errorf("invalid Volume Name for App Source: %s. %s", appSrc.Name, err)
			}
			vol = appSrc.VolName
		} else {
			if appFramework.Defaults.VolName == "" {
				return fmt.Errorf("volumeName is missing for App Source: %s", appSrc.Name)
			}
			vol = appFramework.Defaults.VolName
		}

		var scope string
		if appSrc.Scope != "" {
			if localOrPremScope && !(appSrc.Scope == enterpriseApi.ScopeLocal || appSrc.Scope == enterpriseApi.ScopePremiumApps) {
				return fmt.Errorf("invalid scope for App Source: %s. Valid scopes are %s or %s for this kind of CR", appSrc.Name, enterpriseApi.ScopeLocal, enterpriseApi.ScopePremiumApps)
			}

			if !isAppSourceScopeValid(appSrc.Scope) {
				return fmt.Errorf("scope for App Source: %s should be either %s or %s or %s", appSrc.Name, enterpriseApi.ScopeLocal, enterpriseApi.ScopeCluster, enterpriseApi.ScopePremiumApps)
			}

			if appSrc.Scope == enterpriseApi.ScopePremiumApps || appFramework.Defaults.Scope == enterpriseApi.ScopePremiumApps {
				err := ValidatePremiumAppsInputs(appSrc, crKind)
				if err != nil {
					return err
				}
			}
			scope = appSrc.Scope
		} else {
			if appFramework.Defaults.Scope == "" {
				return fmt.Errorf("app Source scope is missing for: %s", appSrc.Name)
			}

			scope = appFramework.Defaults.Scope
		}

		if _, ok := duplicateAppSourceStorageChecker[scope][vol+appSrc.Location]; ok {
			return fmt.Errorf("duplicate App Source configured for Volume: %s, and Location: %s combo. Remove the duplicate entry and reapply the configuration", vol, appSrc.Location)
		}
		duplicateAppSourceStorageChecker[scope][vol+appSrc.Location] = true
	}

	if localOrPremScope && appFramework.Defaults.Scope != "" &&
		(appFramework.Defaults.Scope != enterpriseApi.ScopeLocal && appFramework.Defaults.Scope != enterpriseApi.ScopePremiumApps) {
		return fmt.Errorf("invalid scope for defaults config. Only local scope is supported for this kind of CR")
	}

	if appFramework.Defaults.Scope != "" && !isAppSourceScopeValid(appFramework.Defaults.Scope) {
		return fmt.Errorf("scope for defaults should be either local Or cluster, but configured as: %s", appFramework.Defaults.Scope)
	}

	if appFramework.Defaults.VolName != "" {
		_, err := splutil.CheckIfVolumeExists(appFramework.VolList, appFramework.Defaults.VolName)
		if err != nil {
			return fmt.Errorf("invalid Volume Name for Defaults. Error: %s", err)
		}
	}

	return nil
}

// ValidatePremiumAppsInputs validates premium app source spec.
func ValidatePremiumAppsInputs(appSrc enterpriseApi.AppSourceSpec, crKind string) error {
	if appSrc.AppSourceDefaultSpec.PremiumAppsProps.Type != enterpriseApi.PremiumAppsTypeEs {
		return fmt.Errorf("invalid PremiumAppsProps. Valid value is %s", enterpriseApi.PremiumAppsTypeEs)
	}

	sslEnablementValue := appSrc.AppSourceDefaultSpec.PremiumAppsProps.EsDefaults.SslEnablement
	if sslEnablementValue != "" && !(sslEnablementValue == enterpriseApi.SslEnablementAuto ||
		sslEnablementValue == enterpriseApi.SslEnablementIgnore ||
		sslEnablementValue == enterpriseApi.SslEnablementStrict) {
		return fmt.Errorf("invalid sslEnablement. Valid values are %s or %s or %s", enterpriseApi.SslEnablementAuto,
			enterpriseApi.SslEnablementIgnore, enterpriseApi.SslEnablementStrict)
	}

	if crKind == "SearchHeadCluster" && appSrc.PremiumAppsProps.Type == enterpriseApi.PremiumAppsTypeEs &&
		appSrc.AppSourceDefaultSpec.PremiumAppsProps.EsDefaults.SslEnablement == enterpriseApi.SslEnablementAuto {
		return fmt.Errorf("scope for app source: %s search head cluster cannot have an ES app installed with ssl_enablement auto", appSrc.Name)
	}
	return nil
}

func isAppFrameworkConfigured(appFramework *enterpriseApi.AppFrameworkSpec) bool {
	return !(appFramework == nil || appFramework.AppSources == nil)
}

// ValidateAppFrameworkSpec checks and validates the Apps Frame Work config.
func ValidateAppFrameworkSpec(ctx context.Context, appFramework *enterpriseApi.AppFrameworkSpec, appContext *enterpriseApi.AppDeploymentContext, localScope bool, crKind string) error {
	var err error
	if !isAppFrameworkConfigured(appFramework) {
		return nil
	}

	logger := logging.FromContext(ctx).With("func", "ValidateAppFrameworkSpec")
	logger.InfoContext(ctx, "configCheck", "scope", localScope)

	appContext.AppsRepoStatusPollInterval = appFramework.AppsRepoPollInterval
	appContext.AppsStatusMaxConcurrentAppDownloads = appFramework.MaxConcurrentAppDownloads

	if appContext.AppsRepoStatusPollInterval <= 0 {
		logger.ErrorContext(ctx, "appsRepoPollIntervalSeconds is not configured. Disabling polling of apps repo changes, defaulting to manual updates", "error", err)
		appContext.AppsRepoStatusPollInterval = 0
	} else if appFramework.AppsRepoPollInterval < splcommon.MinAppsRepoPollInterval {
		logger.ErrorContext(ctx, "configured appsRepoPollIntervalSeconds is too small", "error", err, "configuredValue", appFramework.AppsRepoPollInterval, "defaultMinSeconds", splcommon.MinAppsRepoPollInterval)
		appContext.AppsRepoStatusPollInterval = splcommon.MinAppsRepoPollInterval
	} else if appFramework.AppsRepoPollInterval > splcommon.MaxAppsRepoPollInterval {
		logger.ErrorContext(ctx, "configured appsRepoPollIntervalSeconds is too large", "error", err, "configuredValue", appFramework.AppsRepoPollInterval, "defaultMaxSeconds", splcommon.MaxAppsRepoPollInterval)
		appContext.AppsRepoStatusPollInterval = splcommon.MaxAppsRepoPollInterval
	}

	if appContext.AppsStatusMaxConcurrentAppDownloads <= 0 {
		logger.InfoContext(ctx, "invalid value of maxConcurrentAppDownloads", "configuredValue", appContext.AppsStatusMaxConcurrentAppDownloads, "defaultValue", splcommon.DefaultMaxConcurrentAppDownloads)
		appContext.AppsStatusMaxConcurrentAppDownloads = splcommon.DefaultMaxConcurrentAppDownloads
	}

	appDownloadVolume := getResolvedAppDownloadVolume()
	if _, err := os.Stat(appDownloadVolume); errors.Is(err, os.ErrNotExist) {
		logger.ErrorContext(ctx, "volume needs to be mounted on operator pod to download apps. Please mount it as a separate volume on operator pod", "error", err, "volumePath", appDownloadVolume)
		return err
	}

	err = validateRemoteVolumeSpec(ctx, appFramework.VolList, true)
	if err != nil {
		return err
	}

	err = validateSplunkAppSources(appFramework, localScope, crKind)
	if err == nil {
		logger.InfoContext(ctx, "app framework configuration is valid")
	}

	return err
}

func validateRemoteVolumeSpec(ctx context.Context, volList []enterpriseApi.VolumeSpec, isAppFramework bool) error {
	duplicateChecker := make(map[string]bool)

	logger := logging.FromContext(ctx).With("func", "validateRemoteVolumeSpec")

	for i, volume := range volList {
		if _, ok := duplicateChecker[volume.Name]; ok {
			return fmt.Errorf("duplicate volume name detected: %s. Remove the duplicate entry and reapply the configuration", volume.Name)
		}
		duplicateChecker[volume.Name] = true
		if volume.Name == "" {
			return fmt.Errorf("volume name is missing for volume at : %d", i)
		}
		if volume.Endpoint == "" {
			return fmt.Errorf("volume Endpoint URI is missing")
		}
		if volume.Path == "" {
			return fmt.Errorf("volume Path is missing")
		}
		if volume.SecretRef == "" {
			logger.InfoContext(ctx, "no valid SecretRef for volume", "volumeName", volume.Name)
		}

		if isAppFramework {
			if !isValidStorageType(volume.Type) {
				return fmt.Errorf("storageType '%s' is invalid. Valid values are 's3', 'gcs' and 'blob'", volume.Type)
			}

			if !isValidProvider(volume.Provider) {
				return fmt.Errorf("provider '%s' is invalid. Valid values are 'aws', 'minio', 'gcp' and 'azure'", volume.Provider)
			}

			if !isValidProviderForStorageType(volume.Type, volume.Provider) {
				return fmt.Errorf("storageType '%s' cannot be used with provider '%s'. Valid combinations are (s3,aws), (s3,minio), (gcs,gcp) and (blob,azure)", volume.Type, volume.Provider)
			}
		}
	}
	return nil
}

func isValidStorageType(storage string) bool {
	return storage != "" && (storage == "s3" || storage == "blob" || storage == "gcs")
}

func isValidProvider(provider string) bool {
	return provider != "" && (provider == "aws" || provider == "minio" || provider == "azure" || provider == "gcp")
}

func isValidProviderForStorageType(storageType string, provider string) bool {
	return ((storageType == "s3" && (provider == "aws" || provider == "minio")) ||
		(storageType == "blob" && provider == "azure") ||
		(storageType == "gcs" && provider == "gcp"))
}
