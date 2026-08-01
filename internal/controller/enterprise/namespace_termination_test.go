package controller

import (
	"context"
	"errors"
	"fmt"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

func TestShouldStopForTerminatingNamespace(t *testing.T) {
	t.Parallel()

	now := metav1.Now()
	tests := []struct {
		name      string
		namespace *corev1.Namespace
		wantStop  bool
	}{
		{
			name:      "active namespace continues",
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "active"}},
			wantStop:  false,
		},
		{
			name: "deletion timestamp stops",
			namespace: &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
				Name:              "deleting",
				DeletionTimestamp: &now,
				Finalizers:        []string{"kubernetes"},
			}},
			wantStop: true,
		},
		{
			name: "terminating phase stops",
			namespace: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: "terminating"},
				Status:     corev1.NamespaceStatus{Phase: corev1.NamespaceTerminating},
			},
			wantStop: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			scheme := runtime.NewScheme()
			if err := corev1.AddToScheme(scheme); err != nil {
				t.Fatalf("add core scheme: %v", err)
			}
			reader := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.namespace).Build()

			stop, err := shouldStopForTerminatingNamespace(context.Background(), reader, tt.namespace.Name)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if stop != tt.wantStop {
				t.Fatalf("stop = %v, want %v", stop, tt.wantStop)
			}
		})
	}
}

func TestShouldStopForMissingNamespace(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	reader := fake.NewClientBuilder().WithScheme(scheme).Build()

	stop, err := shouldStopForTerminatingNamespace(context.Background(), reader, "absent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stop {
		t.Fatal("missing namespace must stop ordinary reconciliation")
	}
}

func TestShouldStopForNamespaceReadError(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	wantErr := errors.New("namespace read denied")
	reader := fake.NewClientBuilder().
		WithScheme(scheme).
		WithInterceptorFuncs(interceptor.Funcs{
			Get: func(
				ctx context.Context,
				c client.WithWatch,
				key types.NamespacedName,
				obj client.Object,
				opts ...client.GetOption,
			) error {
				if _, ok := obj.(*corev1.Namespace); ok {
					return wantErr
				}
				return c.Get(ctx, key, obj, opts...)
			},
		}).
		Build()

	stop, err := shouldStopForTerminatingNamespace(context.Background(), reader, "denied")
	if stop {
		t.Fatal("read error must not be reported as a known terminating namespace")
	}
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want wrapped %v", err, wantErr)
	}
}

func TestNamespaceTerminatingAdmissionErrorClassification(t *testing.T) {
	t.Parallel()

	if !handleNamespaceTerminatingAdmissionError(context.Background(), "test", newNamespaceTerminatingAdmissionError()) {
		t.Fatal("wrapped NamespaceTerminating admission error must be classified")
	}
	otherForbidden := k8serrors.NewForbidden(
		schema.GroupResource{Resource: "configmaps"},
		"test",
		errors.New("policy denied"),
	)
	if handleNamespaceTerminatingAdmissionError(context.Background(), "test", otherForbidden) {
		t.Fatal("an unrelated forbidden error must not be classified")
	}
	if handleNamespaceTerminatingAdmissionError(context.Background(), "test", nil) {
		t.Fatal("nil must not be classified")
	}
}

func newNamespaceTerminatingAdmissionError() error {
	statusErr := k8serrors.NewForbidden(
		schema.GroupResource{Resource: "configmaps"},
		"test",
		errors.New("unable to create new content because the namespace is being terminated"),
	)
	statusErr.ErrStatus.Details.Causes = []metav1.StatusCause{{
		Type:    corev1.NamespaceTerminatingCause,
		Message: "namespace test is being terminated",
	}}
	return fmt.Errorf("apply ConfigMap: %w", statusErr)
}
