// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package builders

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
)

func TestServiceBuilder_Build(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*ServiceBuilder)
		wantErr   bool
		errMsg    string
		validate  func(*testing.T, *ServiceBuilder)
	}{
		{
			name: "valid basic service",
			setupFunc: func(b *ServiceBuilder) {
				b.WithName("test-svc").
					WithPorts([]corev1.ServicePort{
						{Name: "web", Port: 8000},
					})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ServiceBuilder) {
				svc, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if svc.Name != "test-svc" {
					t.Errorf("Name = %v, want test-svc", svc.Name)
				}
				if len(svc.Spec.Ports) != 1 {
					t.Errorf("Ports count = %v, want 1", len(svc.Spec.Ports))
				}
			},
		},
		{
			name: "missing name",
			setupFunc: func(b *ServiceBuilder) {
				b.WithPorts([]corev1.ServicePort{{Name: "web", Port: 8000}})
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing ports",
			setupFunc: func(b *ServiceBuilder) {
				b.WithName("test-svc")
			},
			wantErr: true,
			errMsg:  "at least one port is required",
		},
		{
			name: "with service type",
			setupFunc: func(b *ServiceBuilder) {
				b.WithName("test-svc").
					WithType(corev1.ServiceTypeNodePort).
					WithPorts([]corev1.ServicePort{{Name: "web", Port: 8000}})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ServiceBuilder) {
				svc, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if svc.Spec.Type != corev1.ServiceTypeNodePort {
					t.Errorf("Type = %v, want NodePort", svc.Spec.Type)
				}
			},
		},
		{
			name: "with custom selector",
			setupFunc: func(b *ServiceBuilder) {
				b.WithName("test-svc").
					WithPorts([]corev1.ServicePort{{Name: "web", Port: 8000}}).
					WithSelector(map[string]string{"app": "custom"})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ServiceBuilder) {
				svc, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if svc.Spec.Selector["app"] != "custom" {
					t.Error("Custom selector not applied")
				}
			},
		},
		{
			name: "with discovery labels",
			setupFunc: func(b *ServiceBuilder) {
				b.WithName("test-svc").
					WithPorts([]corev1.ServicePort{{Name: "web", Port: 8000}}).
					WithDiscoveryLabels()
			},
			wantErr: false,
			validate: func(t *testing.T, b *ServiceBuilder) {
				svc, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if svc.Labels["splunk.com/discoverable"] != "true" {
					t.Error("Discovery label not set")
				}
			},
		},
		{
			name: "default selector generated",
			setupFunc: func(b *ServiceBuilder) {
				b.WithName("test-svc").
					WithPorts([]corev1.ServicePort{{Name: "web", Port: 8000}})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ServiceBuilder) {
				svc, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if svc.Spec.Selector["app.kubernetes.io/name"] != "test-svc" {
					t.Error("Default selector not generated")
				}
			},
		},
		{
			name: "multiple ports",
			setupFunc: func(b *ServiceBuilder) {
				b.WithName("test-svc").
					WithPorts([]corev1.ServicePort{
						{Name: "web", Port: 8000},
						{Name: "mgmt", Port: 8089},
						{Name: "hec", Port: 8088},
					})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ServiceBuilder) {
				svc, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if len(svc.Spec.Ports) != 3 {
					t.Errorf("Ports count = %v, want 3", len(svc.Spec.Ports))
				}
			},
		},
		{
			name: "standard labels applied",
			setupFunc: func(b *ServiceBuilder) {
				b.WithName("test-svc").
					WithPorts([]corev1.ServicePort{{Name: "web", Port: 8000}})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ServiceBuilder) {
				svc, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if svc.Labels["app.kubernetes.io/managed-by"] != "splunk-operator" {
					t.Error("Standard managed-by label not set")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewServiceBuilder("default", "test-owner")
			tt.setupFunc(builder)

			if tt.wantErr {
				_, err := builder.Build()
				if err == nil {
					t.Error("Build() expected error but got none")
				} else if tt.errMsg != "" && err.Error() != tt.errMsg {
					t.Errorf("Build() error = %v, want %v", err.Error(), tt.errMsg)
				}
			} else if tt.validate != nil {
				tt.validate(t, builder)
			}
		})
	}
}

func TestServiceBuilder_Defaults(t *testing.T) {
	builder := NewServiceBuilder("test-ns", "test-owner")

	if builder.serviceType != corev1.ServiceTypeClusterIP {
		t.Errorf("default service type = %v, want ClusterIP", builder.serviceType)
	}
}
