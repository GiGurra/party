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

func ForeachPar[T any](
	ctx *Context,
	data []T,
	processor func(t T) error,
) error {

	slog.Info("ForeachPar", "data", data)

	processItem := func(t T) {
		if ctx.err.Load() != nil {
			return // just empty the queue
		}
		err := processor(t)
		if err != nil {
			ctx.err.Store(&err)
		}
	}

	// These are our extra workers
	activeOps := sync.WaitGroup{}
	rootWaitGroup := sync.WaitGroup{}
	isRoot := false
	if ctx.workQue.Load() == nil {
		isRoot = true
		globalChan := make(chan any)
		ctx.workQue.Store(&globalChan)
		for i := 0; i < ctx.Parallelization-1; i++ {
			rootWaitGroup.Add(1)
			go func() {
				for t := range globalChan {
					slog.Info("ForeachPar worker", "data", data, "item", t)
					processItem(t.(T))
					activeOps.Done()
				}
				slog.Info("ForeachPar worker done", "data", data)
				rootWaitGroup.Done()
			}()
		}
		defer func() {
			ctx.workQue = nil
		}()
	}

	// We must always have at least one worker per level, to avoid deadlocks
	// when running through recursive calls.
	localThreadWorkQue := make(chan T)
	localThreadWaitGroup := sync.WaitGroup{}
	localThreadWaitGroup.Add(1)
	go func() {
		for t := range localThreadWorkQue {
			processItem(t)
			activeOps.Done()
		}
		localThreadWaitGroup.Done()
	}()

	// Distribute the work to the first available worker
	globalWorkQueue := *ctx.workQue.Load()
	for _, t := range data {
		if ctx.err.Load() != nil {
			break // quit early if any worker reported an error
		}
		activeOps.Add(1)
		select {
		case globalWorkQueue <- t:
		case localThreadWorkQue <- t:
		}
	}
	slog.Info("ForeachPar done enqueing", "data", data)
	activeOps.Wait() // wait for all children and children of children to finish
	if isRoot {
		slog.Info("ForeachPar closing workQue")
		close(globalWorkQueue)
	}
	close(localThreadWorkQue)

	if isRoot {
		rootWaitGroup.Wait()
	}
	localThreadWaitGroup.Wait()
	slog.Info("ForeachPar done processing", "data", data)

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
	err := ForeachPar(ctx, data, func(t T) error {
		success, err := processor(t)
		if err != nil {
			return err
		} else {
			resultQue <- success
		}
		return nil
	})
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
