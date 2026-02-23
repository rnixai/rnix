package xsync

import (
	"errors"
	"sync"
	"testing"
)

func TestFuture_AwaitSuccess(t *testing.T) {
	f, resolve := NewFuture[int]()
	go resolve(42, nil)
	v, err := f.Await()
	if err != nil || v != 42 {
		t.Fatalf("got (%d, %v), want (42, nil)", v, err)
	}
}

func TestFuture_AwaitError(t *testing.T) {
	f, resolve := NewFuture[string]()
	go resolve("", errors.New("fail"))
	_, err := f.Await()
	if err == nil || err.Error() != "fail" {
		t.Fatalf("expected error 'fail', got %v", err)
	}
}

func TestFuture_MultipleAwait(t *testing.T) {
	f, resolve := NewFuture[int]()
	resolve(7, nil)
	v1, _ := f.Await()
	v2, _ := f.Await()
	if v1 != 7 || v2 != 7 {
		t.Fatalf("got (%d, %d), want (7, 7)", v1, v2)
	}
}

func TestFuture_ConcurrentAwait(t *testing.T) {
	f, resolve := NewFuture[int]()
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i == 25 {
				resolve(99, nil)
			}
			v, err := f.Await()
			if err != nil || v != 99 {
				t.Errorf("goroutine %d: got (%d, %v)", i, v, err)
			}
		}(i)
	}
	wg.Wait()
}

func TestFuture_DoubleResolve(t *testing.T) {
	f, resolve := NewFuture[int]()
	resolve(1, nil)
	resolve(2, nil) // should be no-op
	v, _ := f.Await()
	if v != 1 {
		t.Fatalf("got %d, want 1 (first resolve wins)", v)
	}
}

func TestResult_Ok(t *testing.T) {
	r := Ok(42)
	v, err := r.Unwrap()
	if err != nil || v != 42 {
		t.Fatalf("got (%d, %v), want (42, nil)", v, err)
	}
	if !r.IsOk() {
		t.Fatal("expected IsOk=true")
	}
	if r.IsErr() {
		t.Fatal("expected IsErr=false")
	}
}

func TestResult_Err(t *testing.T) {
	r := Err[int](errors.New("bad"))
	_, err := r.Unwrap()
	if err == nil {
		t.Fatal("expected error")
	}
	if r.IsOk() {
		t.Fatal("expected IsOk=false")
	}
	if !r.IsErr() {
		t.Fatal("expected IsErr=true")
	}
}

func TestResult_Map_Ok(t *testing.T) {
	r := Ok(10).Map(func(v int) int { return v * 2 })
	v, err := r.Unwrap()
	if err != nil || v != 20 {
		t.Fatalf("got (%d, %v), want (20, nil)", v, err)
	}
}

func TestResult_Map_Err(t *testing.T) {
	r := Err[int](errors.New("fail")).Map(func(v int) int { return v * 2 })
	if r.IsOk() {
		t.Fatal("Map on Err should propagate error")
	}
}
