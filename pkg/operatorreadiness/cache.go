package operatorreadiness

import (
	"context"
	"errors"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// InformerGetter is the narrow controller-runtime cache operation used to
// register the complete informer set before the manager starts its cache.
type InformerGetter interface {
	GetInformer(
		ctx context.Context,
		object client.Object,
		opts ...cache.InformerGetOption,
	) (cache.Informer, error)
}

// InformerRegistrar registers the informer set that controller-runtime must
// synchronize before it starts non-cache runnables or leader election.
type InformerRegistrar interface {
	Register(ctx context.Context) error
}

type informerRegistrar struct {
	informers InformerGetter
	objects   []client.Object
}

// NewInformerRegistrar creates a registrar for every object type watched by a
// registered controller. Register must be called before manager.Start.
func NewInformerRegistrar(
	informers InformerGetter,
	objects []client.Object,
) (InformerRegistrar, error) {
	if informers == nil {
		return nil, errors.New("informer registrar requires an informer getter")
	}
	if len(objects) == 0 {
		return nil, errors.New("informer registrar requires at least one object type")
	}
	for index, object := range objects {
		if object == nil {
			return nil, fmt.Errorf("informer registrar object %d is nil", index)
		}
	}
	return &informerRegistrar{
		informers: informers,
		objects:   append([]client.Object(nil), objects...),
	}, nil
}

func (r *informerRegistrar) Register(ctx context.Context) error {
	for _, object := range r.objects {
		// Registration must not wait: the cache has not been started yet.
		// controller-runtime's cache runnable subsequently waits for every
		// registered informer to complete its initial list and synchronize.
		if _, err := r.informers.GetInformer(ctx, object, cache.BlockUntilSynced(false)); err != nil {
			return fmt.Errorf("register %T informer: %w", object, err)
		}
	}
	return nil
}
