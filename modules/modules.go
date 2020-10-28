package modules

import (
	"fmt"
	"strings"
	"sync"
)

//nolint:gochecknoglobals
var (
	modules = make(map[string]interface{})
	mx      sync.RWMutex
)

const extPrefix string = "k6/x/"

// GetModule returns the module registered with name.
func GetModule(name string) interface{} {
	mx.RLock()
	defer mx.RUnlock()
	return modules[name]
}

// RegisterModule registers the given mod as a JavaScript module, available
// for import from JS scripts with the "k6/x/<name>" import path.
// This function panics if a module with the same name is already registered.
func RegisterModule(name string, mod interface{}) {
	if !strings.HasPrefix(name, extPrefix) {
		name = fmt.Sprintf("%s%s", extPrefix, name)
	}

	mx.Lock()
	defer mx.Unlock()

	if _, ok := modules[name]; ok {
		panic(fmt.Sprintf("module already registered: %s", name))
	}
	modules[name] = mod
}
