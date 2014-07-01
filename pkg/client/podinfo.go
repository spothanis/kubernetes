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

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
)

// PodInfoGetter is an interface for things that can get information about a pod's containers.
// Injectable for easy testing.
type PodInfoGetter interface {
	// GetPodInfo returns information about all containers which are part
	// Returns an api.PodInfo, or an error if one occurs.
	GetPodInfo(host, podID string) (api.PodInfo, error)
}

// The default implementation, accesses the kubelet over HTTP
type HTTPPodInfoGetter struct {
	Client *http.Client
	Port   uint
}

func (c *HTTPPodInfoGetter) GetPodInfo(host, podID string) (api.PodInfo, error) {
	request, err := http.NewRequest(
		"GET",
		fmt.Sprintf(
			"http://%s/podInfo?podID=%s",
			net.JoinHostPort(host, strconv.FormatUint(uint64(c.Port), 10)),
			podID),
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
	info := api.PodInfo{}
	err = json.Unmarshal(body, &info)
	if err != nil {
		return nil, err
	}
	return info, nil
}

// Useful for testing.
type FakePodInfoGetter struct {
	data api.PodInfo
	err  error
}

func (c *FakePodInfoGetter) GetPodInfo(host, podID string) (api.PodInfo, error) {
	return c.data, c.err
}
