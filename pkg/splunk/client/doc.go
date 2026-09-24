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

// Package client wires together the remote storage and Splunk REST sub-packages
// and exposes backward-compatible type aliases for existing callers.
//
// Sub-packages:
//
//	splunk/          Splunk Enterprise REST API client
//	storage/         Remote storage client registry and test utilities
//	storage/aws/     AWS S3 remote storage client
//	storage/azure/   Azure Blob Storage remote storage client
//	storage/gcp/     Google Cloud Storage remote storage client
//	storage/minio/   Minio/S3-compatible remote storage client
//	certmanager/     cert-manager Certificate/Issuer client library
package client
