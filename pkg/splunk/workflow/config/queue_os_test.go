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

package config_test

import (
	"context"
	"testing"

	enterpriseApi "github.com/splunk/splunk-operator/api/enterprise/v4"
	"github.com/splunk/splunk-operator/pkg/splunk/workflow/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func newQueueOSScheme() *runtime.Scheme {
	sch := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(sch))
	utilruntime.Must(corev1.AddToScheme(sch))
	utilruntime.Must(enterpriseApi.AddToScheme(sch))
	return sch
}

func TestResolveQueueAndObjectStorage(t *testing.T) {
	ctx := context.TODO()
	sch := newQueueOSScheme()

	t.Run("empty refs returns empty config", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(sch).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		cfg, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, corev1.ObjectReference{}, corev1.ObjectReference{})
		if err != nil {
			t.Errorf("unexpected error = %v", err)
		}
		if cfg == nil {
			t.Fatal("returned nil config")
		}
		if cfg.Queue.Provider != "" {
			t.Errorf("Expected empty Queue.Provider, got %q", cfg.Queue.Provider)
		}
		if cfg.OS.Provider != "" {
			t.Errorf("Expected empty OS.Provider, got %q", cfg.OS.Provider)
		}
	})

	t.Run("queue ref not found returns error", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(sch).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		queueRef := corev1.ObjectReference{Name: "nonexistent-queue"}
		_, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, queueRef, corev1.ObjectReference{})
		if err == nil {
			t.Error("expected error for nonexistent queue, got nil")
		}
	})

	t.Run("objectstorage ref not found returns error", func(t *testing.T) {
		c := fake.NewClientBuilder().WithScheme(sch).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		osRef := corev1.ObjectReference{Name: "nonexistent-os"}
		_, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, corev1.ObjectReference{}, osRef)
		if err == nil {
			t.Error("expected error for nonexistent objectstorage, got nil")
		}
	})

	t.Run("valid queue ref returns queue spec", func(t *testing.T) {
		queue := &enterpriseApi.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "test-queue", Namespace: "test"},
			Spec: enterpriseApi.QueueSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "my-queue",
					DLQ:      "my-dlq",
					Endpoint: "https://sqs.us-east-1.amazonaws.com",
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(sch).WithObjects(queue).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		queueRef := corev1.ObjectReference{Name: "test-queue"}
		cfg, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, queueRef, corev1.ObjectReference{})
		if err != nil {
			t.Errorf("unexpected error = %v", err)
		}
		if cfg.Queue.Provider != "sqs" {
			t.Errorf("Expected Queue.Provider = 'sqs', got %q", cfg.Queue.Provider)
		}
		if cfg.Queue.SQS.Name != "my-queue" {
			t.Errorf("Expected Queue.SQS.Name = 'my-queue', got %q", cfg.Queue.SQS.Name)
		}
		if cfg.Queue.SQS.Endpoint != "https://sqs.us-east-1.amazonaws.com" {
			t.Errorf("Expected Queue.SQS.Endpoint = 'https://sqs.us-east-1.amazonaws.com', got %q", cfg.Queue.SQS.Endpoint)
		}
	})

	t.Run("valid objectstorage ref returns os spec", func(t *testing.T) {
		os := &enterpriseApi.ObjectStorage{
			ObjectMeta: metav1.ObjectMeta{Name: "test-os", Namespace: "test"},
			Spec: enterpriseApi.ObjectStorageSpec{
				Provider: "s3",
				S3: enterpriseApi.S3Spec{
					Endpoint: "https://s3.us-east-1.amazonaws.com",
					Path:     "my-bucket/prefix",
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(sch).WithObjects(os).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		osRef := corev1.ObjectReference{Name: "test-os"}
		cfg, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, corev1.ObjectReference{}, osRef)
		if err != nil {
			t.Errorf("unexpected error = %v", err)
		}
		if cfg.OS.Provider != "s3" {
			t.Errorf("Expected OS.Provider = 's3', got %q", cfg.OS.Provider)
		}
		if cfg.OS.S3.Path != "my-bucket/prefix" {
			t.Errorf("Expected OS.S3.Path = 'my-bucket/prefix', got %q", cfg.OS.S3.Path)
		}
	})

	t.Run("queue ref with different namespace", func(t *testing.T) {
		queue := &enterpriseApi.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "test-queue", Namespace: "other-ns"},
			Spec: enterpriseApi.QueueSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "cross-ns-queue",
					DLQ:      "my-dlq",
					Endpoint: "https://sqs.eu-west-1.amazonaws.com",
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(sch).WithObjects(queue).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		queueRef := corev1.ObjectReference{Name: "test-queue", Namespace: "other-ns"}
		cfg, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, queueRef, corev1.ObjectReference{})
		if err != nil {
			t.Errorf("unexpected error = %v", err)
		}
		if cfg.Queue.SQS.Name != "cross-ns-queue" {
			t.Errorf("Expected Queue.SQS.Name = 'cross-ns-queue', got %q", cfg.Queue.SQS.Name)
		}
	})

	t.Run("queue with SecretKeyRef extracts credentials", func(t *testing.T) {
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "aws-creds", Namespace: "test"},
			Data: map[string][]byte{
				"access_key_id":     []byte("abc"),
				"secret_access_key": []byte("123"),
			},
		}
		queue := &enterpriseApi.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "test-queue", Namespace: "test"},
			Spec: enterpriseApi.QueueSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "my-queue",
					DLQ:      "my-dlq",
					Endpoint: "https://sqs.us-east-1.amazonaws.com",
					SecretKeyRef: &enterpriseApi.SQSSecretKeyRef{
						AwsAccessKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "aws-creds"}, Key: "access_key_id"},
						AwsSecretKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "aws-creds"}, Key: "secret_access_key"},
					},
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(sch).WithObjects(queue, secret).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		queueRef := corev1.ObjectReference{Name: "test-queue"}
		cfg, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, queueRef, corev1.ObjectReference{})
		if err != nil {
			t.Errorf("unexpected error = %v", err)
		}
		if cfg.AccessKey != "abc" {
			t.Errorf("Expected AccessKey = 'abc', got %q", cfg.AccessKey)
		}
		if cfg.SecretKey != "123" {
			t.Errorf("Expected SecretKey = '123', got %q", cfg.SecretKey)
		}
	})

	t.Run("queue with nil SecretKeyRef skips secret extraction (IRSA)", func(t *testing.T) {
		queue := &enterpriseApi.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "test-queue", Namespace: "test"},
			Spec: enterpriseApi.QueueSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "my-queue",
					DLQ:      "my-dlq",
					Endpoint: "https://sqs.us-east-1.amazonaws.com",
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(sch).WithObjects(queue).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		queueRef := corev1.ObjectReference{Name: "test-queue"}
		cfg, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, queueRef, corev1.ObjectReference{})
		if err != nil {
			t.Errorf("unexpected error = %v", err)
		}
		if cfg.AccessKey != "" {
			t.Errorf("Expected empty AccessKey for IRSA (no SecretKeyRef), got %q", cfg.AccessKey)
		}
		if cfg.SecretKey != "" {
			t.Errorf("Expected empty SecretKey for IRSA (no SecretKeyRef), got %q", cfg.SecretKey)
		}
	})

	t.Run("queue with missing secret returns error", func(t *testing.T) {
		queue := &enterpriseApi.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "test-queue", Namespace: "test"},
			Spec: enterpriseApi.QueueSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "my-queue",
					DLQ:      "my-dlq",
					Endpoint: "https://sqs.us-east-1.amazonaws.com",
					SecretKeyRef: &enterpriseApi.SQSSecretKeyRef{
						AwsAccessKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-secret"}, Key: "access_key_id"},
						AwsSecretKey: corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: "nonexistent-secret"}, Key: "secret_access_key"},
					},
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(sch).WithObjects(queue).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		queueRef := corev1.ObjectReference{Name: "test-queue"}
		_, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, queueRef, corev1.ObjectReference{})
		if err == nil {
			t.Error("expected error for missing secret, got nil")
		}
	})

	t.Run("both queue and objectstorage refs", func(t *testing.T) {
		queue := &enterpriseApi.Queue{
			ObjectMeta: metav1.ObjectMeta{Name: "test-queue", Namespace: "test"},
			Spec: enterpriseApi.QueueSpec{
				Provider: "sqs",
				SQS: enterpriseApi.SQSSpec{
					Name:     "my-queue",
					DLQ:      "my-dlq",
					Endpoint: "https://sqs.us-east-1.amazonaws.com",
				},
			},
		}
		os := &enterpriseApi.ObjectStorage{
			ObjectMeta: metav1.ObjectMeta{Name: "test-os", Namespace: "test"},
			Spec: enterpriseApi.ObjectStorageSpec{
				Provider: "s3",
				S3: enterpriseApi.S3Spec{
					Endpoint: "https://s3.us-east-1.amazonaws.com",
					Path:     "my-bucket",
				},
			},
		}
		c := fake.NewClientBuilder().WithScheme(sch).WithObjects(queue, os).Build()
		cr := &enterpriseApi.IndexerCluster{
			ObjectMeta: metav1.ObjectMeta{Name: "test-idxc", Namespace: "test"},
		}

		queueRef := corev1.ObjectReference{Name: "test-queue"}
		osRef := corev1.ObjectReference{Name: "test-os"}
		cfg, err := config.ResolveQueueAndObjectStorage(ctx, c, cr, queueRef, osRef)
		if err != nil {
			t.Errorf("unexpected error = %v", err)
		}
		if cfg.Queue.Provider != "sqs" {
			t.Errorf("Expected Queue.Provider = 'sqs', got %q", cfg.Queue.Provider)
		}
		if cfg.OS.Provider != "s3" {
			t.Errorf("Expected OS.Provider = 's3', got %q", cfg.OS.Provider)
		}
	})
}
