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

// Reads the configuration from the file. Example file for two services [nodejs & mysql]
//{"Services": [
//   {
//      "Name":"nodejs",
//      "Port":10000,
//      "Endpoints":["10.240.180.168:8000", "10.240.254.199:8000", "10.240.62.150:8000"]
//   },
//   {
//      "Name":"mysql",
//      "Port":10001,
//      "Endpoints":["10.240.180.168:9000", "10.240.254.199:9000", "10.240.62.150:9000"]
//   }
//]
//}

package config

import (
	"bytes"
	"encoding/json"
	"io/ioutil"
	"reflect"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/golang/glog"
)

// serviceConfig is a deserialized form of the config file format which ConfigSourceFile accepts.
type serviceConfig struct {
	Services []struct {
		Name      string   `json: "name"`
		Port      int      `json: "port"`
		Endpoints []string `json: "endpoints"`
	} `json: "service"`
}

// ConfigSourceFile periodically reads service configurations in JSON from a file, and sends the services and endpoints defined in th file to the specified channels.
type ConfigSourceFile struct {
	serviceChannel   chan ServiceUpdate
	endpointsChannel chan EndpointsUpdate
	filename         string
}

// NewConfigSourceFile creates a new ConfigSourceFile.
// It immediately runs the created ConfigSourceFile in a goroutine.
func NewConfigSourceFile(filename string, serviceChannel chan ServiceUpdate, endpointsChannel chan EndpointsUpdate) ConfigSourceFile {
	config := ConfigSourceFile{
		filename:         filename,
		serviceChannel:   serviceChannel,
		endpointsChannel: endpointsChannel,
	}
	go config.Run()
	return config
}

// Run begins watching the config file.
func (impl ConfigSourceFile) Run() {
	glog.Infof("Watching file %s", impl.filename)
	var lastData []byte
	var lastServices []api.Service
	var lastEndpoints []api.Endpoints

	for {
		data, err := ioutil.ReadFile(impl.filename)
		if err != nil {
			glog.Errorf("Couldn't read file: %s : %v", impl.filename, err)
			continue
		}

		if bytes.Equal(lastData, data) {
			continue
		}
		lastData = data

		config := &serviceConfig{}
		if err = json.Unmarshal(data, config); err != nil {
			glog.Errorf("Couldn't unmarshal configuration from file : %s %v", data, err)
			continue
		}
		// Ok, we have a valid configuration, send to channel for
		// rejiggering.
		newServices := make([]api.Service, len(config.Services))
		newEndpoints := make([]api.Endpoints, len(config.Services))
		for i, service := range config.Services {
			newServices[i] = api.Service{JSONBase: api.JSONBase{ID: service.Name}, Port: service.Port}
			newEndpoints[i] = api.Endpoints{Name: service.Name, Endpoints: service.Endpoints}
		}
		if !reflect.DeepEqual(lastServices, newServices) {
			serviceUpdate := ServiceUpdate{Op: SET, Services: newServices}
			impl.serviceChannel <- serviceUpdate
			lastServices = newServices
		}
		if !reflect.DeepEqual(lastEndpoints, newEndpoints) {
			endpointsUpdate := EndpointsUpdate{Op: SET, Endpoints: newEndpoints}
			impl.endpointsChannel <- endpointsUpdate
			lastEndpoints = newEndpoints
		}

		time.Sleep(5 * time.Second)
	}
}
