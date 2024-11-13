// Copyright (c) 2018-2022 Splunk Inc.
// All rights reserved.
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
	"context"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"cloud.google.com/go/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"google.golang.org/api/iterator"
)

// MockGCSClientInterface is a mock implementation of GCSClientInterface
type MockGCSClientInterface struct {
	mock.Mock
}

// Bucket mocks the Bucket method of GCSClientInterface
func (m *MockGCSClientInterface) Bucket(bucketName string) BucketHandleInterface {
	args := m.Called(bucketName)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(BucketHandleInterface)
}

// MockBucketHandle is a mock implementation of BucketHandleInterface
type MockBucketHandle struct {
	mock.Mock
}

// Objects mocks the Objects method of BucketHandleInterface
func (m *MockBucketHandle) Objects(ctx context.Context, query *storage.Query) ObjectIteratorInterface {
	args := m.Called(ctx, query)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ObjectIteratorInterface)
}

// Object mocks the Object method of BucketHandleInterface
func (m *MockBucketHandle) Object(name string) ObjectHandleInterface {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(ObjectHandleInterface)
}

// MockObjectIterator is a mock implementation of ObjectIteratorInterface
type MockObjectIterator struct {
	mock.Mock
	Objects []*storage.ObjectAttrs
}

// Next mocks the Next method of ObjectIteratorInterface
func (m *MockObjectIterator) Next() (*storage.ObjectAttrs, error) {
	if len(m.Objects) == 0 {
		return nil, iterator.Done
	}
	obj := m.Objects[0]
	m.Objects = m.Objects[1:]
	return obj, nil
}

// MockObjectHandle is a mock implementation of ObjectHandleInterface
type MockObjectHandle struct {
	mock.Mock
}

// NewReader mocks the NewReader method of ObjectHandleInterface
func (m *MockObjectHandle) NewReader(ctx context.Context) (io.ReadCloser, error) {
	args := m.Called(ctx)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(io.ReadCloser), args.Error(1)
}

// MockReader is a mock implementation of io.ReadCloser
type MockReader struct {
	mock.Mock
}

// Read mocks the Read method of io.Reader
func (m *MockReader) Read(p []byte) (n int, err error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

// Close mocks the Close method of io.Closer
func (m *MockReader) Close() error {
	args := m.Called()
	return args.Error(0)
}

// TestGetAppsList tests the GetAppsList method of GCSClient
func TestGetAppsList(t *testing.T) {
	// Create a mock GCS client
	mockClient := new(MockGCSClientInterface)
	mockBucket := new(MockBucketHandle)
	mockIterator := new(MockObjectIterator)

	// Setup mock objects
	mockObjects := []*storage.ObjectAttrs{
		{
			Name:         "test-prefix/app1",
			Etag:         "etag1",
			Updated:      time.Now(),
			Size:         1024,
			StorageClass: "STANDARD",
		},
		{
			Name:         "test-prefix/app2",
			Etag:         "etag2",
			Updated:      time.Now(),
			Size:         2048,
			StorageClass: "STANDARD",
		},
	}
	mockIterator.Objects = mockObjects

	// No need to set expectation on Bucket since it's not called
	// mockClient.On("Bucket", "test-bucket").Return(mockBucket)

	// Mock the Objects method to return the custom MockObjectIterator
	mockBucket.On("Objects", mock.Anything, mock.Anything).Return(mockIterator)

	// Create the GCSClient with the mock client
	gcsClient := &GCSClient{
		BucketName:   "test-bucket",
		Prefix:       "test-prefix/",
		StartAfter:   "test-prefix/app1",
		Client:       mockClient,
		BucketHandle: mockBucket, // Set the mocked bucket handle
	}

	// Call the GetAppsList method
	resp, err := gcsClient.GetAppsList(context.Background())

	// Assertions
	assert.NoError(t, err)
	assert.Equal(t, 1, len(resp.Objects)) // Only app2 should be returned due to StartAfter logic
	assert.Equal(t, "test-prefix/app2", *resp.Objects[0].Key)
	assert.Equal(t, int64(2048), *resp.Objects[0].Size)
	assert.Equal(t, "etag2", *resp.Objects[0].Etag)

	// Verify expectations
	mockBucket.AssertExpectations(t)
}

// TestDownloadApp tests the DownloadApp method of GCSClient
func TestDownloadApp(t *testing.T) {
	// Create a mock GCS client
	mockClient := new(MockGCSClientInterface)
	mockBucket := new(MockBucketHandle)
	mockObject := new(MockObjectHandle)
	mockReader := new(MockReader)

	// No need to set expectation on Bucket since it's not called
	// mockClient.On("Bucket", "test-bucket").Return(mockBucket)

	// Mock the Object method to return the mock ObjectHandle
	mockBucket.On("Object", "remote-file").Return(mockObject)

	// Mock the NewReader method to return the mock Reader
	mockObject.On("NewReader", mock.Anything).Return(mockReader, nil)

	// Simulate reading from the mock Reader
	mockReader.On("Read", mock.AnythingOfType("[]uint8")).Return(0, io.EOF)
	mockReader.On("Close").Return(nil)

	// Create a temporary file to simulate local file
	tmpFile, err := os.CreateTemp("", "testfile")
	assert.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	// Create the GCSClient with the mock client
	gcsClient := &GCSClient{
		BucketName:   "test-bucket",
		Client:       mockClient,
		BucketHandle: mockBucket, // Set the mocked bucket handle
	}

	// Prepare download request
	downloadRequest := RemoteDataDownloadRequest{
		RemoteFile: "remote-file",
		LocalFile:  tmpFile.Name(),
		Etag:       "etag",
	}

	// Call the DownloadApp method
	success, err := gcsClient.DownloadApp(context.Background(), downloadRequest)

	// Assertions
	assert.NoError(t, err)
	assert.True(t, success)

	// Verify expectations
	mockBucket.AssertExpectations(t)
	mockObject.AssertExpectations(t)
	mockReader.AssertExpectations(t)
}

// TestDownloadAppError tests the DownloadApp method of GCSClient for error case
func TestDownloadAppError(t *testing.T) {
	// Create a mock GCS client
	mockClient := new(MockGCSClientInterface)
	mockBucket := new(MockBucketHandle)
	mockObject := new(MockObjectHandle)

	// No need to set expectation on Bucket since it's not called
	// mockClient.On("Bucket", "test-bucket").Return(mockBucket)

	// Mock the Object method to return the mock ObjectHandle
	mockBucket.On("Object", "remote-file").Return(mockObject)

	// Mock the NewReader method to return an error
	mockObject.On("NewReader", mock.Anything).Return(nil, errors.New("failed to create reader"))

	// Create the GCSClient with the mock client
	gcsClient := &GCSClient{
		BucketName:   "test-bucket",
		Client:       mockClient,
		BucketHandle: mockBucket, // Set the mocked bucket handle
	}

	// Prepare download request
	downloadRequest := RemoteDataDownloadRequest{
		RemoteFile: "remote-file",
		LocalFile:  "testfile",
		Etag:       "etag",
	}

	// Call the DownloadApp method
	success, err := gcsClient.DownloadApp(context.Background(), downloadRequest)

	// Assertions
	assert.Error(t, err)
	assert.False(t, success)

	// Verify expectations
	mockBucket.AssertExpectations(t)
	mockObject.AssertExpectations(t)
}
