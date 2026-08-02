package operatorreadiness

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InformerGetter is the narrow controller-runtime cache operation used during
// manager warmup. GetInformer blocks until the requested informer has synced
// when the cache is already running.
type InformerGetter interface {
	GetInformer(
		ctx context.Context,
		object client.Object,
		opts ...cache.InformerGetOption,
	) (cache.Informer, error)
}

type informerCacheSynchronizer struct {
	informers InformerGetter
	objects   []client.Object
}

// NewInformerCacheSynchronizer creates an explicit warmup boundary for every
// object type watched by a registered controller.
func NewInformerCacheSynchronizer(
	informers InformerGetter,
	objects []client.Object,
) (CacheSynchronizer, error) {
	if informers == nil {
		return nil, errors.New("informer cache synchronizer requires an informer getter")
	}
	if len(objects) == 0 {
		return nil, errors.New("informer cache synchronizer requires at least one object type")
	}
	for index, object := range objects {
		if object == nil {
			return nil, fmt.Errorf("informer cache synchronizer object %d is nil", index)
		}
	}
	return &informerCacheSynchronizer{
		informers: informers,
		objects:   append([]client.Object(nil), objects...),
	}, nil
}

func (s *informerCacheSynchronizer) Synchronize(ctx context.Context) error {
	for _, object := range s.objects {
		if _, err := s.informers.GetInformer(ctx, object); err != nil {
			return fmt.Errorf("synchronize %T informer: %w", object, err)
		}
	}
	return nil
}
