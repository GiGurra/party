package party

import (
	"context"
	"runtime"
	"sync"
	"sync/atomic"
)

// Context holds configuration and runtime state for parallel operations.
type Context struct {
	ctx        context.Context
	maxWorkers int
	err        *atomic.Pointer[error]
	workQueue  *atomic.Pointer[chan func()]
	autoClose  bool
}

// Ctx creates a new parallel processing context. Pass a context.Context to support
// cancellation, or omit for context.Background().
func Ctx(ctx ...context.Context) *Context {
	backing := context.Background()
	if len(ctx) > 0 {
		backing = ctx[0]
	}
	return &Context{
		ctx:        backing,
		maxWorkers: runtime.NumCPU(),
		err:        &atomic.Pointer[error]{},
		workQueue:  &atomic.Pointer[chan func()]{},
		autoClose:  true,
	}
}

// WithAutoClose sets whether the context should automatically close the global work queue
// when the root operation completes. Set to false when reusing a context across multiple
// top-level calls (e.g. recursive patterns), and call Close() manually when done.
func (c *Context) WithAutoClose(autoClose bool) *Context {
	if c.workQueue.Load() != nil {
		panic("cannot change auto close after workers have been spawned")
	}
	c.autoClose = autoClose
	return c
}

// WithMaxWorkers sets the maximum number of workers in the pool.
func (c *Context) WithMaxWorkers(maxWorkers int) *Context {
	if c.workQueue.Load() != nil {
		panic("cannot change max workers after workers have been spawned")
	}
	if maxWorkers < 1 {
		panic("max workers must be at least 1")
	}
	c.maxWorkers = maxWorkers
	return c
}

// Close shuts down the worker pool. Must be called when WithAutoClose(false) is used.
func (c *Context) Close() {
	if q := c.workQueue.Load(); q != nil {
		close(*q)
	}
}

// Async launches f in a new goroutine and returns a handle to await its result.
func Async[T any](f func() (T, error)) AsyncOp[T] {
	ch := make(chan Result[T], 1)
	go func() {
		v, err := f()
		ch <- Result[T]{v, err}
	}()
	return ch
}

// Await blocks until the async operation completes and returns its result.
func Await[T any](op AsyncOp[T]) (T, error) {
	r := <-op
	return r.Value, r.Err
}

// Result holds a value and an error from an async or parallel operation.
type Result[T any] struct {
	Value T
	Err   error
}

// AsyncOp is a handle to an in-flight async operation.
type AsyncOp[T any] <-chan Result[T]

// Foreach applies processor to each element of data in parallel.
// Returns the first error encountered, if any.
func Foreach[T any](
	ctx *Context,
	data []T,
	processor func(T, int) error,
) error {
	if len(data) == 0 {
		return nil
	}

	if ctx.maxWorkers == 1 {
		for i, t := range data {
			if err := processor(t, i); err != nil {
				return err
			}
		}
		return nil
	}

	if len(data) == 1 {
		return processor(data[0], 0)
	}

	var wg sync.WaitGroup

	// The first call to Foreach creates the global worker pool.
	// Subsequent (recursive) calls reuse the pool and add a local worker
	// to avoid deadlocks: the parent worker is effectively parked while
	// the local worker processes children depth-first.
	isRoot := ctx.workQueue.Load() == nil
	if isRoot {
		q := make(chan func())
		ctx.workQueue.Store(&q)
		for range ctx.maxWorkers {
			go func() {
				for work := range q {
					work()
				}
			}()
		}
	}

	// Non-root calls get a local worker to prevent deadlocks in recursive usage.
	// This doesn't increase active concurrency — it replaces the parked parent.
	localQueue := make(chan func())
	if !isRoot {
		go func() {
			for work := range localQueue {
				work()
			}
		}()
	}

	globalQueue := *ctx.workQueue.Load()
	for i, item := range data {
		select {
		case <-ctx.ctx.Done():
			err := ctx.ctx.Err()
			ctx.err.CompareAndSwap(nil, &err)
		default:
		}
		if ctx.err.Load() != nil {
			break
		}

		wg.Add(1)
		item, i := item, i
		work := func() {
			defer wg.Done()
			if ctx.err.Load() != nil {
				return
			}
			if err := processor(item, i); err != nil {
				ctx.err.CompareAndSwap(nil, &err)
			}
		}

		select {
		case globalQueue <- work:
		case localQueue <- work:
		}
	}

	wg.Wait()
	if isRoot && ctx.autoClose {
		close(globalQueue)
	}
	close(localQueue)

	if err := ctx.err.Load(); err != nil {
		return *err
	}
	return nil
}

// Map applies processor to each element of data in parallel and returns the results
// in the same order as the input.
func Map[T any, R any](
	ctx *Context,
	data []T,
	processor func(T, int) (R, error),
) ([]R, error) {
	results := make([]R, len(data))
	err := Foreach(ctx, data, func(t T, i int) error {
		r, err := processor(t, i)
		if err != nil {
			return err
		}
		results[i] = r
		return nil
	})
	if err != nil {
		return nil, err
	}
	return results, nil
}

// FlatMap applies processor to each element in parallel, then flattens the results.
func FlatMap[T any, R any](
	ctx *Context,
	data []T,
	processor func(T, int) ([]R, error),
) ([]R, error) {
	nested, err := Map(ctx, data, processor)
	if err != nil {
		return nil, err
	}
	return flatten(nested), nil
}

func flatten[T any](collection [][]T) []T {
	totalLen := 0
	for i := range collection {
		totalLen += len(collection[i])
	}
	result := make([]T, 0, totalLen)
	for i := range collection {
		result = append(result, collection[i]...)
	}
	return result
}
