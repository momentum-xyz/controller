package utils

import (
	"github.com/sasha-s/go-deadlock"
)

type SyncMap[K comparable, V any] struct {
	Mu   *deadlock.Mutex
	Data map[K]V
}

func NewSyncMap[K comparable, V any]() *SyncMap[K, V] {
	return &SyncMap[K, V]{
		Mu:   &deadlock.Mutex{},
		Data: make(map[K]V),
	}
}

func (m *SyncMap[K, V]) Store(k K, v V) {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	m.Data[k] = v
}

func (m *SyncMap[K, V]) Load(k K) (V, bool) {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	v, ok := m.Data[k]
	return v, ok
}

func (m *SyncMap[K, V]) Remove(k K) {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	delete(m.Data, k)
}

func (m *SyncMap[K, V]) Purge() {
	m.Mu.Lock()
	defer m.Mu.Unlock()

	m.Data = make(map[K]V)
}
