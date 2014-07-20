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

// apiserver is the main api server and master for the cluster.
// it is responsible for serving the cluster management API.
package main

import (
	"flag"
	"net"
	"net/http"
	"strconv"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/cloudprovider"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/master"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
)

var (
	port                        = flag.Uint("port", 8080, "The port to listen on.  Default 8080.")
	address                     = flag.String("address", "127.0.0.1", "The address on the local server to listen to. Default 127.0.0.1")
	apiPrefix                   = flag.String("api_prefix", "/api/v1beta1", "The prefix for API requests on the server. Default '/api/v1beta1'")
	cloudProvider               = flag.String("cloud_provider", "", "The provider for cloud services.  Empty string for no provider.")
	minionRegexp                = flag.String("minion_regexp", "", "If non empty, and -cloud_provider is specified, a regular expression for matching minion VMs")
	minionPort                  = flag.Uint("minion_port", 10250, "The port at which kubelet will be listening on the minions.")
	etcdServerList, machineList util.StringList
)

func init() {
	flag.Var(&etcdServerList, "etcd_servers", "List of etcd servers to watch (http://ip:port), comma separated")
	flag.Var(&machineList, "machines", "List of machines to schedule onto, comma separated.")
}

func main() {
	flag.Parse()
	util.InitLogs()
	defer util.FlushLogs()

	if len(machineList) == 0 {
		glog.Fatal("No machines specified!")
	}

	var cloud cloudprovider.Interface
	switch *cloudProvider {
	case "gce":
		var err error
		cloud, err = cloudprovider.NewGCECloud()
		if err != nil {
			glog.Fatal("Couldn't connect to GCE cloud: %#v", err)
		}
	default:
		if len(*cloudProvider) > 0 {
			glog.Infof("Unknown cloud provider: %s", *cloudProvider)
		} else {
			glog.Info("No cloud provider specified.")
		}
	}

	podInfoGetter := &client.HTTPPodInfoGetter{
		Client: http.DefaultClient,
		Port:   *minionPort,
	}

	client := client.New("http://"+net.JoinHostPort(*address, strconv.Itoa(int(*port))), nil)

	var m *master.Master
	if len(etcdServerList) > 0 {
		m = master.New(etcdServerList, machineList, podInfoGetter, cloud, *minionRegexp, client)
	} else {
		m = master.NewMemoryServer(machineList, podInfoGetter, cloud, client)
	}

	glog.Fatal(m.Run(net.JoinHostPort(*address, strconv.Itoa(int(*port))), *apiPrefix))
}
