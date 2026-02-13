// Copyright (c) 2018-2026 Splunk Inc. All rights reserved.

package builders

import (
	"testing"
)

func TestConfigMapBuilder_Build(t *testing.T) {
	tests := []struct {
		name      string
		setupFunc func(*ConfigMapBuilder)
		wantErr   bool
		errMsg    string
		validate  func(*testing.T, *ConfigMapBuilder)
	}{
		{
			name: "valid basic configmap",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithName("test-cm").
					WithData(map[string]string{"key": "value"})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ConfigMapBuilder) {
				cm, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if cm.Name != "test-cm" {
					t.Errorf("Name = %v, want test-cm", cm.Name)
				}
				if cm.Data["key"] != "value" {
					t.Error("Data not set correctly")
				}
			},
		},
		{
			name: "missing name",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithData(map[string]string{"key": "value"})
			},
			wantErr: true,
			errMsg:  "name is required",
		},
		{
			name: "missing data",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithName("test-cm")
			},
			wantErr: true,
			errMsg:  "at least one data entry is required",
		},
		{
			name: "with binary data",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithName("test-cm").
					WithBinaryData(map[string][]byte{"binary": []byte{0x00, 0x01}})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ConfigMapBuilder) {
				cm, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if len(cm.BinaryData["binary"]) != 2 {
					t.Error("Binary data not set correctly")
				}
			},
		},
		{
			name: "with both string and binary data",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithName("test-cm").
					WithData(map[string]string{"text": "value"}).
					WithBinaryData(map[string][]byte{"binary": []byte{0x00}})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ConfigMapBuilder) {
				cm, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if cm.Data["text"] != "value" {
					t.Error("String data not set")
				}
				if len(cm.BinaryData["binary"]) != 1 {
					t.Error("Binary data not set")
				}
			},
		},
		{
			name: "multiple data entries",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithName("test-cm").
					WithData(map[string]string{
						"config.yaml": "content1",
						"app.conf":    "content2",
					})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ConfigMapBuilder) {
				cm, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if len(cm.Data) != 2 {
					t.Errorf("Data count = %v, want 2", len(cm.Data))
				}
			},
		},
		{
			name: "with labels",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithName("test-cm").
					WithData(map[string]string{"key": "value"}).
					WithLabels(map[string]string{"custom": "label"})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ConfigMapBuilder) {
				cm, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if cm.Labels["custom"] != "label" {
					t.Error("Custom label not set")
				}
			},
		},
		{
			name: "standard labels applied",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithName("test-cm").
					WithData(map[string]string{"key": "value"})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ConfigMapBuilder) {
				cm, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if cm.Labels["app.kubernetes.io/name"] != "test-cm" {
					t.Error("Standard name label not set")
				}
				if cm.Labels["app.kubernetes.io/managed-by"] != "splunk-operator" {
					t.Error("Standard managed-by label not set")
				}
			},
		},
		{
			name: "incremental data addition",
			setupFunc: func(b *ConfigMapBuilder) {
				b.WithName("test-cm").
					WithData(map[string]string{"key1": "value1"}).
					WithData(map[string]string{"key2": "value2"})
			},
			wantErr: false,
			validate: func(t *testing.T, b *ConfigMapBuilder) {
				cm, err := b.Build()
				if err != nil {
					t.Fatalf("Build() failed: %v", err)
				}
				if cm.Data["key1"] != "value1" || cm.Data["key2"] != "value2" {
					t.Error("Incremental data addition failed")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewConfigMapBuilder("default", "test-owner")
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
