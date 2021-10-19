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

package client

import (
	"reflect"
	"testing"
)

func TestInitMinioClientWrapper(t *testing.T) {

	minioS3ClientSession := InitMinioClientWrapper("https://s3.us-east-1.amazonaws.com", "abcd", "1234")
	if minioS3ClientSession == nil {
		t.Errorf("We should have got a valid Minio S3 client object")
	}
}

func TestNewMinioClient(t *testing.T) {

	fn := InitMinioClientWrapper

	// Test1. Test for endpoint with https
	minioS3Client, err := NewMinioClient("sample_bucket", "abcd", "xyz", "admin/", "admin", "https://s3.us-west-2.amazonaws.com", fn)
	if minioS3Client == nil || err != nil {
		t.Errorf("NewMinioClient should have returned a valid Minio S3 client.")
	}

	// Test2. Test for endpoint with http
	minioS3Client, err = NewMinioClient("sample_bucket", "abcd", "xyz", "admin/", "admin", "http://s3.us-west-2.amazonaws.com", fn)
	if minioS3Client == nil || err != nil {
		t.Errorf("NewMinioClient should have returned a valid Minio S3 client.")
	}

	// Test3. Test for invalid endpoint
	minioS3Client, err = NewMinioClient("sample_bucket", "abcd", "xyz", "admin/", "admin", "random-endpoint.com", fn)
	if minioS3Client != nil || err == nil {
		t.Errorf("NewMinioClient should have returned a error.")
	}
}

func TestMinioGetInitContainerImage(t *testing.T) {
	minioClient := &MinioClient{}

	if minioClient.GetInitContainerImage() != "amazon/aws-cli" {
		t.Errorf("Got invalid init container image for Minio client.")
	}
}

func TestGetMinioInitContainerCmd(t *testing.T) {
	wantCmd := []string{"--endpoint-url=https://s3.us-west-2.amazonaws.com", "s3", "sync", "s3://sample_bucket/admin/", "/mnt/apps-local/admin/"}

	minioClient := &MinioClient{}
	gotCmd := minioClient.GetInitContainerCmd("https://s3.us-west-2.amazonaws.com", "sample_bucket", "admin", "admin", "/mnt/apps-local/")
	if !reflect.DeepEqual(wantCmd, gotCmd) {
		t.Errorf("Got incorrect Init container cmd")
	}
}
