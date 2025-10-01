// Copyright (c) 2018-2024 Splunk Inc. All rights reserved.
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
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	appframeworkv1 "github.com/splunk/splunk-operator/api/appframework/v1"
)

// RemoteClientManager manages remote storage clients
type RemoteClientManager struct {
	Client  client.Client
	clients map[string]*MockRemoteClient
	mutex   sync.RWMutex
}

// MockRemoteClient provides a mock implementation for testing
type MockRemoteClient struct {
	repository *appframeworkv1.AppFrameworkRepository
	connected  bool
}

// RemoteStorageClient interface for remote storage operations
type RemoteStorageClient interface {
	TestConnection(ctx context.Context) error
	ListObjects(ctx context.Context, path string) ([]*RemoteObject, error)
	DownloadObject(ctx context.Context, key, localPath string) error
	GetObjectMetadata(ctx context.Context, key string) (*RemoteObjectMetadata, error)
}

// RemoteObject represents an object in remote storage
type RemoteObject struct {
	Key          string
	ETag         string
	Size         int64
	LastModified string
}

// RemoteObjectMetadata contains metadata about a remote object
type RemoteObjectMetadata struct {
	Key          string
	ETag         string
	Size         int64
	LastModified string
	ContentType  string
}

// NewRemoteClientManager creates a new RemoteClientManager
func NewRemoteClientManager(client client.Client) *RemoteClientManager {
	return &RemoteClientManager{
		Client:  client,
		clients: make(map[string]*MockRemoteClient),
	}
}

// GetClient gets or creates a remote storage client for a repository
func (rcm *RemoteClientManager) GetClient(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) (RemoteStorageClient, error) {
	logger := log.FromContext(ctx)

	clientKey := fmt.Sprintf("%s/%s", repository.Namespace, repository.Name)

	rcm.mutex.RLock()
	if client, exists := rcm.clients[clientKey]; exists {
		rcm.mutex.RUnlock()
		return client, nil
	}
	rcm.mutex.RUnlock()

	// Create new client
	rcm.mutex.Lock()
	defer rcm.mutex.Unlock()

	// Double-check after acquiring write lock
	if client, exists := rcm.clients[clientKey]; exists {
		return client, nil
	}

	logger.Info("Creating new remote storage client", "repository", repository.Name, "provider", repository.Spec.Provider)

	// Create mock client for now
	mockClient := &MockRemoteClient{
		repository: repository,
		connected:  true,
	}

	rcm.clients[clientKey] = mockClient
	return mockClient, nil
}

// Mock implementation methods

// ValidateConnection validates connection to the repository
func (rcm *RemoteClientManager) ValidateConnection(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository) error {
	client, err := rcm.GetClient(ctx, repository)
	if err != nil {
		return err
	}
	return client.TestConnection(ctx)
}

// GetAppsList gets list of apps from repository
func (rcm *RemoteClientManager) GetAppsList(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository, path string) ([]*RemoteObject, error) {
	client, err := rcm.GetClient(ctx, repository)
	if err != nil {
		return nil, err
	}
	return client.ListObjects(ctx, path)
}

// DownloadApp downloads an app from repository
func (rcm *RemoteClientManager) DownloadApp(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository, key, localPath string) error {
	client, err := rcm.GetClient(ctx, repository)
	if err != nil {
		return err
	}
	return client.DownloadObject(ctx, key, localPath)
}

// TestConnection tests the connection to remote storage
func (m *MockRemoteClient) TestConnection(ctx context.Context) error {
	if !m.connected {
		return fmt.Errorf("connection failed for provider %s", m.repository.Spec.Provider)
	}
	return nil
}

// ListObjects lists objects in the remote storage
func (m *MockRemoteClient) ListObjects(ctx context.Context, path string) ([]*RemoteObject, error) {
	// Mock implementation - return sample apps
	return []*RemoteObject{
		{
			Key:          "sample-app.tgz",
			ETag:         "abc123",
			Size:         1024000,
			LastModified: "2024-01-01T12:00:00Z",
		},
	}, nil
}

// DownloadObject downloads an object from remote storage
func (m *MockRemoteClient) DownloadObject(ctx context.Context, key, localPath string) error {
	// Mock implementation - simulate download
	return nil
}

// GetObjectMetadata gets metadata for an object
func (m *MockRemoteClient) GetObjectMetadata(ctx context.Context, key string) (*RemoteObjectMetadata, error) {
	return &RemoteObjectMetadata{
		Key:          key,
		ETag:         "abc123",
		Size:         1024000,
		LastModified: "2024-01-01T12:00:00Z",
		ContentType:  "application/gzip",
	}, nil
}

// RemoveClient removes a client from the cache
func (rcm *RemoteClientManager) RemoveClient(repository *appframeworkv1.AppFrameworkRepository) {
	clientKey := fmt.Sprintf("%s/%s", repository.Namespace, repository.Name)

	defer rcm.mutex.Unlock()

	delete(rcm.clients, clientKey)
}

// GetAppMetadata gets metadata for an app
func (rcm *RemoteClientManager) GetAppMetadata(ctx context.Context, repository *appframeworkv1.AppFrameworkRepository, appKey string) (*RemoteObjectMetadata, error) {
	client, err := rcm.GetClient(ctx, repository)
	if err != nil {
		return nil, fmt.Errorf("failed to get client: %w", err)
	}

	// Double-check after acquiring write lock
	if client, exists := rcm.clients[clientKey]; exists {
		return client.GetObjectMetadata(ctx, appKey)
	}

	return nil, fmt.Errorf("client not found for repository %s/%s", repository.Namespace, repository.Name)

	return client.GetObjectMetadata(ctx, appKey)
}
