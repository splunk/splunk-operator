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

// Suppression strings for certain splunk CLI commands
// Splunk ES app has a lot of info strings marked as stderr, ignore them
var splunkCliSuppressionStrings = []string{
	"WARNING: Server Certificate Hostname Validation is disabled. Please see server.conf/[sslConfig]/cliVerifyServerName for details.\n",
	"INFO: Initialization complete\nINFO: SSL enablement set to ignore, continuing...\n",
	"INFO: Installation complete\nINFO: SSL enablement set to ignore, continuing...\n",
	"INFO: Enabled SSL in system namespace\nINFO: Initialization complete\n",
	"INFO: Enabled SSL in system namespace\nINFO: Installation complete\n",
	"INFO: Initialization complete, please restart Splunk\nINFO: SSL enablement set to ignore, continuing...\n",
	"INFO: Installation complete, please restart Splunk\nINFO: SSL enablement set to ignore, continuing...\n",
	"INFO: Initialization complete\n",
	"INFO: Installation complete\n",
	"INFO: Init complete\n",
	"App \"SplunkEnterpriseSecuritySuite\" already exists; use the \"update\" argument to install anyway\n",
}

const (
	// Less than zero version error
	lessThanOrEqualToZeroVersionError = "versions shouldn't be <= 0"

	// Non-integer version error
	nonIntegerVersionError = "failed to convert non integer string to integer value"

	// Non-matching string error
	nonMatchingStringError = "non-matching string secretName %s versionedSecretIdentifier %s"

	// nilSecretDataError indicates nil secret data
	invalidSecretDataError = "invalid secret data"

	// emptySecretTokenError indicates empty secret token
	emptySecretTokenError = "empty secret token"

	// nonExistingSecret indicates a non-existing secret
	emptyPodSpecVolumes = "empty pod spec volumes"

	// emptySecretVolumeSource indicates an empty
	emptySecretVolumeSource = "didn't find secret volume source in any pod volume"

	// splunkSSHWarningMessage Note: splunk 9.0 throws warning message "warning: server certificate hostname validation is disabled. please see server.conf/[sslconfig]/cliverifyservername for details.\n"
	// we are supressing the message
	splunkSSHWarningMessage = "WARNING: Server Certificate Hostname Validation is disabled. Please see server.conf/[sslConfig]/cliVerifyServerName for details.\n"
)
