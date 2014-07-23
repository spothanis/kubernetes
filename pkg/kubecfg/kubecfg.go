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

package kubecfg

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/golang/glog"
	"gopkg.in/v1/yaml"
)

func promptForString(field string) string {
	fmt.Printf("Please enter %s: ", field)
	var result string
	fmt.Scan(&result)
	return result
}

// LoadAuthInfo parses an AuthInfo object from a file path. It prompts user and creates file if it doesn't exist.
func LoadAuthInfo(path string) (*client.AuthInfo, error) {
	var auth client.AuthInfo
	if _, err := os.Stat(path); os.IsNotExist(err) {
		auth.User = promptForString("Username")
		auth.Password = promptForString("Password")
		data, err := json.Marshal(auth)
		if err != nil {
			return &auth, err
		}
		err = ioutil.WriteFile(path, data, 0600)
		return &auth, err
	}
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, err
	}
	err = json.Unmarshal(data, &auth)
	if err != nil {
		return nil, err
	}
	return &auth, err
}

// Update performs a rolling update of a collection of pods.
// 'name' points to a replication controller.
// 'client' is used for updating pods.
// 'updatePeriod' is the time between pod updates.
func Update(name string, client client.Interface, updatePeriod time.Duration) error {
	controller, err := client.GetReplicationController(name)
	if err != nil {
		return err
	}
	s := labels.Set(controller.DesiredState.ReplicaSelector).AsSelector()

	podList, err := client.ListPods(s)
	if err != nil {
		return err
	}
	for _, pod := range podList.Items {
		// We delete the pod here, the controller will recreate it.  This will result in pulling
		// a new Docker image.  This isn't a full "update" but its what we support for now.
		err = client.DeletePod(pod.ID)
		if err != nil {
			return err
		}
		time.Sleep(updatePeriod)
	}
	return nil
}

// StopController stops a controller named 'name' by setting replicas to zero
func StopController(name string, client client.Interface) error {
	controller, err := client.GetReplicationController(name)
	if err != nil {
		return err
	}
	controller.DesiredState.Replicas = 0
	controllerOut, err := client.UpdateReplicationController(controller)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(controllerOut)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

// ResizeController resizes a controller named 'name' by setting replicas to 'replicas'
func ResizeController(name string, replicas int, client client.Interface) error {
	controller, err := client.GetReplicationController(name)
	if err != nil {
		return err
	}
	controller.DesiredState.Replicas = replicas
	controllerOut, err := client.UpdateReplicationController(controller)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(controllerOut)
	if err != nil {
		return err
	}
	fmt.Print(string(data))
	return nil
}

func makePorts(spec string) []api.Port {
	parts := strings.Split(spec, ",")
	var result []api.Port
	for _, part := range parts {
		pieces := strings.Split(part, ":")
		if len(pieces) != 2 {
			glog.Infof("Bad port spec: %s", part)
			continue
		}
		host, err := strconv.Atoi(pieces[0])
		if err != nil {
			glog.Errorf("Host part is not integer: %s %v", pieces[0], err)
			continue
		}
		container, err := strconv.Atoi(pieces[1])
		if err != nil {
			glog.Errorf("Container part is not integer: %s %v", pieces[1], err)
			continue
		}
		result = append(result, api.Port{ContainerPort: container, HostPort: host})
	}
	return result
}

// RunController creates a new replication controller named 'name' which creates 'replicas' pods running 'image'
func RunController(image, name string, replicas int, client client.Interface, portSpec string, servicePort int) error {
	controller := api.ReplicationController{
		JSONBase: api.JSONBase{
			ID: name,
		},
		DesiredState: api.ReplicationControllerState{
			Replicas: replicas,
			ReplicaSelector: map[string]string{
				"name": name,
			},
			PodTemplate: api.PodTemplate{
				DesiredState: api.PodState{
					Manifest: api.ContainerManifest{
						Containers: []api.Container{
							{
								Name:  name,
								Image: image,
								Ports: makePorts(portSpec),
							},
						},
					},
				},
				Labels: map[string]string{
					"name": name,
				},
			},
		},
		Labels: map[string]string{
			"name": name,
		},
	}

	controllerOut, err := client.CreateReplicationController(controller)
	if err != nil {
		return err
	}
	data, err := yaml.Marshal(controllerOut)
	if err != nil {
		return err
	}
	fmt.Print(string(data))

	if servicePort > 0 {
		svc, err := createService(name, servicePort, client)
		if err != nil {
			return err
		}
		data, err = yaml.Marshal(svc)
		if err != nil {
			return err
		}
		fmt.Printf(string(data))
	}
	return nil
}

func createService(name string, port int, client client.Interface) (api.Service, error) {
	svc := api.Service{
		JSONBase: api.JSONBase{ID: name},
		Port:     port,
		Labels: map[string]string{
			"name": name,
		},
		Selector: map[string]string{
			"name": name,
		},
	}
	svc, err := client.CreateService(svc)
	return svc, err
}

// DeleteController deletes a replication controller named 'name', requires that the controller
// already be stopped
func DeleteController(name string, client client.Interface) error {
	controller, err := client.GetReplicationController(name)
	if err != nil {
		return err
	}
	if controller.DesiredState.Replicas != 0 {
		return fmt.Errorf("controller has non-zero replicas (%d), please stop it first", controller.DesiredState.Replicas)
	}
	return client.DeleteReplicationController(name)
}
