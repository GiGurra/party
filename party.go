package party

import (
	"context"
	"github.com/samber/lo"
	"github.com/samber/mo"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
)

type Context struct {
	context.Context
	Parallelization int
	err             *atomic.Pointer[error]
	workQue         *atomic.Pointer[chan any]
}

func NewContext(backing context.Context) *Context {
	return &Context{
		Context:         backing,
		Parallelization: runtime.NumCPU(),
		err:             &atomic.Pointer[error]{},
		workQue:         &atomic.Pointer[chan any]{},
	}
}

func DefaultContext() *Context {
	return NewContext(context.Background())
}

func (c *Context) WithMaxWorkers(maxWorkers int) *Context {
	if c.workQue.Load() != nil {
		panic("Cannot change max workers after workers have been spawned")
	}
	c.Parallelization = maxWorkers
	return c
}

func (c *Context) WithContext(ctx context.Context) *Context {
	if c.workQue.Load() != nil {
		panic("Cannot change context after workers have been spawned")
	}
	c.Context = ctx
	return c
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
	item T
	wg   *sync.WaitGroup
}

func ForeachPar[T any](
	ctx *Context,
	data []T,
	processor func(t T) error,
) error {

	if len(data) == 0 {
		return nil
	}

	if ctx.Parallelization == 1 {
		for _, t := range data {
			err := processor(t)
			if err != nil {
				return err
			}
		}
		return nil
	}

	if len(data) == 1 {
		return processor(data[0])
	}

	slog.Info("ForeachPar BEGIN", "data", data)

	// This could be defered to the root workers/workqueue
	processItem := func(item PendingItem[T]) {
		if ctx.err.Load() != nil {
			item.wg.Done()
			return // just empty the queue
		}
		err := processor(item.item)
		if err != nil {
			ctx.err.Store(&err)
		}
		item.wg.Done()
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
					processItem(t.(PendingItem[T]))
				}
			}()
		}
	}

	// We must always have at least one worker per level, to avoid deadlocks
	// when running through recursive calls.
	localThreadWorkQue := make(chan PendingItem[T])
	go func() {
		for t := range localThreadWorkQue {
			processItem(t)
		}
	}()

	// Distribute the work to the first available worker
	globalWorkQueue := *ctx.workQue.Load()
	for _, t := range data {
		if ctx.err.Load() != nil {
			break // quit early if any worker reported an error
		}
		slog.Info("ForeachPar ENQ", "data", data, "t", t)
		pendingWork.Add(1)
		select {
		case globalWorkQueue <- PendingItem[T]{t, pendingWork}:
		case localThreadWorkQue <- PendingItem[T]{t, pendingWork}:
		}
	}
	pendingWork.Wait()
	if isRoot {
		close(globalWorkQueue)
	}
	close(localThreadWorkQue)

	slog.Info("ForeachPar END", "data", data)

	if err := ctx.err.Load(); err != nil {
		return *err
	}

	return nil
}

func MapPar[T any, R any](
	ctx *Context,
	data []T,
	processor func(t T) (R, error),
) ([]R, error) {
	resultQue := make(chan R, len(data))
	slog.Info("MapPar BEGIN", "data", data)
	err := ForeachPar(ctx, data, func(t T) error {
		success, err := processor(t)
		if err != nil {
			return err
		} else {
			resultQue <- success
		}
		return nil
	})
	slog.Info("MapPar END", "data", data)
	close(resultQue)

	return lo.ChannelToSlice(resultQue), err
}

func FlatMapPar[T any, R any](
	ctx *Context,
	data []T,
	processor func(t T) ([]R, error),
) ([]R, error) {
	nested, err := MapPar(ctx, data, processor)
	return lo.Flatten(nested), err
}
