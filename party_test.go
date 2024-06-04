package party

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"time"
)

func TestMapPar(t *testing.T) {
	items := makeRange(1000)

	refResult := make([]int, 1000)
	for i := range refResult {
		refResult[i] = i * 2
	}

	result, err := MapPar(10, items, func(item int) (int, error) {
		randSleep := time.Duration(rand.Int64N(10))
		time.Sleep(randSleep * time.Millisecond)
		return item * 2, nil
	})

	if err != nil {
		t.Fatalf("ParallelProcessRet() error: %v", err)
	}

	serialResult, err := MapPar(1, items, func(item int) (int, error) {
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

	_, err = MapPar(100, items, func(item int) (int, error) {
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
