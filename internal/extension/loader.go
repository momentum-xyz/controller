package extension

import (
	"sync"
)

type Loader interface {
	Get(name string) func(wc WorldController) Extension
	Set(name string, provider func(wc WorldController) Extension)
	IsInitialized() bool
	SetInitialized()
}

func NewLoader() Loader {
	return &loader{
		providers: make(map[string]func(wc WorldController) Extension),
	}
}

type loader struct {
	initialized bool
	mu          sync.Mutex
	providers   map[string]func(wc WorldController) Extension
}

func (l *loader) SetInitialized() {
	l.mu.Lock()
	l.initialized = true
	l.mu.Unlock()
}

func (l *loader) IsInitialized() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.initialized
}

func (l *loader) Get(name string) func(wc WorldController) Extension {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.providers[name]
}

func (l *loader) Set(name string, provider func(wc WorldController) Extension) {
	l.mu.Lock()
	if !l.initialized {
		l.providers = make(map[string]func(wc WorldController) Extension)
	}
	l.providers[name] = provider
	l.mu.Unlock()
}
