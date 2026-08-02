package operatorreadiness

import (
	"context"
	"errors"
	"strings"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type fakeInformerGetter struct {
	objects []client.Object
	err     error
}

func (f *fakeInformerGetter) GetInformer(
	_ context.Context,
	object client.Object,
	_ ...cache.InformerGetOption,
) (cache.Informer, error) {
	f.objects = append(f.objects, object)
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

func TestInformerCacheSynchronizerRequestsEveryRequiredInformer(t *testing.T) {
	getter := &fakeInformerGetter{}
	synchronizer, err := NewInformerCacheSynchronizer(getter, []client.Object{
		&corev1.Pod{},
		&appsv1.StatefulSet{},
	})
	if err != nil {
		t.Fatalf("NewInformerCacheSynchronizer() error = %v", err)
	}
	if err := synchronizer.Synchronize(context.Background()); err != nil {
		t.Fatalf("Synchronize() error = %v", err)
	}
	if len(getter.objects) != 2 {
		t.Fatalf("informer requests = %d, want 2", len(getter.objects))
	}
	if _, ok := getter.objects[0].(*corev1.Pod); !ok {
		t.Fatalf("first informer object = %T, want *v1.Pod", getter.objects[0])
	}
	if _, ok := getter.objects[1].(*appsv1.StatefulSet); !ok {
		t.Fatalf("second informer object = %T, want *v1.StatefulSet", getter.objects[1])
	}
}

func TestInformerCacheSynchronizerReturnsTypedFailure(t *testing.T) {
	synchronizer, err := NewInformerCacheSynchronizer(
		&fakeInformerGetter{err: errors.New("list forbidden")},
		[]client.Object{&corev1.Secret{}},
	)
	if err != nil {
		t.Fatalf("NewInformerCacheSynchronizer() error = %v", err)
	}
	err = synchronizer.Synchronize(context.Background())
	if err == nil || !strings.Contains(err.Error(), "*v1.Secret") || !strings.Contains(err.Error(), "list forbidden") {
		t.Fatalf("Synchronize() error = %v, want typed informer failure", err)
	}
}

func TestNewInformerCacheSynchronizerValidatesInputs(t *testing.T) {
	if _, err := NewInformerCacheSynchronizer(nil, []client.Object{&corev1.Pod{}}); err == nil {
		t.Fatal("nil informer getter error = nil")
	}
	if _, err := NewInformerCacheSynchronizer(&fakeInformerGetter{}, nil); err == nil {
		t.Fatal("empty informer set error = nil")
	}
	if _, err := NewInformerCacheSynchronizer(&fakeInformerGetter{}, []client.Object{nil}); err == nil {
		t.Fatal("nil informer object error = nil")
	}
}
