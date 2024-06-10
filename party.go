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
	MaxWorkers  int
	workerCount *atomic.Int32
	err         *atomic.Pointer[error]
}

func NewContext(backing context.Context) *Context {
	return &Context{
		Context:     backing,
		MaxWorkers:  runtime.NumCPU(),
		workerCount: &atomic.Int32{},
		err:         &atomic.Pointer[error]{},
	}
}

func DefaultContext() *Context {
	return NewContext(context.Background())
}

func (c *Context) WithMaxWorkers(maxWorkers int) *Context {
	c.MaxWorkers = maxWorkers
	return c
}

func (c *Context) WithContext(ctx context.Context) *Context {
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
	idealWorkerCount := ctx.MaxWorkers
	if idealWorkerCount < 1 {
		slog.Error("Parallelization must be greater than 0")
		idealWorkerCount = 1
	}

	if len(data) < idealWorkerCount {
		idealWorkerCount = len(data)
	}

	nextItem := 0

tryReserveSlots:
	itemsLeft := len(data) - nextItem

	if itemsLeft == 0 {
		return nil
	}

	if itemsLeft < idealWorkerCount {
		idealWorkerCount = itemsLeft
	}
	existingWorkerCount := int(ctx.workerCount.Load())
	workerSlotsLeft := ctx.MaxWorkers - existingWorkerCount

	if workerSlotsLeft <= 1 {
		// run sequentially (triggers on for example when parallelizing recursive algorithms)
		// We can't wait for slots to become available in recursive algorithms, because it
		// might be the parents that are waiting, so we must run sequentially when we are out of slots.
		for nextItem < len(data) {
			err := processor(data[nextItem])
			if err != nil {
				return err
			}
			nextItem++
			goto tryReserveSlots
		}
	} else {

		workerSlotsReserved := min(idealWorkerCount, workerSlotsLeft)
		wasReserved := ctx.workerCount.CompareAndSwap(int32(existingWorkerCount), int32(existingWorkerCount+workerSlotsReserved))
		if !wasReserved {
			goto tryReserveSlots
		}

		wg := sync.WaitGroup{}
		wg.Add(workerSlotsReserved)
		workQue := make(chan T, workerSlotsReserved)
		for i := 0; i < workerSlotsReserved; i++ {
			go func() {
				ctx.workerCount.Add(1)
				for t := range workQue {
					if ctx.err.Load() != nil {
						break
					}
					err := processor(t)
					if err != nil {
						ctx.err.Store(&err)
					}
				}
				wg.Done()
				ctx.workerCount.Add(-1)
			}()
		}
		for nextItem < len(data) {
		tryAgain:
			select {
			case workQue <- data[nextItem]:
				nextItem++ // ok!
			default:
				// queue is full, wait for a slot to become available
				if ctx.err.Load() != nil {
					return *ctx.err.Load()
				} else {
					goto tryAgain
				}
			}
		}
		close(workQue)

		wg.Wait()
	}

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
