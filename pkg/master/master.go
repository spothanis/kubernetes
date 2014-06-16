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

package master

import (
	"math/rand"
	"net/http"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/apiserver"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/registry"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/coreos/go-etcd/etcd"
)

// Master contains state for a Kubernetes cluster master/api server.
type Master struct {
	podRegistry        registry.PodRegistry
	controllerRegistry registry.ControllerRegistry
	serviceRegistry    registry.ServiceRegistry

	minions []string
	random  *rand.Rand
	storage map[string]apiserver.RESTStorage
}

// Returns a memory (not etcd) backed apiserver.
func NewMemoryServer(minions []string) *Master {
	m := &Master{
		podRegistry:        registry.MakeMemoryRegistry(),
		controllerRegistry: registry.MakeMemoryRegistry(),
		serviceRegistry:    registry.MakeMemoryRegistry(),
	}
	m.init(minions)
	return m
}

// Returns a new apiserver.
func New(etcdServers, minions []string) *Master {
	etcdClient := etcd.NewClient(etcdServers)
	m := &Master{
		podRegistry:        registry.MakeEtcdRegistry(etcdClient, minions),
		controllerRegistry: registry.MakeEtcdRegistry(etcdClient, minions),
		serviceRegistry:    registry.MakeEtcdRegistry(etcdClient, minions),
	}
	m.init(minions)
	return m
}

func (m *Master) init(minions []string) {
	containerInfo := &client.HTTPContainerInfo{
		Client: http.DefaultClient,
		Port:   10250,
	}

	m.minions = minions
	m.random = rand.New(rand.NewSource(int64(time.Now().Nanosecond())))
	m.storage = map[string]apiserver.RESTStorage{
		"pods": registry.MakePodRegistryStorage(m.podRegistry, containerInfo, registry.MakeFirstFitScheduler(m.minions, m.podRegistry, m.random)),
		"replicationControllers": registry.MakeControllerRegistryStorage(m.controllerRegistry),
		"services":               registry.MakeServiceRegistryStorage(m.serviceRegistry),
	}

}

// Runs master. Never returns.
func (m *Master) Run(myAddress, apiPrefix string) error {
	endpoints := registry.MakeEndpointController(m.serviceRegistry, m.podRegistry)
	go util.Forever(func() { endpoints.SyncServiceEndpoints() }, time.Second*10)

	s := &http.Server{
		Addr:           myAddress,
		Handler:        apiserver.New(m.storage, apiPrefix),
		ReadTimeout:    10 * time.Second,
		WriteTimeout:   10 * time.Second,
		MaxHeaderBytes: 1 << 20,
	}
	return s.ListenAndServe()
}
