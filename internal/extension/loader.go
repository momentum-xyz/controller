package extension

import (
	"github.com/momentum-xyz/controller/utils"
)

type LoaderFunc func(wc WorldController) Extension

type Loader interface {
	Get(name string) (LoaderFunc, bool)
	Set(name string, provider LoaderFunc)
}

var _ Loader = (*loader)(nil)

type loader struct {
	providers *utils.SyncMap[string, LoaderFunc]
}

func NewLoader() Loader {
	return &loader{
		providers: utils.NewSyncMap[string, LoaderFunc](),
	}
}

func (l *loader) Get(name string) (LoaderFunc, bool) {
	return l.providers.Load(name)
}

func (l *loader) Set(name string, provider LoaderFunc) {
	l.providers.Store(name, provider)
}
