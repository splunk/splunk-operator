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

import "strings"

const (
	livenessProbeLevelDefault = iota
	livenessProbeLevelOne

	applySHCBundleCmdStr = "/opt/splunk/bin/splunk apply shcluster-bundle -target https://%s:8089 -auth admin:%s --answer-yes -push-default-apps true &> %s &"

	shcBundlePushCompleteStr = "Bundle has been pushed successfully to all the cluster members.\n"

	shcBundlePushStatusCheckFile = "/operator-staging/appframework/.shcluster_bundle_status.txt"

	applyIdxcBundleCmdStr = "/opt/splunk/bin/splunk apply cluster-bundle -auth admin:%s --skip-validation --answer-yes"

	// splunkFIPSProviderBannerStr is written by the Splunk CLI before some bundle-push output on FIPS-enabled clusters.
	splunkFIPSProviderBannerStr = "FIPS provider enabled."

	// splunkSSLCertWarnStr prefixes SSL certificate warnings emitted by the Splunk CLI.
	splunkSSLCertWarnStr = "WARNING: Server Certificate"

	idxcShowClusterBundleStatusStr = "/opt/splunk/bin/splunk show cluster-bundle-status -auth admin:%s"

	idxcBundleAlreadyPresentStr = "No new bundle will be pushed. The cluster manager and peers already have this bundle"

	shcAppsLocationOnDeployer = "/opt/splunk/etc/shcluster/apps/"

	idxcAppsLocationOnClusterManager = "/opt/splunk/etc/manager-apps/"

	cmdSetFilePermissionsToRW = "chmod +660 -R %s"

	appBktMnt = "/operator-staging/appframework/"
)

// shellQuote returns a shell-safe single-quoted value.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// redactSplunkAuth replaces both raw and shell-quoted forms of adminPwd with
// **** for safe logging.
func redactSplunkAuth(cmd, adminPwd string) string {
	if adminPwd == "" {
		return cmd
	}
	redacted := strings.ReplaceAll(cmd, shellQuote(adminPwd), "'****'")
	return strings.ReplaceAll(redacted, adminPwd, "****")
}
