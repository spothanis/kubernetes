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

package registry

import (
	"fmt"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/apiserver"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
)

// MinionRegistryStorage implements the RESTStorage interface, backed by a MinionRegistry.
type MinionRegistryStorage struct {
	registry MinionRegistry
}

func MakeMinionRegistryStorage(m MinionRegistry) apiserver.RESTStorage {
	return &MinionRegistryStorage{
		registry: m,
	}
}

func (storage *MinionRegistryStorage) toApiMinion(name string) api.Minion {
	return api.Minion{JSONBase: api.JSONBase{ID: name}}
}

func (storage *MinionRegistryStorage) List(selector labels.Selector) (interface{}, error) {
	nameList, err := storage.registry.List()
	if err != nil {
		return nil, err
	}
	var list api.MinionList
	for _, name := range nameList {
		list.Items = append(list.Items, storage.toApiMinion(name))
	}
	return list, nil
}

func (storage *MinionRegistryStorage) Get(id string) (interface{}, error) {
	exists, err := storage.registry.Contains(id)
	if !exists {
		return nil, ErrDoesNotExist
	}
	return storage.toApiMinion(id), err
}

func (storage *MinionRegistryStorage) Extract(body []byte) (interface{}, error) {
	var minion api.Minion
	err := api.DecodeInto(body, &minion)
	return minion, err
}

func (storage *MinionRegistryStorage) Create(obj interface{}) (<-chan interface{}, error) {
	minion, ok := obj.(api.Minion)
	if !ok {
		return nil, fmt.Errorf("not a minion: %#v", obj)
	}
	if minion.ID == "" {
		return nil, fmt.Errorf("ID should not be empty: %#v", minion)
	}
	return apiserver.MakeAsync(func() (interface{}, error) {
		err := storage.registry.Insert(minion.ID)
		if err != nil {
			return nil, err
		}
		contains, err := storage.registry.Contains(minion.ID)
		if err != nil {
			return nil, err
		}
		if contains {
			return storage.toApiMinion(minion.ID), nil
		}
		return nil, fmt.Errorf("unable to add minion %#v", minion)
	}), nil
}

func (storage *MinionRegistryStorage) Update(minion interface{}) (<-chan interface{}, error) {
	return nil, fmt.Errorf("Minions can only be created (inserted) and deleted.")
}

func (storage *MinionRegistryStorage) Delete(id string) (<-chan interface{}, error) {
	exists, err := storage.registry.Contains(id)
	if !exists {
		return nil, ErrDoesNotExist
	}
	if err != nil {
		return nil, err
	}
	return apiserver.MakeAsync(func() (interface{}, error) {
		return api.Status{Status: api.StatusSuccess}, storage.registry.Delete(id)
	}), nil
}
