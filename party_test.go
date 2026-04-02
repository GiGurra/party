package party

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync/atomic"
	"testing"
	"time"
)

// --- Async / Await ---

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

	if t1.Sub(t0) > 100*time.Millisecond {
		t.Fatalf("Async() should return immediately")
	}

	if t2.Sub(t0) < 450*time.Millisecond {
		t.Fatalf("Await() should block until result is ready")
	}
}

func TestAsyncAwaitError(t *testing.T) {
	op := Async(func() (int, error) {
		return 0, fmt.Errorf("test error")
	})
	_, err := Await(op)
	if err == nil {
		t.Fatalf("expected error")
	}
	if err.Error() != "test error" {
		t.Fatalf("error = %q, want %q", err.Error(), "test error")
	}
}

func TestAsyncAwaitMultiple(t *testing.T) {
	op1 := Async(func() (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "first", nil
	})
	op2 := Async(func() (string, error) {
		time.Sleep(100 * time.Millisecond)
		return "second", nil
	})

	t0 := time.Now()
	r1, _ := Await(op1)
	r2, _ := Await(op2)
	elapsed := time.Since(t0)

	if r1 != "first" || r2 != "second" {
		t.Fatalf("results: %q, %q", r1, r2)
	}
	// Both run concurrently, so total time should be ~100ms not ~200ms
	if elapsed > 180*time.Millisecond {
		t.Fatalf("expected concurrent execution, took %v", elapsed)
	}
}

// --- Empty and single-element edge cases ---

func TestForeachEmpty(t *testing.T) {
	err := Foreach(Ctx(), []int{}, func(item int, _ int) error {
		t.Fatal("should not be called")
		return nil
	})
	if err != nil {
		t.Fatalf("Foreach(empty) error: %v", err)
	}
}

func TestMapEmpty(t *testing.T) {
	results, err := Map(Ctx(), []string{}, func(s string, _ int) (int, error) {
		t.Fatal("should not be called")
		return 0, nil
	})
	if err != nil {
		t.Fatalf("Map(empty) error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("Map(empty) len = %d", len(results))
	}
}

func TestFlatMapEmpty(t *testing.T) {
	results, err := FlatMap(Ctx(), []int{}, func(i int, _ int) ([]string, error) {
		t.Fatal("should not be called")
		return nil, nil
	})
	if err != nil {
		t.Fatalf("FlatMap(empty) error: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("FlatMap(empty) len = %d", len(results))
	}
}

func TestMapSingleElement(t *testing.T) {
	result, err := Map(Ctx(), []int{7}, func(item int, i int) (int, error) {
		if i != 0 {
			t.Fatalf("index = %d, want 0", i)
		}
		return item * 3, nil
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	assertSliceEqual(t, result, []int{21})
}

// --- Foreach ---

func TestForeach(t *testing.T) {
	var count atomic.Int64
	items := makeRange(100)

	err := Foreach(Ctx().WithMaxWorkers(4), items, func(item int, _ int) error {
		count.Add(1)
		return nil
	})
	if err != nil {
		t.Fatalf("Foreach() error: %v", err)
	}
	if count.Load() != 100 {
		t.Fatalf("processed %d items, want 100", count.Load())
	}
}

func TestForeachError(t *testing.T) {
	err := Foreach(Ctx().WithMaxWorkers(4), makeRange(100), func(item int, _ int) error {
		if item == 50 {
			return fmt.Errorf("item 50 failed")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestForeachSerial(t *testing.T) {
	var order []int
	items := makeRange(10)

	err := Foreach(Ctx().WithMaxWorkers(1), items, func(item int, _ int) error {
		order = append(order, item)
		return nil
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// Serial execution preserves input order
	assertSliceEqual(t, order, items)
}

func TestForeachSerialError(t *testing.T) {
	err := Foreach(Ctx().WithMaxWorkers(1), makeRange(10), func(item int, _ int) error {
		if item == 3 {
			return fmt.Errorf("serial fail")
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "serial fail" {
		t.Fatalf("error = %q, want %q", err.Error(), "serial fail")
	}
}

// --- Map ---

func TestMapOrdering(t *testing.T) {
	items := makeRange(1000)

	expected := make([]int, 1000)
	for i := range expected {
		expected[i] = i * 2
	}

	// Parallel with random delays — results must still be ordered
	result, err := Map(Ctx(), items, func(item int, _ int) (int, error) {
		time.Sleep(time.Duration(rand.Int64N(10)) * time.Millisecond)
		return item * 2, nil
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	assertSliceEqual(t, result, expected)
}

func TestMapSerial(t *testing.T) {
	items := makeRange(100)
	expected := make([]int, 100)
	for i := range expected {
		expected[i] = i * 2
	}

	result, err := Map(Ctx().WithMaxWorkers(1), items, func(item int, _ int) (int, error) {
		return item * 2, nil
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	assertSliceEqual(t, result, expected)
}

func TestMapError(t *testing.T) {
	_, err := Map(Ctx().WithMaxWorkers(100), makeRange(1000), func(item int, _ int) (int, error) {
		if item > 150 {
			return 0, fmt.Errorf("too large")
		}
		return item, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "too large" {
		t.Fatalf("error = %q, want %q", err.Error(), "too large")
	}
}

func TestMapTypeConversion(t *testing.T) {
	words := []string{"hello", "world", "foo"}
	lengths, err := Map(Ctx(), words, func(s string, _ int) (int, error) {
		return len(s), nil
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	assertSliceEqual(t, lengths, []int{5, 5, 3})
}

// --- FlatMap ---

func TestFlatMap(t *testing.T) {
	items := []int{1, 2, 3}
	result, err := FlatMap(Ctx(), items, func(n int, _ int) ([]int, error) {
		out := make([]int, n)
		for i := range out {
			out[i] = n
		}
		return out, nil
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	// 1 -> [1], 2 -> [2,2], 3 -> [3,3,3]
	assertSliceEqual(t, result, []int{1, 2, 2, 3, 3, 3})
}

func TestFlatMapError(t *testing.T) {
	_, err := FlatMap(Ctx(), []int{1, 2, 3}, func(n int, _ int) ([]string, error) {
		if n == 2 {
			return nil, fmt.Errorf("bad item")
		}
		return []string{fmt.Sprint(n)}, nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Bounded concurrency ---

func TestMaxWorkersBounds(t *testing.T) {
	var active atomic.Int64
	var maxSeen atomic.Int64
	maxWorkers := 4

	err := Foreach(Ctx().WithMaxWorkers(maxWorkers), makeRange(100), func(_ int, _ int) error {
		cur := active.Add(1)
		for {
			old := maxSeen.Load()
			if cur <= old || maxSeen.CompareAndSwap(old, cur) {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
		active.Add(-1)
		return nil
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	seen := maxSeen.Load()
	// The distributing goroutine + maxWorkers pool workers can run concurrently,
	// but active workers should not wildly exceed maxWorkers
	if seen > int64(maxWorkers)+1 {
		t.Fatalf("max concurrent = %d, want <= %d", seen, maxWorkers+1)
	}
}

// --- Recursive ---

func TestMapRecursive(t *testing.T) {
	depth := 10
	items := makeRange(depth)

	ctx := Ctx().
		WithMaxWorkers(3).
		WithAutoClose(false)
	defer ctx.Close()

	res, err := Map(ctx, items, func(item int, _ int) ([]int, error) {
		return recFn(ctx, item)
	})
	if err != nil {
		t.Fatalf("Map(recursive) error: %v", err)
	}
	if len(res) != depth {
		t.Fatalf("Map(recursive) len = %d, want %d", len(res), depth)
	}

	fmRes, err := FlatMap(ctx, items, func(item int, _ int) ([]int, error) {
		return recFn(ctx, item)
	})
	if err != nil {
		t.Fatalf("FlatMap(recursive) error: %v", err)
	}

	expFmRes := []int{0, 1, 1, 1, 1, 1, 2, 1, 1, 2, 3, 1, 1, 2, 3, 4, 1, 1, 2, 3, 4, 5, 1, 1, 2, 3, 4, 5, 6, 1, 1, 2, 3, 4, 5, 6, 7, 1, 1, 2, 3, 4, 5, 6, 7, 8}
	assertSliceEqual(t, fmRes, expFmRes)
}

func TestMapRecursiveHeterogeneousTypes(t *testing.T) {
	type User struct {
		Name     string
		OrderIDs []int
	}

	users := []User{
		{"Alice", []int{1, 2, 3}},
		{"Bob", []int{4, 5}},
		{"Carol", []int{6}},
	}

	ctx := Ctx().
		WithMaxWorkers(3).
		WithAutoClose(false)
	defer ctx.Close()

	results, err := Map(ctx, users, func(u User, _ int) ([]string, error) {
		return Map(ctx, u.OrderIDs, func(id int, _ int) (string, error) {
			return fmt.Sprintf("%s:%d", u.Name, id), nil
		})
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}

	expected := [][]string{
		{"Alice:1", "Alice:2", "Alice:3"},
		{"Bob:4", "Bob:5"},
		{"Carol:6"},
	}
	if len(results) != len(expected) {
		t.Fatalf("len = %d, want %d", len(results), len(expected))
	}
	for i := range expected {
		assertSliceEqual(t, results[i], expected[i])
	}
}

// --- Context cancellation ---

func TestCancelContext(t *testing.T) {
	srcCtx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		time.Sleep(1000 * time.Millisecond)
		cancel()
	}()

	ctx := Ctx(srcCtx)
	items := makeRange(1000)

	_, err := Map(ctx, items, func(item int, _ int) (int, error) {
		time.Sleep(100 * time.Millisecond)
		return item, nil
	})
	if err == nil {
		t.Fatal("expected cancellation error")
	}
	if err.Error() != "context canceled" {
		t.Fatalf("error = %q, want %q", err.Error(), "context canceled")
	}
}

func TestDeadlineContext(t *testing.T) {
	srcCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	ctx := Ctx(srcCtx)

	_, err := Map(ctx, makeRange(1000), func(item int, _ int) (int, error) {
		time.Sleep(100 * time.Millisecond)
		return item, nil
	})
	if err == nil {
		t.Fatal("expected deadline error")
	}
}

// --- Index parameter ---

func TestMapIndexIsCorrect(t *testing.T) {
	items := []string{"a", "b", "c", "d", "e"}
	result, err := Map(Ctx().WithMaxWorkers(2), items, func(s string, i int) (string, error) {
		return fmt.Sprintf("%d:%s", i, s), nil
	})
	if err != nil {
		t.Fatalf("error: %v", err)
	}
	assertSliceEqual(t, result, []string{"0:a", "1:b", "2:c", "3:d", "4:e"})
}

// --- Context configuration panics ---

func TestWithMaxWorkersPanicsAfterUse(t *testing.T) {
	ctx := Ctx().WithMaxWorkers(2).WithAutoClose(false)
	defer ctx.Close()

	// Use the context to spawn workers
	_, _ = Map(ctx, []int{1, 2}, func(i int, _ int) (int, error) { return i, nil })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	ctx.WithMaxWorkers(4)
}

func TestWithAutoClosePanicsAfterUse(t *testing.T) {
	ctx := Ctx().WithAutoClose(false)
	defer ctx.Close()

	_, _ = Map(ctx, []int{1, 2}, func(i int, _ int) (int, error) { return i, nil })

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	ctx.WithAutoClose(true)
}

func TestWithMaxWorkersPanicsOnZero(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic")
		}
	}()
	Ctx().WithMaxWorkers(0)
}

// --- Reusing context with WithAutoClose(false) ---

func TestReuseContextAcrossCalls(t *testing.T) {
	ctx := Ctx().WithMaxWorkers(4).WithAutoClose(false)
	defer ctx.Close()

	a, err := Map(ctx, []int{1, 2, 3}, func(i int, _ int) (int, error) { return i * 10, nil })
	if err != nil {
		t.Fatalf("first Map error: %v", err)
	}
	assertSliceEqual(t, a, []int{10, 20, 30})

	b, err := Map(ctx, []int{4, 5, 6}, func(i int, _ int) (int, error) { return i * 10, nil })
	if err != nil {
		t.Fatalf("second Map error: %v", err)
	}
	assertSliceEqual(t, b, []int{40, 50, 60})
}

// --- Helpers ---

func recFn(ctx *Context, item int) ([]int, error) {
	if item == 0 {
		return []int{0}, nil
	}
	return Map(ctx, makeRange(item), func(t int, _ int) (int, error) {
		innerRes, err := recFn(ctx, t)
		if err != nil {
			return 0, err
		}
		return len(innerRes), nil
	})
}

func assertSliceEqual[T comparable](t *testing.T, got, want []T) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("index %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

func makeRange(n int) []int {
	result := make([]int, n)
	for i := range result {
		result[i] = i
	}
	return result
}
