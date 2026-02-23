package xsync

import (
	"sync"
	"testing"
)

func TestSyncMap_StoreAndLoad(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Store("a", 1)
	v, ok := m.Load("a")
	if !ok || v != 1 {
		t.Fatalf("got (%d, %v), want (1, true)", v, ok)
	}
}

func TestSyncMap_LoadMissing(t *testing.T) {
	m := NewSyncMap[string, int]()
	_, ok := m.Load("missing")
	if ok {
		t.Fatal("expected ok=false for missing key")
	}
}

func TestSyncMap_Delete(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Store("a", 1)
	m.Delete("a")
	_, ok := m.Load("a")
	if ok {
		t.Fatal("expected ok=false after delete")
	}
}

func TestSyncMap_Range(t *testing.T) {
	m := NewSyncMap[string, int]()
	m.Store("a", 1)
	m.Store("b", 2)
	m.Store("c", 3)
	count := 0
	m.Range(func(k string, v int) bool {
		count++
		return true
	})
	if count != 3 {
		t.Fatalf("range visited %d items, want 3", count)
	}
}

func TestSyncMap_RangeEarlyStop(t *testing.T) {
	m := NewSyncMap[int, int]()
	for i := 0; i < 10; i++ {
		m.Store(i, i)
	}
	count := 0
	m.Range(func(k, v int) bool {
		count++
		return count < 3
	})
	if count != 3 {
		t.Fatalf("range visited %d items, want 3", count)
	}
}

func TestSyncMap_Len(t *testing.T) {
	m := NewSyncMap[string, int]()
	if m.Len() != 0 {
		t.Fatalf("expected len 0, got %d", m.Len())
	}
	m.Store("a", 1)
	m.Store("b", 2)
	if m.Len() != 2 {
		t.Fatalf("expected len 2, got %d", m.Len())
	}
}

func TestSyncMap_ConcurrentAccess(t *testing.T) {
	m := NewSyncMap[int, int]()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m.Store(i, i*10)
			m.Load(i)
			m.Range(func(k, v int) bool { return true })
			if i%2 == 0 {
				m.Delete(i)
			}
		}(i)
	}
	wg.Wait()
	t.Logf("SyncMap concurrent test: %d items remaining", m.Len())
}
