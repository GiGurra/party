package party

import (
	"context"
	"github.com/samber/lo"
	"github.com/samber/mo"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
)

type Context struct {
	context.Context
	Parallelization int
	err             *atomic.Pointer[error]
	workQue         *atomic.Pointer[chan any]
	orderedResults  bool
	autoClose       bool
}

func NewContext(backing context.Context) *Context {
	return &Context{
		Context:         backing,
		Parallelization: runtime.NumCPU(),
		err:             &atomic.Pointer[error]{},
		workQue:         &atomic.Pointer[chan any]{},
		orderedResults:  true,
		autoClose:       true,
	}
}

func DefaultContext() *Context {
	return NewContext(context.Background())
}

func (c *Context) WithAutoClose(autoClose bool) *Context {
	if c.workQue.Load() != nil {
		panic("Cannot change auto close after workers have been spawned")
	}
	c.autoClose = autoClose
	return c
}

func (c *Context) WithMaxWorkers(maxWorkers int) *Context {
	if c.workQue.Load() != nil {
		panic("Cannot change max workers after workers have been spawned")
	}
	c.Parallelization = maxWorkers
	return c
}

func (c *Context) WithOrderedResults(orderedResults bool) *Context {
	if c.workQue.Load() != nil {
		panic("Cannot change max workers after workers have been spawned")
	}
	c.orderedResults = orderedResults
	return c
}

func (c *Context) WithContext(ctx context.Context) *Context {
	if c.workQue.Load() != nil {
		panic("Cannot change context after workers have been spawned")
	}
	c.Context = ctx
	return c
}

func (c *Context) Close() {
	if c.workQue.Load() != nil {
		globalWorkQueue := *c.workQue.Load()
		close(globalWorkQueue)
	}
}

func Async[T any](f func() (T, error)) AsyncOp[T] {
	ch := make(chan mo.Result[T], 1)
	go func() {
		ch <- mo.TupleToResult(f())
	}()
	return ch
}

func Await[T any](ch AsyncOp[T]) (T, error) {
	res := <-ch
	return res.Get()
}

type AsyncOp[T any] <-chan mo.Result[T]

type PendingItem[T any] struct {
	item      T
	index     int
	processor func(this *PendingItem[T])
	dbg       any
}

func ForeachPar[T any](
	ctx *Context,
	data []T,
	processor func(t T, index int) error,
) error {

	if len(data) == 0 {
		return nil
	}

	if ctx.Parallelization == 1 {
		for i, t := range data {
			err := processor(t, i)
			if err != nil {
				return err
			}
		}
		return nil
	}

	if len(data) == 1 {
		return processor(data[0], 0)
	}

	pendingWork := &sync.WaitGroup{}

	// These are our extra workers
	isRoot := false
	if ctx.workQue.Load() == nil {
		isRoot = true
		globalChan := make(chan any)
		ctx.workQue.Store(&globalChan)
		for i := 0; i < ctx.Parallelization-1; i++ {
			go func() {
				for t := range globalChan {
					item := t.(PendingItem[T])
					item.processor(&item)
				}
			}()
		}
	}

	// We must always have at least one worker per level, to avoid deadlocks
	// when running through recursive calls.
	localThreadWorkQue := make(chan PendingItem[T])
	go func() {
		for item := range localThreadWorkQue {
			item.processor(&item)
		}
	}()

	// We must send the processor along with the item. This is because, otherwise the
	// root/go routine pool would capture its root processor, which would result in callbacks
	// being made to the topmost caller, while the results come from inner calls.
	processItem := func(item *PendingItem[T]) {
		if ctx.err.Load() != nil {
			pendingWork.Done()
			return // just empty the queue
		}
		err := processor(item.item, item.index)
		if err != nil {
			ctx.err.CompareAndSwap(nil, &err) // we only want to output the first error
		}
		pendingWork.Done()
	}

	// Distribute the work to the first available worker
	globalWorkQueue := *ctx.workQue.Load()
	for i, itemData := range data {
		// Check if context is stopped
		select {
		case <-ctx.Done():
			err := ctx.Err()
			if err != nil {
				ctx.err.CompareAndSwap(nil, &err)
			} else {
				ctx.err.CompareAndSwap(nil, &context.Canceled)
			}
		default:
			// continue
		}
		if ctx.err.Load() != nil {
			break // quit early if any worker reported an error
		}
		pendingWork.Add(1)
		select {
		case globalWorkQueue <- PendingItem[T]{itemData, i, processItem, data}:
		case localThreadWorkQue <- PendingItem[T]{itemData, i, processItem, data}:
		}
	}
	pendingWork.Wait()
	if isRoot && ctx.autoClose {
		close(globalWorkQueue)
	}
	close(localThreadWorkQue)

	if err := ctx.err.Load(); err != nil {
		return *err
	}

	return nil
}

type indexedResult[R any] struct {
	result R
	index  int
}

func MapPar[T any, R any](
	ctx *Context,
	data []T,
	processor func(t T, index int) (R, error),
) ([]R, error) {
	resultQue := make(chan indexedResult[R], len(data))
	err := ForeachPar(ctx, data, func(t T, index int) error {
		success, err := processor(t, index)
		if err != nil {
			return err
		} else {
			resultQue <- indexedResult[R]{success, index}
		}
		return nil
	})
	close(resultQue)
	wrappedResults := lo.ChannelToSlice(resultQue)
	if ctx.orderedResults {
		slices.SortFunc(wrappedResults, func(a, b indexedResult[R]) int {
			return a.index - b.index
		})
	}

	return lo.Map(wrappedResults, func(item indexedResult[R], _ int) R {
		return item.result
	}), err
}

func FlatMapPar[T any, R any](
	ctx *Context,
	data []T,
	processor func(t T, index int) ([]R, error),
) ([]R, error) {
	nested, err := MapPar(ctx, data, processor)
	return lo.Flatten(nested), err
}
