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

// An all-in-one binary for standing up a fake Kubernetes cluster on your
// local machine.
// Assumes that there is a pre-existing etcd server running on localhost.
package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/controller"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubelet"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/master"
	"github.com/coreos/go-etcd/etcd"
	"github.com/fsouza/go-dockerclient"
)

// kubelet flags
var (
	file               = flag.String("config", "", "Path to the config file/dir")
	syncFrequency      = flag.Duration("sync_frequency", 10*time.Second, "Max period between synchronizing running containers and config")
	fileCheckFrequency = flag.Duration("file_check_frequency", 20*time.Second, "Duration between checking file for new data")
	httpCheckFrequency = flag.Duration("http_check_frequency", 20*time.Second, "Duration between checking http for new data")
	manifestUrl        = flag.String("manifest_url", "", "URL for accessing the container manifest")
	kubeletAddress     = flag.String("kubelet_address", "127.0.0.1", "The address for the kubelet info server to serve on")
	kubeletPort        = flag.Uint("kubelet_port", 10250, "The port for the kubelete info server to serve on")
)

// master flags
var (
	masterPort    = flag.Uint("master_port", 8080, "The port for the master to listen on.  Default 8080.")
	masterAddress = flag.String("master_address", "127.0.0.1", "The address for the master to listen to. Default 127.0.0.1")
	apiPrefix     = flag.String("api_prefix", "/api/v1beta1", "The prefix for API requests on the server. Default '/api/v1beta1'")
)

// flags that affect both
var (
	etcdServer = flag.String("etcd_server", "http://localhost:4001", "Url of local etcd server")
)

// Starts kubelet services. Never returns.
func fakeKubelet() {
	endpoint := "unix:///var/run/docker.sock"
	dockerClient, err := docker.NewClient(endpoint)
	if err != nil {
		log.Fatal("Couldn't connnect to docker.")
	}

	myKubelet := kubelet.Kubelet{
		Hostname:           *kubeletAddress,
		DockerClient:       dockerClient,
		FileCheckFrequency: *fileCheckFrequency,
		SyncFrequency:      *syncFrequency,
		HTTPCheckFrequency: *httpCheckFrequency,
	}
	myKubelet.RunKubelet(*file, *manifestUrl, *etcdServer, *kubeletAddress, *kubeletPort)
}

// Starts api services (the master). Never returns.
func apiServer() {
	m := master.New([]string{*etcdServer}, []string{*kubeletAddress}, nil)
	log.Fatal(m.Run(net.JoinHostPort(*masterAddress, strconv.Itoa(int(*masterPort))), *apiPrefix))
}

// Starts up a controller manager. Never returns.
func controllerManager() {
	controllerManager := controller.MakeReplicationManager(
		etcd.NewClient([]string{*etcdServer}),
		client.New(fmt.Sprintf("http://%s:%d", *masterAddress, *masterPort), nil))

	controllerManager.Run(20 * time.Second)
	select {}
}

func main() {
	flag.Parse()
	rand.Seed(time.Now().UTC().UnixNano())

	// Set up logger for etcd client
	etcd.SetLogger(log.New(os.Stderr, "etcd ", log.LstdFlags))

	go apiServer()
	go fakeKubelet()
	go controllerManager()

	log.Printf("All components started.\nMaster running at: http://%s:%d\nKubelet running at: http://%s:%d\n",
		*masterAddress, *masterPort,
		*kubeletAddress, *kubeletPort)
	select {}
}
