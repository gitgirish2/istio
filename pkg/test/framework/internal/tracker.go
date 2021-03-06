//  Copyright 2018 Istio Authors
//
//  Licensed under the Apache License, Version 2.0 (the "License");
//  you may not use this file except in compliance with the License.
//  You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.

package internal

import (
	"io"

	"go.uber.org/multierr"

	"istio.io/istio/pkg/test/framework/scopes"

	"fmt"

	"istio.io/istio/pkg/test/framework/component"
	"istio.io/istio/pkg/test/framework/components/registry"
	"istio.io/istio/pkg/test/framework/dependency"
	"istio.io/istio/pkg/test/framework/environment"
)

type componentInstance struct {
	id    dependency.Instance
	value interface{}
}

// Tracker keeps track of the state information for dependencies
type Tracker struct {
	// Map dependency ID to instance
	instanceMap map[dependency.Instance]interface{}
	// Also store the instances in the order they were initialized. This is use for ordered cleanup of the components.
	instances []componentInstance
	registry  *registry.Registry
}

func newTracker(registry *registry.Registry) *Tracker {
	return &Tracker{
		instanceMap: make(map[dependency.Instance]interface{}),
		registry:    registry,
	}
}

// Initialize a test dependency and start tracking it.
func (t *Tracker) Initialize(ctx environment.ComponentContext, c component.Component) (interface{}, error) {
	id := c.ID()
	if s, ok := t.instanceMap[id]; ok {
		// Already initialized.
		return s, nil
	}

	// Make sure all dependencies of the component are initialized first.
	depMap := make(map[dependency.Instance]interface{})
	for _, depID := range c.Requires() {
		depComp, ok := t.registry.Get(depID)
		if !ok {
			return nil, fmt.Errorf("unable to resolve dependency %s for component %s", depID, id)
		}

		// TODO(nmittler): We might want to protect against circular dependencies.
		s, err := t.Initialize(ctx, depComp)
		if err != nil {
			return nil, err
		}

		depMap[depID] = s
	}

	s, err := c.Init(ctx, depMap)
	if err != nil {
		return nil, err
	}

	t.instanceMap[id] = s
	t.instances = append(t.instances, componentInstance{
		id:    id,
		value: s,
	})

	return s, nil
}

// Get the tracked resource with the given ID.
func (t *Tracker) Get(id dependency.Instance) (interface{}, bool) {
	s, ok := t.instanceMap[id]
	return s, ok
}

// All returns all tracked resources.
func (t *Tracker) All() []interface{} {
	all := make([]interface{}, len(t.instances))
	for i, e := range t.instances {
		all[i] = e.value
	}
	return all
}

// Reset the all Resettable resources.
func (t *Tracker) Reset() error {
	var er error

	for _, e := range t.instances {
		if cl, ok := e.value.(Resettable); ok {
			scopes.Framework.Debugf("Resetting state for dependency: %s", e.id)
			if err := cl.Reset(); err != nil {
				scopes.Framework.Errorf("Error resetting dependency state: %s: %v", e.id, err)
				er = multierr.Append(er, err)
			}
		}
	}

	return er
}

// Cleanup closes all resources that implement io.Closer
func (t *Tracker) Cleanup() {
	for _, e := range t.instances {
		if cl, ok := e.value.(io.Closer); ok {
			scopes.Framework.Debugf("Cleaning up state for dependency: %s", e.id)
			if err := cl.Close(); err != nil {
				scopes.Framework.Errorf("Error cleaning up dependency state: %s: %v", e.id, err)
			}
		}
	}

	for k := range t.instanceMap {
		delete(t.instanceMap, k)
	}
	t.instances = make([]componentInstance, 0)
}
