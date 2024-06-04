# 🎉 Party

Welcome to Party, the Go library that brings the fun to asynchronous and parallel processing!

Similar to [github.com/samber/lo](https://github.com/samber/lo), but with support for errors and customizable parallelization
factors.

## Installation

```
go get github.com/GiGurra/party
```

## Usage

### 🎈 Async/Await

Run functions asynchronously and wait for their results. Built with one channel per operation, so optimized for small
datasets with heavy individual computations.

```go
asyncOp := party.Async(func () (int, error) {
    // Your async code here
    return 42, nil
})

result, err := party.Await(asyncOp)
if err != nil {
    // Handle error
}
fmt.Println(result) // Output: 42
```

### 🎉 Parallel Processing

#### ForeachPar

Process elements of a collection in parallel. Build with waitgroups, especially useful for larger data sets.

```go
package main

import (
	"fmt"
	"github.com/GiGurra/party"
)

func main() {
	data := []int{1, 2, 3, 4, 5}
	party.ForeachPar(3, data, func(t int) {
		fmt.Println(t)
	})
}

```

#### MapPar

Apply a function to each element of a collection in parallel and collect the results. It's like a conga line for your
data!

```go
package main

import (
	"fmt"
	"github.com/GiGurra/party"
)

func main() {
	data := []int{1, 2, 3, 4, 5}
	results, err := party.MapPar(3, data, func(t int) (int, error) {
		return t * 2, nil
	})
	if err != nil {
		// Handle error
	}
	fmt.Println(results) // Output: [2, 4, 6, 8, 10]
}

```

#### FlatMapPar

Apply a function that returns a slice to each element of a collection in parallel and flatten the results. Because
sometimes, you just need to spread the fun around!

```go
package main

import (
	"fmt"
	"github.com/GiGurra/party"
)

func main() {
	data := []int{1, 2, 3}
	results, err := party.FlatMapPar(3, data, func(t int) ([]int, error) {
		return []int{t, t * 2}, nil
	})
	if err != nil {
		// Handle error
	}
	fmt.Println(results) // Output: [1, 2, 2, 4, 3, 6]
}

```

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.
