/*
Copyright 2015 Google Inc. All rights reserved.

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

package servicecontroller

import (
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client/testclient"
	fake_cloud "github.com/GoogleCloudPlatform/kubernetes/pkg/cloudprovider/fake"
)

const region = "us-central"

func TestCreateExternalLoadBalancer(t *testing.T) {
	table := []struct {
		service             *api.Service
		expectErr           bool
		expectCreateAttempt bool
	}{
		{
			service: &api.Service{
				ObjectMeta: api.ObjectMeta{
					Name:      "no-external-balancer",
					Namespace: "default",
				},
				Spec: api.ServiceSpec{
					CreateExternalLoadBalancer: false,
				},
			},
			expectErr:           false,
			expectCreateAttempt: false,
		},
		{
			service: &api.Service{
				ObjectMeta: api.ObjectMeta{
					Name:      "udp-service",
					Namespace: "default",
				},
				Spec: api.ServiceSpec{
					Ports: []api.ServicePort{{
						Port:     80,
						Protocol: api.ProtocolUDP,
					}},
					CreateExternalLoadBalancer: true,
				},
			},
			expectErr:           true,
			expectCreateAttempt: false,
		},
		{
			service: &api.Service{
				ObjectMeta: api.ObjectMeta{
					Name:      "basic-service1",
					Namespace: "default",
				},
				Spec: api.ServiceSpec{
					Ports: []api.ServicePort{{
						Port:     80,
						Protocol: api.ProtocolTCP,
					}},
					CreateExternalLoadBalancer: true,
				},
			},
			expectErr:           false,
			expectCreateAttempt: true,
		},
	}

	for _, item := range table {
		cloud := &fake_cloud.FakeCloud{}
		cloud.Region = region
		client := &testclient.Fake{}
		controller := New(cloud, client, "test-cluster")
		controller.init()
		cloud.Calls = nil    // ignore any cloud calls made in init()
		client.Actions = nil // ignore any client calls made in init()
		err, _ := controller.createLoadBalancerIfNeeded(item.service)
		if !item.expectErr && err != nil {
			t.Errorf("unexpected error: %v", err)
		} else if item.expectErr && err == nil {
			t.Errorf("expected error creating %v, got nil", item.service)
		}
		if !item.expectCreateAttempt {
			if len(cloud.Calls) > 0 {
				t.Errorf("unexpected cloud provider calls: %v", cloud.Calls)
			}
			if len(client.Actions) > 0 {
				t.Errorf("unexpected client actions: %v", client.Actions)
			}
		} else {
			if len(cloud.Balancers) != 1 {
				t.Errorf("expected one load balancer to be created, got %v", cloud.Balancers)
			} else if cloud.Balancers[0].Name != controller.loadBalancerName(item.service) ||
				cloud.Balancers[0].Region != region ||
				cloud.Balancers[0].Ports[0] != item.service.Spec.Ports[0].Port {
				t.Errorf("created load balancer has incorrect parameters: %v", cloud.Balancers[0])
			}
			actionFound := false
			for _, action := range client.Actions {
				if action.Action == "update-service" {
					actionFound = true
				}
			}
			if !actionFound {
				t.Errorf("expected updated service to be sent to client, got these actions instead: %v", client.Actions)
			}
		}
	}
}

// TODO(a-robinson): Add tests for update/sync/delete.
