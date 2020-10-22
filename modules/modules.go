// Copyright 2015 Matthew Holt and The Caddy Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package modules

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

//nolint:gochecknoglobals
var (
	modules   = make(map[string]Module)
	modulesMu sync.RWMutex
)

// Module is a type that is used as a k6 module. In
// addition to this interface, most modules will implement
// some interface expected by their host module in order
// to be useful. To learn which interface(s) to implement,
// see the documentation for the host module. At a bare
// minimum, this interface, when implemented, only provides
// the module's ID and constructor function.
//
// Modules will often implement additional interfaces
// including Provisioner, Validator, and CleanerUpper.
// If a module implements these interfaces, their
// methods are called during the module's lifespan.
//
// When a module is loaded by a host module, the following
// happens: 1) ModuleInfo.New() is called to get a new
// instance of the module. 2) The module's configuration is
// unmarshaled into that instance. 3) If the module is a
// Provisioner, the Provision() method is called. 4) If the
// module is a Validator, the Validate() method is called.
// 5) The module will probably be type-asserted from
// interface{} to some other, more useful interface expected
// by the host module. For example, HTTP handler modules are
// type-asserted as caddyhttp.MiddlewareHandler values.
// 6) When a module's containing Context is canceled, if it is
// a CleanerUpper, its Cleanup() method is called.
type Module interface {
	// This method indicates that the type is a k6
	// module. The returned ModuleInfo must have both
	// a name and a constructor function. This method
	// must not have any side-effects.
	K6Module() ModuleInfo
}

// ModuleInfo represents a registered k6 module.
type ModuleInfo struct {
	// ID is the "full name" of the module. It
	// must be unique and properly namespaced.
	ID ModuleID

	// New returns a pointer to a new, empty
	// instance of the module's type. This
	// method must not have any side-effects,
	// and no other initialization should
	// occur within it. Any initialization
	// of the returned value should be done
	// in a Provision() method (see the
	// Provisioner interface).
	New func() Module
}

// ModuleID is a string that uniquely identifies a k6 module. A
// module ID is lightly structured. It consists of dot-separated
// labels which form a simple hierarchy from left to right. The last
// label is the module name, and the labels before that constitute
// the namespace (or scope).
//
// Thus, a module ID has the form: <namespace>.<name>
//
// Module IDs should be lowercase and use underscores (_) instead of
// spaces.
//
// Examples of valid IDs:
// - js.sql
// - out.timescale
type ModuleID string

// Namespace returns the namespace (or scope) portion of a module ID,
// which is all but the last label of the ID. If the ID has only one
// label, then the namespace is empty.
func (id ModuleID) Namespace() string {
	lastDot := strings.LastIndex(string(id), ".")
	if lastDot < 0 {
		return ""
	}
	return string(id)[:lastDot]
}

// Name returns the Name (last element) of a module ID.
func (id ModuleID) Name() string {
	if id == "" {
		return ""
	}
	parts := strings.Split(string(id), ".")
	return parts[len(parts)-1]
}

func (mi ModuleInfo) String() string { return string(mi.ID) }

// RegisterModule registers a module by receiving a
// plain/empty value of the module. For registration to
// be properly recorded, this should be called in the
// init phase of runtime. Typically, the module package
// will do this as a side-effect of being imported.
// This function panics if the module's info is
// incomplete or invalid, or if the module is already
// registered.
func RegisterModule(instance Module) {
	mod := instance.K6Module()

	if mod.ID == "" {
		panic("module ID missing")
	}
	if mod.ID == "k6" || mod.ID == "admin" {
		panic(fmt.Sprintf("module ID '%s' is reserved", mod.ID))
	}
	if mod.New == nil {
		panic("missing ModuleInfo.New")
	}
	val := mod.New()
	if val == nil {
		panic("ModuleInfo.New must return a non-nil module instance")
	}

	modulesMu.Lock()
	defer modulesMu.Unlock()

	if _, ok := modules[string(mod.ID)]; ok {
		panic(fmt.Sprintf("module already registered: %s", mod.ID))
	}
	modules[string(mod.ID)] = val
}

// GetModule returns the module given its ID (full name).
func GetModule(name string) (Module, error) {
	modulesMu.RLock()
	defer modulesMu.RUnlock()
	m, ok := modules[name]
	if !ok {
		return nil, fmt.Errorf("module not registered: %s", name)
	}
	return m, nil
}

// GetModuleName returns a module's name (the last label of its ID)
// from an instance of its value. If the value is not a module, an
// empty string will be returned.
func GetModuleName(instance interface{}) string {
	var name string
	if mod, ok := instance.(Module); ok {
		name = mod.K6Module().ID.Name()
	}
	return name
}

// GetModuleID returns a module's ID from an instance of its value.
// If the value is not a module, an empty string will be returned.
func GetModuleID(instance interface{}) string {
	var id string
	if mod, ok := instance.(Module); ok {
		id = string(mod.K6Module().ID)
	}
	return id
}

// GetModules returns all modules in the given scope/namespace.
// For example, a scope of "foo" returns modules named "foo.bar",
// "foo.loo", but not "bar", "foo.bar.loo", etc. An empty scope
// returns top-level modules, for example "foo" or "bar". Partial
// scopes are not matched (i.e. scope "foo.ba" does not match
// name "foo.bar").
//
// Because modules are registered to a map under the hood, the
// returned slice will be sorted to keep it deterministic.
func GetModules(scope string) []Module {
	modulesMu.RLock()
	defer modulesMu.RUnlock()

	scopeParts := strings.Split(scope, ".")

	// handle the special case of an empty scope, which
	// should match only the top-level modules
	if scope == "" {
		scopeParts = []string{}
	}

	var mods []Module
iterateModules:
	for id, m := range modules {
		modParts := strings.Split(id, ".")

		// match only the next level of nesting
		if len(modParts) != len(scopeParts)+1 {
			continue
		}

		// specified parts must be exact matches
		for i := range scopeParts {
			if modParts[i] != scopeParts[i] {
				continue iterateModules
			}
		}

		mods = append(mods, m)
	}

	// make return value deterministic
	sort.Slice(mods, func(i, j int) bool {
		return mods[i].K6Module().ID < mods[j].K6Module().ID
	})

	return mods
}

// Modules returns the names of all registered modules
// in ascending lexicographical order.
func Modules() []string {
	modulesMu.RLock()
	defer modulesMu.RUnlock()

	names := make([]string, 0, len(modules))
	for name := range modules {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// Provisioner is implemented by modules which may need to perform
// some additional "setup" steps immediately after being loaded.
// Provisioning should be fast (imperceptible running time). If
// any side-effects result in the execution of this function (e.g.
// creating global state, any other allocations which require
// garbage collection, opening files, starting goroutines etc.),
// be sure to clean up properly by implementing the CleanerUpper
// interface to avoid leaking resources.
type Provisioner interface {
	// Provision(Context) error
	Provision() error
}

// Validator is implemented by modules which can verify that their
// configurations are valid. This method will be called after
// Provision() (if implemented). Validation should always be fast
// (imperceptible running time) and an error must be returned if
// the module's configuration is invalid.
type Validator interface {
	Validate() error
}

// CleanerUpper is implemented by modules which may have side-effects
// such as opened files, spawned goroutines, or allocated some sort
// of non-stack state when they were provisioned. This method should
// deallocate/cleanup those resources to prevent memory leaks. Cleanup
// should be fast and efficient. Cleanup should work even if Provision
// returns an error, to allow cleaning up from partial provisionings.
type CleanerUpper interface {
	Cleanup() error
}
