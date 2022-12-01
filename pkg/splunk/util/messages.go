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

const (
	// Less than zero version error
	lessThanOrEqualToZeroVersionError = "Versions shouldn't be <= 0"

	// Non-integer version error
	nonIntegerVersionError = "Failed to convert non integer string to integer value"

	// Non-matching string error
	nonMatchingStringError = "Non-matching string secretName %s versionedSecretIdentifier %s"

	// Missing Token error
	missingTokenError = "Couldn't convert to splunk readable format, %s token missing"

	// nilSecretDataError indicates nil secret data
	invalidSecretDataError = "Invalid secret data"

	// emptySecretTokenError indicates empty secret token
	emptySecretTokenError = "Empty secret token"

	// nonExistingSecret indicates a non-existing secret
	emptyPodSpecVolumes = "Empty pod spec volumes"

	// emptySecretVolumeSource indicates an empty
	emptySecretVolumeSource = "Didn't find secret volume source in any pod volume"

	// splunkSSHWarningMessage Note: splunk 9.0 throws warning message "warning: server certificate hostname validation is disabled. please see server.conf/[sslconfig]/cliverifyservername for details.\n"
	// we are supressing the message
	splunkSSHWarningMessage = "WARNING: Server Certificate Hostname Validation is disabled. Please see server.conf/[sslConfig]/cliVerifyServerName for details.\n"

	// splunkEsAppSSLWarning Note: The ES app post install spews out a harmless ES app message which can be ignored
	splunkEsAppSSLWarning = "INFO: Installation complete\nINFO: SSL enablement set to ignore, continuing...\n"

	// splunkEsAppSSLAutoWarning Note: The ES app post install spews out a harmless ES app message which can be ignored
	splunkEsAppSSLAutoWarning = "INFO: Enabled SSL in system namespace\nINFO: Installation complete\n"

	// splunkEsAppAlreadyExists Note: The ES app post install spews out a harmless ES app message which can be ignored
	splunkEsAppAlreadyExists = "App \"SplunkEnterpriseSecuritySuite\" already exists; use the \"update\" argument to install anyway\n"

	// splunkEsAppInstallationComplete
	splunkEsAppInstallationComplete = "INFO: Installation complete\n"
)
