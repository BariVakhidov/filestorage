package safemap

import "sync"

type SafeMap[K comparable, V any] struct {
	mu   sync.RWMutex
	data map[K]V
}

func New[K comparable, V any]() *SafeMap[K, V] {
	return &SafeMap[K, V]{
		mu:   sync.RWMutex{},
		data: make(map[K]V),
	}
}

func (m *SafeMap[K, V]) Get(k K) (any, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	v, ok := m.data[k]
	return v, ok
}

func (m *SafeMap[K, V]) Set(k K, v V) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[k] = v
}

func (m *SafeMap[K, V]) Delete(k K) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, k)
}

func (m *SafeMap[K, V]) Len() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	l := len(m.data)
	return l
}

func (m *SafeMap[K, V]) ForEach(fn func(K, V)) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for k, v := range m.data {
		fn(k, v)
	}
}
