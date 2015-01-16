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

package main

import (
	"net"
	"net/http"
	"strconv"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client/record"
	_ "github.com/GoogleCloudPlatform/kubernetes/pkg/healthz"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/master/ports"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/version/verflag"
	"github.com/GoogleCloudPlatform/kubernetes/plugin/pkg/scheduler"
	_ "github.com/GoogleCloudPlatform/kubernetes/plugin/pkg/scheduler/algorithmprovider"
	"github.com/GoogleCloudPlatform/kubernetes/plugin/pkg/scheduler/factory"
	"github.com/golang/glog"

	flag "github.com/spf13/pflag"
)

var (
	port              = flag.Int("port", ports.SchedulerPort, "The port that the scheduler's http service runs on")
	address           = util.IP(net.ParseIP("127.0.0.1"))
	clientConfig      = &client.Config{}
	algorithmProvider = flag.String("algorithm_provider", factory.DefaultProvider, "The scheduling algorithm provider to use")
)

func init() {
	flag.Var(&address, "address", "The IP address to serve on (set to 0.0.0.0 for all interfaces)")
	client.BindClientConfigFlags(flag.CommandLine, clientConfig)
}

func main() {
	util.InitFlags()
	util.InitLogs()
	defer util.FlushLogs()

	verflag.PrintAndExitIfRequested()

	kubeClient, err := client.New(clientConfig)
	if err != nil {
		glog.Fatalf("Invalid API configuration: %v", err)
	}

	record.StartRecording(kubeClient.Events(""), api.EventSource{Component: "scheduler"})

	go http.ListenAndServe(net.JoinHostPort(address.String(), strconv.Itoa(*port)), nil)

	configFactory := factory.NewConfigFactory(kubeClient)
	config, err := createConfig(configFactory)
	if err != nil {
		glog.Fatalf("Failed to create scheduler configuration: %v", err)
	}
	s := scheduler.New(config)
	s.Run()

	select {}
}

func createConfig(configFactory *factory.ConfigFactory) (*scheduler.Config, error) {
	// check of algorithm provider is registered and fail fast
	_, err := factory.GetAlgorithmProvider(*algorithmProvider)
	if err != nil {
		return nil, err
	}
	return configFactory.CreateFromProvider(*algorithmProvider)
}
