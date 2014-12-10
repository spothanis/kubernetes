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

import "github.com/GoogleCloudPlatform/kubernetes/pkg/api"

type NodesInterface interface {
	Nodes() NodeInterface
}

type NodeInterface interface {
	Get(id string) (result *api.Node, err error)
	Create(minion *api.Node) (*api.Node, error)
	List() (*api.NodeList, error)
	Delete(id string) error
}

// nodes implements NodesInterface
type nodes struct {
	r          *Client
	preV1Beta3 bool
}

// newNodes returns a nodes object. Uses "minions" as the
// URL resource name for v1beta1 and v1beta2.
func newNodes(c *Client, isPreV1Beta3 bool) *nodes {
	return &nodes{c, isPreV1Beta3}
}

func (c *nodes) resourceName() string {
	if c.preV1Beta3 {
		return "minions"
	}
	return "nodes"
}

// Create creates a new minion.
func (c *nodes) Create(minion *api.Node) (*api.Node, error) {
	result := &api.Node{}
	err := c.r.Post().Path(c.resourceName()).Body(minion).Do().Into(result)
	return result, err
}

// List lists all the nodes in the cluster.
func (c *nodes) List() (*api.NodeList, error) {
	result := &api.NodeList{}
	err := c.r.Get().Path(c.resourceName()).Do().Into(result)
	return result, err
}

// Get gets an existing minion
func (c *nodes) Get(id string) (*api.Node, error) {
	result := &api.Node{}
	err := c.r.Get().Path(c.resourceName()).Path(id).Do().Into(result)
	return result, err
}

// Delete deletes an existing minion.
func (c *nodes) Delete(id string) error {
	return c.r.Delete().Path(c.resourceName()).Path(id).Do().Error()
}
