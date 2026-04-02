package party

import (
	"context"
	"fmt"
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

	if t1.Sub(t0) > 100*time.Millisecond {
		t.Fatalf("Async() should return immediately")
	}

	if t2.Sub(t0) < 450*time.Millisecond {
		t.Fatalf("Await() should block until result is ready")
	}

	// errors propagate through
	asyncOp = Async(func() (int, error) {
		return 0, fmt.Errorf("test error")
	})

	_, err = Await(asyncOp)
	if err == nil {
		t.Fatalf("Await() expected error")
	}
	if err.Error() != "test error" {
		t.Fatalf("Await() error = %q, want %q", err.Error(), "test error")
	}
}

func TestMap(t *testing.T) {
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
		t.Fatalf("Map() error: %v", err)
	}
	assertSliceEqual(t, result, expected)

	// Serial (maxWorkers=1) must produce the same result
	serial, err := Map(Ctx().WithMaxWorkers(1), items, func(item int, _ int) (int, error) {
		return item * 2, nil
	})
	if err != nil {
		t.Fatalf("Map(serial) error: %v", err)
	}
	assertSliceEqual(t, serial, expected)
}

func TestMapError(t *testing.T) {
	items := makeRange(1000)

	_, err := Map(Ctx().WithMaxWorkers(100), items, func(item int, _ int) (int, error) {
		if item > 150 {
			return 0, fmt.Errorf("too large")
		}
		return item, nil
	})
	if err == nil {
		t.Fatalf("Map() expected error")
	}
	if err.Error() != "too large" {
		t.Fatalf("Map() error = %q, want %q", err.Error(), "too large")
	}
}

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

// TestMapRecursiveHeterogeneousTypes verifies that recursive parallel calls
// work correctly when different levels use different types. This was broken
// in the original chan-any implementation where global workers would panic
// on type assertion mismatches.
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

	// Outer Map processes Users, inner Map processes ints — different types at each level
	results, err := Map(ctx, users, func(u User, _ int) ([]string, error) {
		return Map(ctx, u.OrderIDs, func(id int, _ int) (string, error) {
			return fmt.Sprintf("%s:%d", u.Name, id), nil
		})
	})
	if err != nil {
		t.Fatalf("Map(heterogeneous) error: %v", err)
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
		t.Fatalf("Map() expected cancellation error")
	}
	if err.Error() != "context canceled" {
		t.Fatalf("Map() error = %q, want %q", err.Error(), "context canceled")
	}
}

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
