// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package noah

import (
	"strings"
	"testing"
)

func validKrakenRuntime() krakenRuntimeExpectations {
	return krakenRuntimeExpectations{
		serviceAccount:      "splunk-runtime",
		authRegion:          "us-west-2",
		objectStorePath:     "s3://kraken-smartstore",
		objectStoreEndpoint: "https://s3.us-west-2.amazonaws.com",
		queueEndpoint:       "https://sqs.us-west-2.amazonaws.com",
		queueURL:            "https://sqs.us-west-2.amazonaws.com/123456789012/kraken-smartstore",
	}
}

func TestValidateKrakenRuntimeExpectations(t *testing.T) {
	valid := validKrakenRuntime()

	tests := []struct {
		name   string
		mutate func(*krakenRuntimeExpectations)
	}{
		{
			name: "missing service account",
			mutate: func(runtime *krakenRuntimeExpectations) {
				runtime.serviceAccount = ""
			},
		},
		{
			name: "missing region",
			mutate: func(runtime *krakenRuntimeExpectations) {
				runtime.authRegion = ""
			},
		},
		{
			name: "non-S3 object-store path",
			mutate: func(runtime *krakenRuntimeExpectations) {
				runtime.objectStorePath = "https://example.invalid/bucket"
			},
		},
		{
			name: "MinIO object-store endpoint",
			mutate: func(runtime *krakenRuntimeExpectations) {
				runtime.objectStoreEndpoint = "https://noah-minio.example.invalid"
			},
		},
		{
			name: "non-TLS queue URL",
			mutate: func(runtime *krakenRuntimeExpectations) {
				runtime.queueURL = "http://sqs.example.invalid/queue"
			},
		},
		{
			name: "queue URL with credentials",
			mutate: func(runtime *krakenRuntimeExpectations) {
				runtime.queueURL = "https://user:password@sqs.example.invalid/queue"
			},
		},
	}

	if err := validateKrakenRuntimeExpectations(valid); err != nil {
		t.Fatalf("valid Kraken runtime was rejected: %v", err)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runtime := valid
			test.mutate(&runtime)
			if err := validateKrakenRuntimeExpectations(runtime); err == nil {
				t.Fatal("invalid Kraken runtime was accepted")
			}
		})
	}
}

func TestValidateIndexerDefaults(t *testing.T) {
	runtime := validKrakenRuntime()
	defaults := func(path, remotePath string) string {
		return `splunk:
  conf:
    indexes:
      content:
        volume:remote_store:
          storageType: remote
          path: ` + path + `
          remote.s3.endpoint: ` + runtime.objectStoreEndpoint + `
          remote.s3.signature_version: v4
          remote.s3.auth_region: ` + runtime.authRegion + `
          remote.s3.sqs.endpoint: ` + runtime.queueEndpoint + `
          remote.s3.sqs.queue_url: ` + runtime.queueURL + `
          remote.s3.supports_versioning: "false"
        main:
          remotePath: ` + remotePath + "\n"
	}

	tests := []struct {
		name      string
		raw       string
		wantError string
	}{
		{name: "matching defaults", raw: defaults(runtime.objectStorePath, "volume:remote_store/main")},
		{name: "wrong object-store path", raw: defaults("s3://other-bucket", "volume:remote_store/main"), wantError: "path"},
		{name: "main not on the remote volume", raw: defaults(runtime.objectStorePath, "volume:other/main"), wantError: "remotePath"},
		{name: "no remote_store stanza", raw: "splunk:\n  conf:\n    indexes:\n      content: {}\n", wantError: "volume:remote_store"},
		{name: "unparseable defaults", raw: "splunk: [", wantError: "parse"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateIndexerDefaults(test.raw, runtime)
			if test.wantError == "" {
				if err != nil {
					t.Errorf("validateIndexerDefaults returned an error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Errorf("validateIndexerDefaults error = %v, want one mentioning %q", err, test.wantError)
			}
		})
	}
}

func TestLoadKrakenRuntimeExpectationsReportsAllMissingVariables(t *testing.T) {
	names := []string{
		"NOAH_TEST_SPLUNK_SERVICE_ACCOUNT",
		"NOAH_TEST_SMARTSTORE_AUTH_REGION",
		"NOAH_TEST_SMARTSTORE_OBJECT_STORE_PATH",
		"NOAH_TEST_SMARTSTORE_OBJECT_STORE_ENDPOINT",
		"NOAH_TEST_SMARTSTORE_QUEUE_ENDPOINT",
		"NOAH_TEST_SMARTSTORE_QUEUE_URL",
	}
	for _, name := range names {
		t.Setenv(name, "set")
	}
	t.Setenv("NOAH_TEST_SMARTSTORE_AUTH_REGION", "")
	t.Setenv("NOAH_TEST_SMARTSTORE_QUEUE_URL", "")

	_, err := loadKrakenRuntimeExpectations()
	if err == nil {
		t.Fatal("loadKrakenRuntimeExpectations accepted empty variables")
	}
	for _, name := range []string{"NOAH_TEST_SMARTSTORE_AUTH_REGION", "NOAH_TEST_SMARTSTORE_QUEUE_URL"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error %q does not mention %s", err, name)
		}
	}
}
