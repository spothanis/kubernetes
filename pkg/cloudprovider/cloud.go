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

package cloudprovider

import (
	"net"
)

// CloudInterface is an abstract, pluggable interface for cloud providers
type Interface interface {
	// TCPLoadBalancer returns a balancer interface. Also returns true if the interface is supported, false otherwise.
	TCPLoadBalancer() (TCPLoadBalancer, bool)
	// Instances returns an instances interface. Also returns true if the interface is supported, false otherwise.
	Instances() (Instances, bool)
}

type TCPLoadBalancer interface {
	// TODO: Break this up into different interfaces (LB, etc) when we have more than one type of service
	TCPLoadBalancerExists(name, region string) (bool, error)
	CreateTCPLoadBalancer(name, region string, port int, hosts []string) error
	UpdateTCPLoadBalancer(name, region string, hosts []string) error
	DeleteTCPLoadBalancer(name, region string) error
}

type Instances interface {
	IPAddress(name string) (net.IP, error)
	// Lists instances that match 'filter' which is a regular expression which must match the entire instance name (fqdn)
	List(filter string) ([]string, error)
}
