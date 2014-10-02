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

// podex is a command line tool to generate a kubernetes container
// manifest placeholder from docker image metadata.
//
// The manifest can then be edited by a human to match the deployment
// needs.
//
// Example usage:
//
// $ docker pull google/nodejs-hello
// $ podex -yaml google/nodejs-hello > google/nodejs-hello/pod.yaml
// $ podex -json google/nodejs-hello > google/nodejs-hello/pod.json

package main

import (
	"encoding/json"
	"flag"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	dockerclient "github.com/fsouza/go-dockerclient"
	"gopkg.in/yaml.v1"
)

const usage = "usage: podex [-json|-yaml] <repo/dockerimage>"

var generateJSON = flag.Bool("json", false, "generate json manifest")
var generateYAML = flag.Bool("yaml", false, "generate yaml manifest")

func main() {
	flag.Parse()

	if flag.NArg() < 1 {
		log.Fatal(usage)
	}

	imageName := flag.Arg(0)
	if len(imageName) == 0 {
		log.Fatal(usage)
	}

	if !*generateJSON && !*generateYAML {
		log.Fatal(usage)
	}

	parts := strings.Split(imageName, "/")
	baseName := parts[len(parts)-1]

	dockerHost := os.Getenv("DOCKER_HOST")
	docker, err := dockerclient.NewClient(dockerHost)
	if err != nil {
		log.Fatalf("failed to connect to %q: %v", dockerHost, err)
	}

	img, err := docker.InspectImage(imageName)
	if err != nil {
		log.Fatalf("failed to inspect image %q: %v", imageName, err)
	}
	manifest := api.ContainerManifest{
		Version: "v1beta1",
		ID:      baseName + "-pod",
		Containers: []api.Container{{
			Name:  baseName,
			Image: imageName,
		}},
		RestartPolicy: api.RestartPolicy{
			Always: &api.RestartPolicyAlways{},
		},
	}
	for p, _ := range img.Config.ExposedPorts {
		port, err := strconv.Atoi(p.Port())
		if err != nil {
			log.Fatalf("failed to parse port %q: %v", parts[0], err)
		}
		manifest.Containers[0].Ports = append(manifest.Containers[0].Ports, api.Port{
			Name:          strings.Join([]string{baseName, p.Proto(), p.Port()}, "-"),
			ContainerPort: port,
			Protocol:      api.Protocol(strings.ToUpper(p.Proto())),
		})
	}
	if *generateJSON {
		bs, err := json.MarshalIndent(manifest, "", "  ")
		if err != nil {
			log.Fatalf("failed to render JSON container manifest: %v", err)
		}
		os.Stdout.Write(bs)
	}
	if *generateYAML {
		bs, err := yaml.Marshal(manifest)
		if err != nil {
			log.Fatalf("failed to render YAML container manifest: %v", err)
		}
		os.Stdout.Write(bs)
	}
}
