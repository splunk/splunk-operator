package operatorreadiness

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type informerRegistrarFunc func(context.Context) error

func (f informerRegistrarFunc) Register(ctx context.Context) error {
	return f(ctx)
}

type fakeInformerGetter struct {
	objects          []client.Object
	blockUntilSynced []*bool
	err              error
}

func (f *fakeInformerGetter) GetInformer(
	_ context.Context,
	object client.Object,
	opts ...cache.InformerGetOption,
) (cache.Informer, error) {
	f.objects = append(f.objects, object)
	options := cache.InformerGetOptions{}
	for _, option := range opts {
		option(&options)
	}
	f.blockUntilSynced = append(f.blockUntilSynced, options.BlockUntilSynced)
	if f.err != nil {
		return nil, f.err
	}
	return nil, nil
}

func TestInformerRegistrarRegistersEveryRequiredInformerWithoutBlocking(t *testing.T) {
	getter := &fakeInformerGetter{}
	registrar, err := NewInformerRegistrar(getter, []client.Object{
		&corev1.Pod{},
		&appsv1.StatefulSet{},
	})
	if err != nil {
		t.Fatalf("NewInformerRegistrar() error = %v", err)
	}
	if err := registrar.Register(context.Background()); err != nil {
		t.Fatalf("Register() error = %v", err)
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
	for index, blockUntilSynced := range getter.blockUntilSynced {
		if blockUntilSynced == nil || *blockUntilSynced {
			t.Fatalf("informer %d BlockUntilSynced = %v, want false", index, blockUntilSynced)
		}
	}
}

func TestInformerRegistrarReturnsTypedFailure(t *testing.T) {
	registrar, err := NewInformerRegistrar(
		&fakeInformerGetter{err: errors.New("list forbidden")},
		[]client.Object{&corev1.Secret{}},
	)
	if err != nil {
		t.Fatalf("NewInformerRegistrar() error = %v", err)
	}
	err = registrar.Register(context.Background())
	if err == nil || !strings.Contains(err.Error(), "*v1.Secret") || !strings.Contains(err.Error(), "list forbidden") {
		t.Fatalf("Register() error = %v, want typed informer failure", err)
	}
}

func TestNewInformerRegistrarValidatesInputs(t *testing.T) {
	if _, err := NewInformerRegistrar(nil, []client.Object{&corev1.Pod{}}); err == nil {
		t.Fatal("nil informer getter error = nil")
	}
	if _, err := NewInformerRegistrar(&fakeInformerGetter{}, nil); err == nil {
		t.Fatal("empty informer set error = nil")
	}
	if _, err := NewInformerRegistrar(&fakeInformerGetter{}, []client.Object{nil}); err == nil {
		t.Fatal("nil informer object error = nil")
	}
}

func TestInformerCacheBarrierRetriesRegistrationBeforeCacheSync(t *testing.T) {
	synchronized := true
	baseCache := &informertest.FakeInformers{Synced: &synchronized}
	calls := 0
	registrar := informerRegistrarFunc(func(context.Context) error {
		calls++
		if calls == 1 {
			return errors.New("API discovery unavailable")
		}
		return nil
	})
	barrier, err := newInformerCacheBarrier(baseCache, registrar, logr.Discard(), time.Millisecond)
	if err != nil {
		t.Fatalf("newInformerCacheBarrier() error = %v", err)
	}
	if !barrier.GetCache().WaitForCacheSync(context.Background()) {
		t.Fatal("WaitForCacheSync() = false, want true")
	}
	if calls != 2 {
		t.Fatalf("registrar calls = %d, want 2", calls)
	}
}

func TestInformerCacheBarrierStartStopsWithManagerContext(t *testing.T) {
	synchronized := true
	barrier, err := newInformerCacheBarrier(
		&informertest.FakeInformers{Synced: &synchronized},
		informerRegistrarFunc(func(context.Context) error { return nil }),
		logr.Discard(),
		time.Millisecond,
	)
	if err != nil {
		t.Fatalf("newInformerCacheBarrier() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- barrier.Start(ctx) }()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not stop after context cancellation")
	}
}

func TestNewInformerCacheBarrierValidatesInputs(t *testing.T) {
	synchronized := true
	baseCache := &informertest.FakeInformers{Synced: &synchronized}
	registrar := informerRegistrarFunc(func(context.Context) error { return nil })
	if _, err := newInformerCacheBarrier(nil, registrar, logr.Discard(), time.Second); err == nil {
		t.Fatal("nil cache error = nil")
	}
	if _, err := newInformerCacheBarrier(baseCache, nil, logr.Discard(), time.Second); err == nil {
		t.Fatal("nil registrar error = nil")
	}
	if _, err := newInformerCacheBarrier(baseCache, registrar, logr.Discard(), 0); err == nil {
		t.Fatal("non-positive retry interval error = nil")
	}
}
