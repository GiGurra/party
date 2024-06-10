package party

import (
	"context"
	"github.com/samber/lo"
	"github.com/samber/mo"
	"runtime"
	"sync"
	"sync/atomic"
)

type Context struct {
	context.Context
	Parallelization int
	err             *atomic.Pointer[error]
	workQue         chan any
}

func NewContext(backing context.Context) *Context {
	return &Context{
		Context:         backing,
		Parallelization: runtime.NumCPU(),
		err:             &atomic.Pointer[error]{},
	}
}

func DefaultContext() *Context {
	return NewContext(context.Background())
}

func (c *Context) WithMaxWorkers(maxWorkers int) *Context {
	if c.workQue != nil {
		panic("Cannot change max workers after workers have been spawned")
	}
	c.Parallelization = maxWorkers
	return c
}

func (c *Context) WithContext(ctx context.Context) *Context {
	if c.workQue != nil {
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

	if len(data) == 0 {
		return nil
	}

	if len(data) == 1 {
		return processor(data[0])
	}

	if ctx.Parallelization <= 1 {
		for _, t := range data {
			err := processor(t)
			if err != nil {
				return err
			}
		}
		return nil
	}

	workersWaitGroup := sync.WaitGroup{}
	if ctx.workQue == nil {
		ctx.workQue = make(chan any)
		for i := 0; i < ctx.Parallelization-1; i++ {
			workersWaitGroup.Add(1)
			go func() {
				for t := range ctx.workQue {
					if ctx.err.Load() != nil {
						continue // just empty the queue
					}
					err := processor(t.(T))
					if err != nil {
						ctx.err.Store(&err)
					}
				}
				workersWaitGroup.Done()
			}()
		}
		defer func() {
			ctx.workQue = nil
		}()
	}

	localThreadWorkQue := make(chan T)
	localThreadWaitGroup := sync.WaitGroup{}
	localThreadWaitGroup.Add(1)

	// We must always have at least one worker, to avoid deadlocks
	// when running through recursive calls.
	go func() {
		for t := range localThreadWorkQue {
			if ctx.err.Load() != nil {
				continue // just empty the queue
			}
			err := processor(t)
			if err != nil {
				ctx.err.Store(&err)
			}
		}
		localThreadWaitGroup.Done()
	}()

	// Distribute the work to the first available worker
	for _, t := range data {
		if ctx.err.Load() != nil {
			break // quit early if any worker reported an error
		}
		select {
		case ctx.workQue <- t:
		case localThreadWorkQue <- t:
		}
	}
	close(ctx.workQue)
	close(localThreadWorkQue)

	localThreadWaitGroup.Wait()
	workersWaitGroup.Wait()

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
