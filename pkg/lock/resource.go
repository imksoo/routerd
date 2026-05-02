package lock

import (
	"context"
	"sync"
)

type ResourceLocker struct {
	mu    sync.Mutex
	locks map[string]chan struct{}
}

func NewResourceLocker() *ResourceLocker {
	return &ResourceLocker{locks: map[string]chan struct{}{}}
}

func (l *ResourceLocker) Lock(ctx context.Context, key string) (func(), error) {
	if l == nil {
		return func() {}, nil
	}
	l.mu.Lock()
	if l.locks == nil {
		l.locks = map[string]chan struct{}{}
	}
	resourceLock := l.locks[key]
	if resourceLock == nil {
		resourceLock = make(chan struct{}, 1)
		resourceLock <- struct{}{}
		l.locks[key] = resourceLock
	}
	l.mu.Unlock()

	select {
	case <-resourceLock:
		return func() { resourceLock <- struct{}{} }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
