package importer

import (
	"fmt"
	"sync"
)

type ImportedProfile struct {
	SuggestedName string // e.g. "local" or "dev"
	Driver        string // "postgres", "mysql", etc.
	Host          string
	Port          int
	Database      string
	Username      string
	Password      string
	PasswordIsRef bool
	Source        string // description of source file/key, e.g. "application.yml -> spring.datasource.url"
}

type Importer interface {
	Name() string
	Detect(dir string) ([]string, error)
	Parse(filePath string) ([]ImportedProfile, error)
}

type Registry interface {
	Register(imp Importer)
	Get(name string) (Importer, error)
	All() []Importer
}

type defaultRegistry struct {
	mu    sync.RWMutex
	items map[string]Importer
}

func (r *defaultRegistry) Register(imp Importer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[imp.Name()] = imp
}

func (r *defaultRegistry) Get(name string) (Importer, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	imp, ok := r.items[name]
	if !ok {
		return nil, fmt.Errorf("importer %q not found", name)
	}
	return imp, nil
}

func (r *defaultRegistry) All() []Importer {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]Importer, 0, len(r.items))
	for _, imp := range r.items {
		list = append(list, imp)
	}
	return list
}

var defaultReg Registry = &defaultRegistry{items: make(map[string]Importer)}

func Register(imp Importer) {
	defaultReg.Register(imp)
}

func Get(name string) (Importer, error) {
	return defaultReg.Get(name)
}

func All() []Importer {
	return defaultReg.All()
}
