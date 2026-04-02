# Party

Bounded parallel Map, Foreach, and FlatMap for Go slices — with ordered results, error
propagation, and context cancellation. No channels, no WaitGroups, no boilerplate.

Compare bounded parallel map with errgroup vs party:

```go
// errgroup
results := make([]R, len(items))
g, ctx := errgroup.WithContext(ctx)
g.SetLimit(10)
for i, item := range items {
    g.Go(func() error {
        r, err := process(item)
        if err != nil {
            return err
        }
        results[i] = r
        return nil
    })
}
if err := g.Wait(); err != nil {
    return nil, err
}

// party
results, err := party.Map(party.DefaultContext().WithMaxWorkers(10), items, process)
```

## Installation

```sh
go get github.com/GiGurra/party
```

## Usage

### Bounded Parallel Map

Process a slice with up to 10 workers, results returned in order:

```go
results, err := party.Map(
    party.DefaultContext().WithMaxWorkers(10),
    urls,
    func(url string, _ int) (Response, error) {
        return http.Get(url)
    },
)
```

### Foreach

Same as Map, but when you don't need to collect results:

```go
err := party.Foreach(
    party.DefaultContext().WithMaxWorkers(10),
    files,
    func(f File, _ int) error {
        return upload(f)
    },
)
```

### Async / Await

Fire off work and join later — no channels or WaitGroups:

```go
op := party.Async(func() (int, error) {
    return fetchCount()
})

// ... do other work ...

count, err := party.Await(op)
```

### Context Cancellation

Pass a cancellable context to stop early:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

results, err := party.Map(
    party.NewContext(ctx).WithMaxWorkers(10),
    items,
    process,
)
```

## API

### Context

- `NewContext(ctx context.Context) *Context`
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

<details>
<summary><strong>Advanced: Recursive Parallel Processing</strong></summary>

Party's worker pool is safe for recursive parallel calls. Normally, bounded worker pools
deadlock when a worker spawns child work that competes for the same pool. Party avoids this
by parking the parent worker and spawning a local worker per recursion level, continuing
depth-first without increasing total concurrency.

Set `WithAutoClose(false)` so the pool survives across nested calls, and `Close()` when done:

```go
func walkTree(ctx *party.Context, node Node) ([]Leaf, error) {
    if node.IsLeaf() {
        return []Leaf{node.Leaf()}, nil
    }
    return party.FlatMap(ctx, node.Children(), func(child Node, _ int) ([]Leaf, error) {
        return walkTree(ctx, child)
    })
}

ctx := party.DefaultContext().WithMaxWorkers(8).WithAutoClose(false)
defer ctx.Close()

leaves, err := walkTree(ctx, root)
```

Different types at each recursion level work correctly — the worker pool is type-agnostic:

```go
// Outer: []User, Inner: []OrderID — no issues
results, err := party.Map(ctx, users, func(u User, _ int) ([]Order, error) {
    return party.Map(ctx, u.OrderIDs, func(id int, _ int) (Order, error) {
        return fetchOrder(id)
    })
})
```

</details>

## License

This project is licensed under the MIT License. See the [LICENSE](LICENSE) file for details.

## Contributing

Contributions are welcome! Please open an issue or submit a pull request.

---

For more examples and detailed usage, refer to the [tests](party_test.go).
