# Party

Party is a Go library for parallel processing with context management, error handling, and result ordering. It supports
bounded parallelization in recursive contexts, allowing for efficient tree traversal and parallel execution without
exploding the worker pool or running into deadlocks.

## Installation

```sh
go get github.com/GiGurra/party
```

## Usage

### Basic Example

```go
package main

import (
	"fmt"
	"github.com/GiGurra/party"
)

func main() {
	ctx := party.DefaultContext()
	data := []int{1, 2, 3, 4, 5}

	results, err := party.Map(ctx, data, func(item int, _ int) (int, error) {
		return item * 2, nil
	})

	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Results:", results)
	}
}
```

### Asynchronous Operations

```go
package main

func main() {
	asyncOp := party.Async(func() (int, error) {
		return 42, nil
	})

	result, err := party.Await(asyncOp)
	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Result:", result)
	}
}

```

### Recursive Parallel Processing

```go
package main

func recFn(ctx *party.Context, item int) ([]int, error) {
	if item == 0 {
		return []int{0}, nil
	} else {
		innerRange := makeRange(item)
		return party.Map(ctx, innerRange, func(t int, _ int) (int, error) {
			innerRes, err := recFn(ctx, t)
			if err != nil {
				return 0, err
			} else {
				return len(innerRes), nil
			}
		})
	}
}

func main() {
	ctx := party.DefaultContext().WithMaxWorkers(3).WithAutoClose(false)
	defer ctx.Close()

	items := makeRange(10)
	res, err := party.Map(ctx, items, func(item int, _ int) ([]int, error) {
		return recFn(ctx, item)
	})

	if err != nil {
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Results:", res)
	}
}

```

## API

### Context

- `NewContext(backing context.Context) *Context`
- `DefaultContext() *Context`
- `(*Context) WithAutoClose(autoClose bool) *Context`
- `(*Context) WithMaxWorkers(maxWorkers int) *Context`
- `(*Context) WithContext(ctx context.Context) *Context`
- `(*Context) Close()`

### Parallel Processing

- `Foreach(ctx *Context, data []T, processor func(t T, index int) error) error`
- `Map(ctx *Context, data []T, processor func(t T, index int) (R, error)) ([]R, error)`
- `FlatMap(ctx *Context, data []T, processor func(t T, index int) ([]R, error)) ([]R, error)`

### Asynchronous Operations

- `Async(f func() (T, error)) AsyncOp[T]`
- `Await(ch AsyncOp[T]) (T, error)`

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

---

For more examples and detailed usage, refer to the [tests](party_test.go).