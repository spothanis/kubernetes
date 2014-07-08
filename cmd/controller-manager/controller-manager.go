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

// The controller manager is responsible for monitoring replication controllers, and creating corresponding
// pods to achieve the desired state.  It listens for new controllers in etcd, and it sends requests to the
// master to create/delete pods.
//
// TODO: Refactor the etcd watch code so that it is a pluggable interface.
package main

import (
	"flag"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/controller"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/coreos/go-etcd/etcd"
	"github.com/golang/glog"
)

var (
	etcdServers = flag.String("etcd_servers", "", "Servers for the etcd (http://ip:port).")
	master      = flag.String("master", "", "The address of the Kubernetes API server")
)

func main() {
	flag.Parse()
	util.InitLogs()
	defer util.FlushLogs()

	if len(*etcdServers) == 0 || len(*master) == 0 {
		glog.Fatal("usage: controller-manager -etcd_servers <servers> -master <master>")
	}

	// Set up logger for etcd client
	etcd.SetLogger(util.NewLogger("etcd "))

	controllerManager := controller.MakeReplicationManager(
		etcd.NewClient([]string{*etcdServers}),
		client.New("http://"+*master, nil))

	controllerManager.Run(10 * time.Second)
	select {}
}
