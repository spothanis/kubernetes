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

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/version"
)

// Interface holds the methods for clients of Kubernetes,
// an interface to allow mock testing.
type Interface interface {
	PodsNamespacer
	ReplicationControllersNamespacer
	ServicesNamespacer
	VersionInterface
	MinionsInterface
	EventsInterface
}

func (c *Client) ReplicationControllers(namespace string) ReplicationControllerInterface {
	return NewReplicationControllersClient(c, namespace)
}

func (c *Client) Minions() MinionInterface {
	return NewMinionsClient(c)
}

func (c *Client) Events() EventInterface {
	return NewEventsClient(c)
}

func (c *Client) Endpoints(namespace string) EndpointsInterface {
	return NewEndpointsClient(c, namespace)
}

func (c *Client) Pods(namespace string) PodInterface {
	return NewPodsClient(c, namespace)
}

func (c *Client) Services(namespace string) ServiceInterface {
	return NewServicesClient(c, namespace)
}

// VersionInterface has a method to retrieve the server version.
type VersionInterface interface {
	ServerVersion() (*version.Info, error)
}

// APIStatus is exposed by errors that can be converted to an api.Status object
// for finer grained details.
type APIStatus interface {
	Status() api.Status
}

// Client is the implementation of a Kubernetes client.
type Client struct {
	*RESTClient
}

// ServerVersion retrieves and parses the server's version.
func (c *Client) ServerVersion() (*version.Info, error) {
	body, err := c.Get().AbsPath("/version").Do().Raw()
	if err != nil {
		return nil, err
	}
	var info version.Info
	err = json.Unmarshal(body, &info)
	if err != nil {
		return nil, fmt.Errorf("Got '%s': %v", string(body), err)
	}
	return &info, nil
}
