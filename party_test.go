package party

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"testing"
	"time"
)

func TestAsyncAwait(t *testing.T) {
	t0 := time.Now()

	asyncOp := Async(func() (int, error) {
		time.Sleep(500 * time.Millisecond)
		return 42, nil
	})

	t1 := time.Now()

	result, err := Await(asyncOp)
	if err != nil {
		t.Fatalf("Await() error: %v", err)
	}

	t2 := time.Now()

	if result != 42 {
		t.Fatalf("Await() result: %d", result)
	}

	// t1 should not be more than 100 ms after t0
	if t1.Sub(t0) > 100*time.Millisecond {
		t.Fatalf("Await() should not block for 100ms")
	}

	// t2 should be more than 450 ms after t0
	if t2.Sub(t0) < 450*time.Millisecond {
		t.Fatalf("Await() should block for 500ms")

	}

	// forward errors
	asyncOp = Async(func() (int, error) {
		return 0, fmt.Errorf("error")
	})

	_, err = Await(asyncOp)
	if err == nil {
		t.Fatalf("Await() error expected")
	}

	if err.Error() != "error" {
		t.Fatalf("Await() error mismatch")
	}
}

func TestMapPar(t *testing.T) {
	items := makeRange(1000)

	refResult := make([]int, 1000)
	for i := range refResult {
		refResult[i] = i * 2
	}

	result, err := MapPar(DefaultContext(), items, func(item int) (int, error) {
		randSleep := time.Duration(rand.Int64N(10))
		time.Sleep(randSleep * time.Millisecond)
		return item * 2, nil
	})

	if err != nil {
		t.Fatalf("ParallelProcessRet() error: %v", err)
	}

	serialResult, err := MapPar(DefaultContext().WithMaxWorkers(1), items, func(item int) (int, error) {
		return item * 2, nil
	})

	if err != nil {
		t.Fatalf("ParallelProcessRet() error: %v", err)
	}

	if len(result) != len(refResult) {
		t.Fatalf("ParallelProcessRet() length: %d", len(result))
	}

	if len(serialResult) != len(refResult) {
		t.Fatalf("ParallelProcessRet() length: %d", len(result))
	}

	for i := range serialResult {
		if serialResult[i] != refResult[i] {
			t.Fatalf("ParallelProcessRet() mismatch: %d", i)
		}
	}

	// par set must not equal ref set
	parRefAreEqual := true
	for i := range refResult {
		if refResult[i] != result[i] {
			parRefAreEqual = false
			break
		}
	}
	if parRefAreEqual {
		t.Fatalf("ParallelProcessRet() result must not equal ref result")
	}

	refSet := toSet(refResult)
	parSet := toSet(result)
	for k := range refSet {
		if !parSet[k] {
			t.Fatalf("ParallelProcessRet() key mismatch: %d", k)
		}
	}

	_, err = MapPar(DefaultContext().WithMaxWorkers(100), items, func(item int) (int, error) {
		if item > 150 {
			return 0, fmt.Errorf("error")
		} else {
			return item, nil
		}
	})

	if err == nil {
		t.Fatalf("ParallelProcessRet() error expected")
	}

	if err.Error() != "error" {
		t.Fatalf("ParallelProcessRet() error mismatch")
	}

}

func recFn(ctx *Context, item int) ([]int, error) {
	slog.Info("recFn", "item", item)
	if item == 0 {
		slog.Info("recFn", "item", item, "returning final", 0)
		return []int{0}, nil
	} else {
		innerRange := makeRange(item - 1)
		return MapPar(ctx, innerRange, func(t int) (int, error) {
			innerRes, err := recFn(ctx, t)
			if err != nil {
				return 0, err
			} else {
				return len(innerRes), nil
			}
		})
	}
}

func TestMapParRec(t *testing.T) {
	items := makeRange(4)

	ctx := DefaultContext().WithMaxWorkers(3)

	res, err := MapPar(ctx, items, func(item int) ([]int, error) {
		return recFn(ctx, item)
	})

	if err != nil {
		t.Fatalf("ParallelProcessRet() error: %v", err)
	}

	fmt.Printf("res: %v\n", res)

	//if len(res) != 5 {
	//	t.Fatalf("ParallelProcessRet() length: %d", len(res))
	//
	//}
}

func toSet[T comparable](items []T) map[T]bool {
	result := make(map[T]bool)
	for _, item := range items {
		result[item] = true
	}
	return result
}

func makeRange(n int) []int {
	result := make([]int, n)
	for i := range result {
		result[i] = i
	}
	return result
}
