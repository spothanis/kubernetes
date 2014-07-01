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

package client

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"strconv"

	"github.com/fsouza/go-dockerclient"
)

// ContainerInfo is an interface for things that can get information about a container.
// Injectable for easy testing.
type ContainerInfo interface {
	// GetContainerInfo returns information about container 'name' on 'host'
	// Returns a json-formatted []byte (which can be unmarshalled into a
	// map[string]interface{}) or an error if one occurs.
	GetContainerInfo(host, name string) (*docker.Container, error)
}

// The default implementation, accesses the kubelet over HTTP
type HTTPContainerInfo struct {
	Client *http.Client
	Port   uint
}

func (c *HTTPContainerInfo) GetContainerInfo(host, name string) (*docker.Container, error) {
	request, err := http.NewRequest(
		"GET",
		fmt.Sprintf(
			"http://%s/containerInfo?container=%s",
			net.JoinHostPort(host, strconv.FormatUint(uint64(c.Port), 10)),
			name),
		nil)
	if err != nil {
		return nil, err
	}
	response, err := c.Client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	// Check that this data can be unmarshalled
	var container docker.Container
	err = json.Unmarshal(body, &container)
	if err != nil {
		return nil, err
	}
	return &container, nil
}

// Useful for testing.
type FakeContainerInfo struct {
	data *docker.Container
	err  error
}

func (c *FakeContainerInfo) GetContainerInfo(host, name string) (*docker.Container, error) {
	return c.data, c.err
}
