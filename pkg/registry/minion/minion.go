/*
Copyright 2014 Google Inc. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package minion

import (
	"fmt"
	"sync"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
)

var ErrDoesNotExist = fmt.Errorf("The requested resource does not exist.")

// Registry keeps track of a set of minions. Safe for concurrent reading/writing.
type Registry interface {
	List() (currentMinions *api.MinionList, err error)
	Insert(minion string) error
	Delete(minion string) error
	Contains(minion string) (bool, error)
}

// NewRegistry initializes a minion registry with a list of minions.
func NewRegistry(minions []string, nodeResources api.NodeResources) Registry {
	m := &minionList{
		minions:       util.StringSet{},
		nodeResources: nodeResources,
	}
	for _, minion := range minions {
		m.minions.Insert(minion)
	}
	return m
}

type minionList struct {
	minions       util.StringSet
	lock          sync.Mutex
	nodeResources api.NodeResources
}

func (m *minionList) Contains(minion string) (bool, error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	return m.minions.Has(minion), nil
}

func (m *minionList) Delete(minion string) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.minions.Delete(minion)
	return nil
}

func (m *minionList) Insert(newMinion string) error {
	m.lock.Lock()
	defer m.lock.Unlock()
	m.minions.Insert(newMinion)
	return nil
}

func (m *minionList) List() (currentMinions *api.MinionList, err error) {
	m.lock.Lock()
	defer m.lock.Unlock()
	minions := []api.Minion{}
	for minion := range m.minions {
		minions = append(minions, api.Minion{
			TypeMeta:      api.TypeMeta{ID: minion},
			NodeResources: m.nodeResources,
		})
	}
	return &api.MinionList{
		Items: minions,
	}, nil
}
