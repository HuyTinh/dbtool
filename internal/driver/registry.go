package driver

import (
	"fmt"
	"sync"
)

type Registry interface {
	Register(d Driver)
	Get(name string) (Driver, error)
	All() []Driver
}

type defaultRegistry struct {
	mu    sync.RWMutex
	items map[string]Driver
}

func (r *defaultRegistry) Register(d Driver) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[d.Name()] = d
}

func (r *defaultRegistry) Get(name string) (Driver, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.items[name]
	if !ok {
		return nil, fmt.Errorf("driver %q not found", name)
	}
	return d, nil
}

func (r *defaultRegistry) All() []Driver {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Driver, 0, len(r.items))
	for _, d := range r.items {
		list = append(list, d)
	}
	return list
}

var defaultReg Registry = &defaultRegistry{items: make(map[string]Driver)}

func Register(d Driver) {
	defaultReg.Register(d)
}

func Get(name string) (Driver, error) {
	return defaultReg.Get(name)
}

func All() []Driver {
	return defaultReg.All()
}

func SetDefaultRegistry(r Registry) {
	defaultReg = r
}
