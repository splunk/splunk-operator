package operatorreadiness

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const defaultInformerRegistrationRetryInterval = 10 * time.Second

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

// InformerCacheBarrier is classified by controller-runtime as a cache runnable
// because it exposes GetCache. Its cache wrapper registers the complete
// informer set and waits for that set to synchronize while the manager's HTTP
// servers remain available.
type InformerCacheBarrier struct {
	cache cache.Cache
}

type informerBarrierCache struct {
	cache.Cache
	registrar     InformerRegistrar
	logger        logr.Logger
	retryInterval time.Duration
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

// NewInformerCacheBarrier creates the manager cache-start barrier. Add the
// returned runnable to the manager before Manager.Start.
func NewInformerCacheBarrier(
	baseCache cache.Cache,
	registrar InformerRegistrar,
	logger logr.Logger,
) (*InformerCacheBarrier, error) {
	return newInformerCacheBarrier(
		baseCache,
		registrar,
		logger,
		defaultInformerRegistrationRetryInterval,
	)
}

func newInformerCacheBarrier(
	baseCache cache.Cache,
	registrar InformerRegistrar,
	logger logr.Logger,
	retryInterval time.Duration,
) (*InformerCacheBarrier, error) {
	if baseCache == nil {
		return nil, errors.New("informer cache barrier requires a cache")
	}
	if registrar == nil {
		return nil, errors.New("informer cache barrier requires a registrar")
	}
	if retryInterval <= 0 {
		return nil, errors.New("informer cache barrier requires a positive retry interval")
	}
	return &InformerCacheBarrier{
		cache: &informerBarrierCache{
			Cache:         baseCache,
			registrar:     registrar,
			logger:        logger,
			retryInterval: retryInterval,
		},
	}, nil
}

// GetCache places this runnable in controller-runtime's cache startup group.
func (b *InformerCacheBarrier) GetCache() cache.Cache {
	return b.cache
}

// Start keeps the barrier runnable alive after its cache readiness check has
// succeeded. The manager owns cancellation.
func (*InformerCacheBarrier) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

func (c *informerBarrierCache) WaitForCacheSync(ctx context.Context) bool {
	for {
		if err := c.registrar.Register(ctx); err == nil {
			return c.Cache.WaitForCacheSync(ctx)
		} else if ctx.Err() == nil {
			c.logger.Error(err, "Operator controller informer registration is not ready")
		}

		timer := time.NewTimer(c.retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}
