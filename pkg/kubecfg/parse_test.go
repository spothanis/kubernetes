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
	"testing"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"gopkg.in/v1/yaml"
)

func TestParseBadStorage(t *testing.T) {
	p := NewParser(map[string]runtime.Object{})
	_, err := p.ToWireFormat([]byte("{}"), "badstorage", runtime.DefaultCodec)
	if err == nil {
		t.Errorf("Expected error, received none")
	}
}

func DoParseTest(t *testing.T, storage string, obj runtime.Object, p *Parser) {
	jsonData, _ := runtime.DefaultCodec.Encode(obj)
	var tmp map[string]interface{}
	json.Unmarshal(jsonData, &tmp)
	yamlData, _ := yaml.Marshal(tmp)
	t.Logf("Intermediate yaml:\n%v\n", string(yamlData))
	t.Logf("Intermediate json:\n%v\n", string(jsonData))
	jsonGot, jsonErr := p.ToWireFormat(jsonData, storage, runtime.DefaultCodec)
	yamlGot, yamlErr := p.ToWireFormat(yamlData, storage, runtime.DefaultCodec)

	if jsonErr != nil {
		t.Errorf("json err: %#v", jsonErr)
	}
	if yamlErr != nil {
		t.Errorf("yaml err: %#v", yamlErr)
	}
	if string(jsonGot) != string(jsonData) {
		t.Errorf("json output didn't match:\nGot:\n%v\n\nWanted:\n%v\n",
			string(jsonGot), string(jsonData))
	}
	if string(yamlGot) != string(jsonData) {
		t.Errorf("yaml parsed output didn't match:\nGot:\n%v\n\nWanted:\n%v\n",
			string(yamlGot), string(jsonData))
	}
}

var testParser = NewParser(map[string]runtime.Object{
	"pods":                   &api.Pod{},
	"services":               &api.Service{},
	"replicationControllers": &api.ReplicationController{},
})

func TestParsePod(t *testing.T) {
	DoParseTest(t, "pods", &api.Pod{
		JSONBase: api.JSONBase{APIVersion: "v1beta1", ID: "test pod", Kind: "Pod"},
		DesiredState: api.PodState{
			Manifest: api.ContainerManifest{
				ID: "My manifest",
				Containers: []api.Container{
					{Name: "my container"},
				},
				Volumes: []api.Volume{
					{Name: "volume"},
				},
			},
		},
	}, testParser)
}

func TestParseService(t *testing.T) {
	DoParseTest(t, "services", &api.Service{
		JSONBase: api.JSONBase{APIVersion: "v1beta1", ID: "my service", Kind: "Service"},
		Port:     8080,
		Labels: map[string]string{
			"area": "staging",
		},
		Selector: map[string]string{
			"area": "staging",
		},
	}, testParser)
}

func TestParseController(t *testing.T) {
	DoParseTest(t, "replicationControllers", &api.ReplicationController{
		JSONBase: api.JSONBase{APIVersion: "v1beta1", ID: "my controller", Kind: "ReplicationController"},
		DesiredState: api.ReplicationControllerState{
			Replicas: 9001,
			PodTemplate: api.PodTemplate{
				DesiredState: api.PodState{
					Manifest: api.ContainerManifest{
						ID: "My manifest",
						Containers: []api.Container{
							{Name: "my container"},
						},
						Volumes: []api.Volume{
							{Name: "volume"},
						},
					},
				},
			},
		},
	}, testParser)
}

type TestParseType struct {
	api.JSONBase `json:",inline" yaml:",inline"`
	Data         string `json:"data" yaml:"data"`
}

func (*TestParseType) IsAnAPIObject() {}

func TestParseCustomType(t *testing.T) {
	runtime.DefaultScheme.AddKnownTypes("", &TestParseType{})
	runtime.DefaultScheme.AddKnownTypes("v1beta1", &TestParseType{})
	parser := NewParser(map[string]runtime.Object{
		"custom": &TestParseType{},
	})
	DoParseTest(t, "custom", &TestParseType{
		JSONBase: api.JSONBase{APIVersion: "", ID: "my custom object", Kind: "TestParseType"},
		Data:     "test data",
	}, parser)
}
