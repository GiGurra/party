package party

import (
	"github.com/samber/lo"
	"github.com/samber/mo"
	"log/slog"
	"sync"
)

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

func ForeachPar[T any](parallelization int, data []T, processor func(t T)) {
	if parallelization < 1 {
		slog.Error("parallelization must be greater than 0")
		parallelization = 1
	}
	wg := sync.WaitGroup{}
	wg.Add(parallelization)
	workQue := make(chan T, parallelization)
	for i := 0; i < parallelization; i++ {
		go func() {
			for t := range workQue {
				processor(t)
			}
			wg.Done()
		}()
	}
	for _, t := range data {
		workQue <- t
	}
	close(workQue)

	wg.Wait()
}

func MapPar[T any, R any](parallelization int, data []T, processor func(t T) (R, error)) ([]R, error) {
	resultQue := make(chan mo.Result[R], len(data))
	ForeachPar(parallelization, data, func(t T) {
		resultQue <- mo.TupleToResult(processor(t))
	})
	close(resultQue)

	return Collect(lo.ChannelToSlice(resultQue))
}

func FlatMapPar[T any, R any](parallelization int, data []T, processor func(t T) ([]R, error)) ([]R, error) {
	nested, err := MapPar(parallelization, data, processor)
	return lo.Flatten(nested), err
}

func Collect[T any](ops []mo.Result[T]) ([]T, error) {
	result := make([]T, len(ops))
	for i, op := range ops {
		res, err := op.Get()
		if err != nil {
			return nil, err
		}
		result[i] = res
	}
	return result, nil
}
