package xsync

import "sync"

type result[T any] struct {
	value T
	err   error
}

// Future represents a value that will be available asynchronously.
type Future[T any] struct {
	done chan struct{}
	once sync.Once
	res  result[T]
}

// NewFuture creates a Future and a resolve function. Call resolve exactly once to set the result.
func NewFuture[T any]() (*Future[T], func(T, error)) {
	f := &Future[T]{done: make(chan struct{})}
	resolve := func(v T, err error) {
		f.once.Do(func() {
			f.res = result[T]{value: v, err: err}
			close(f.done)
		})
	}
	return f, resolve
}

// Await blocks until the future is resolved and returns the value or error.
func (f *Future[T]) Await() (T, error) {
	<-f.done
	return f.res.value, f.res.err
}

// Result represents a value-or-error container.
type Result[T any] struct {
	value T
	err   error
}

// Ok creates a successful Result.
func Ok[T any](v T) Result[T] {
	return Result[T]{value: v}
}

// Err creates a failed Result.
func Err[T any](err error) Result[T] {
	return Result[T]{err: err}
}

// Unwrap returns the contained value and error.
func (r Result[T]) Unwrap() (T, error) {
	return r.value, r.err
}

// IsOk returns true if the result contains a value (no error).
func (r Result[T]) IsOk() bool {
	return r.err == nil
}

// IsErr returns true if the result contains an error.
func (r Result[T]) IsErr() bool {
	return r.err != nil
}

// Map applies fn to the value if the result is Ok, returning a new Result.
func (r Result[T]) Map(fn func(T) T) Result[T] {
	if r.err != nil {
		return r
	}
	return Ok(fn(r.value))
}
