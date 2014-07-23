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
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	kube_client "github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubecfg"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/golang/glog"
)

// AppVersion is the current version of kubecfg.
const AppVersion = "0.1"

var (
	versionFlag  = flag.Bool("V", false, "Print the version number.")
	httpServer   = flag.String("h", "", "The host to connect to.")
	config       = flag.String("c", "", "Path to the config file.")
	selector     = flag.String("l", "", "Selector (label query) to use for listing")
	updatePeriod = flag.Duration("u", 60*time.Second, "Update interarrival period")
	portSpec     = flag.String("p", "", "The port spec, comma-separated list of <external>:<internal>,...")
	servicePort  = flag.Int("s", -1, "If positive, create and run a corresponding service on this port, only used with 'run'")
	authConfig   = flag.String("auth", os.Getenv("HOME")+"/.kubernetes_auth", "Path to the auth info file.  If missing, prompt the user.  Only used if doing https.")
	json         = flag.Bool("json", false, "If true, print raw JSON for responses")
	yaml         = flag.Bool("yaml", false, "If true, print raw YAML for responses")
	verbose      = flag.Bool("verbose", false, "If true, print extra information")
	proxy        = flag.Bool("proxy", false, "If true, run a proxy to the api server")
	www          = flag.String("www", "", "If -proxy is true, use this directory to serve static files")
	templateFile = flag.String("template_file", "", "If present load this file as a golang template and us it for output printing")
	templateStr  = flag.String("template", "", "If present parse this string as a golang template and us it for output printing")
)

func usage() {
	fmt.Fprintf(os.Stderr, `usage: kubecfg -h [-c config/file.json] [-p :,..., :] <method>

  Kubernetes REST API:
  kubecfg [OPTIONS] get|list|create|delete|update <%s>[/<id>]

  Manage replication controllers:
  kubecfg [OPTIONS] stop|rm|rollingupdate <controller>
  kubecfg [OPTIONS] run <image> <replicas> <controller>
  kubecfg [OPTIONS] resize <controller> <replicas>

  Options:
`, prettyWireStorage())
	flag.PrintDefaults()
}

func prettyWireStorage() string {
	types := kubecfg.SupportedWireStorage()
	sort.Strings(types)
	return strings.Join(types, "|")
}

// readConfig reads and parses pod, replicationController, and service
// configuration files. If any errors log and exit non-zero.
func readConfig(storage string) []byte {
	if len(*config) == 0 {
		glog.Fatal("Need config file (-c)")
	}
	data, err := ioutil.ReadFile(*config)
	if err != nil {
		glog.Fatalf("Unable to read %v: %v\n", *config, err)
	}
	data, err = kubecfg.ToWireFormat(data, storage)
	if err != nil {
		glog.Fatalf("Error parsing %v as an object for %v: %v\n", *config, storage, err)
	}
	if *verbose {
		glog.Infof("Parsed config file successfully; sending:\n%v\n", string(data))
	}
	return data
}

func main() {
	flag.Usage = func() {
		usage()
	}

	flag.Parse()
	util.InitLogs()
	defer util.FlushLogs()

	if *versionFlag {
		fmt.Println("Version:", AppVersion)
		os.Exit(0)
	}

	secure := true
	var masterServer string
	if len(*httpServer) > 0 {
		masterServer = *httpServer
	} else if len(os.Getenv("KUBERNETES_MASTER")) > 0 {
		masterServer = os.Getenv("KUBERNETES_MASTER")
	} else {
		masterServer = "http://localhost:8080"
	}
	parsedURL, err := url.Parse(masterServer)
	if err != nil {
		glog.Fatalf("Unable to parse %v as a URL\n", err)
	}
	if parsedURL.Scheme != "" && parsedURL.Scheme != "https" {
		secure = false
	}

	var auth *kube_client.AuthInfo
	if secure {
		auth, err = kubecfg.LoadAuthInfo(*authConfig)
		if err != nil {
			glog.Fatalf("Error loading auth: %v", err)
		}
	}

	if *proxy {
		glog.Info("Starting to serve on localhost:8001")
		server := kubecfg.NewProxyServer(*www, masterServer, auth)
		glog.Fatal(server.Serve())
	}

	if len(flag.Args()) < 1 {
		usage()
		os.Exit(1)
	}
	method := flag.Arg(0)

	client := kube_client.New(masterServer, auth)

	matchFound := executeAPIRequest(method, client) || executeControllerRequest(method, client)
	if matchFound == false {
		glog.Fatalf("Unknown command %s", method)
	}
}

func executeAPIRequest(method string, s *kube_client.Client) bool {
	if len(flag.Args()) < 2 {
		glog.Fatalf("usage: kubecfg [OPTIONS] get|list|create|update|delete <%s>[/<id>]", prettyWireStorage())
	}

	verb := ""
	segments := strings.SplitN(flag.Arg(1), "/", 2)
	storage := segments[0]
	path := strings.Trim(flag.Arg(1), "/")
	setBody := false
	switch method {
	case "get", "list":
		verb = "GET"
	case "delete":
		verb = "DELETE"
		if len(segments) == 1 || segments[1] == "" {
			glog.Fatalf("usage: kubecfg [OPTIONS] delete <%s>/<id>", prettyWireStorage())
		}
	case "create":
		verb = "POST"
		setBody = true
		if len(segments) != 1 {
			glog.Fatalf("usage: kubecfg [OPTIONS] create <%s>", prettyWireStorage())
		}
	case "update":
		verb = "PUT"
		setBody = true
		if len(segments) == 1 || segments[1] == "" {
			glog.Fatalf("usage: kubecfg [OPTIONS] update <%s>/<id>", prettyWireStorage())
		}
	default:
		return false
	}

	r := s.Verb(verb).
		Path(path).
		ParseSelector(*selector)
	if setBody {
		r.Body(readConfig(storage))
	}
	result := r.Do()
	obj, err := result.Get()
	if err != nil {
		glog.Fatalf("Got request error: %v\n", err)
		return false
	}

	var printer kubecfg.ResourcePrinter
	switch {
	case *json:
		printer = &kubecfg.IdentityPrinter{}
	case *yaml:
		printer = &kubecfg.YAMLPrinter{}
	case len(*templateFile) > 0 || len(*templateStr) > 0:
		var data []byte
		if len(*templateFile) > 0 {
			var err error
			data, err = ioutil.ReadFile(*templateFile)
			if err != nil {
				glog.Fatalf("Error reading template %s, %v\n", *templateFile, err)
				return false
			}
		} else {
			data = []byte(*templateStr)
		}
		tmpl, err := template.New("output").Parse(string(data))
		if err != nil {
			glog.Fatalf("Error parsing template %s, %v\n", string(data), err)
			return false
		}
		printer = &kubecfg.TemplatePrinter{
			Template: tmpl,
		}
	default:
		printer = &kubecfg.HumanReadablePrinter{}
	}

	if err = printer.PrintObj(obj, os.Stdout); err != nil {
		body, _ := result.Raw()
		glog.Fatalf("Failed to print: %v\nRaw received object:\n%#v\n\nBody received: %v", err, obj, string(body))
	}
	fmt.Print("\n")

	return true
}

func executeControllerRequest(method string, c *kube_client.Client) bool {
	parseController := func() string {
		if len(flag.Args()) != 2 {
			glog.Fatal("usage: kubecfg [OPTIONS] stop|rm|rollingupdate <controller>")
		}
		return flag.Arg(1)
	}

	var err error
	switch method {
	case "stop":
		err = kubecfg.StopController(parseController(), c)
	case "rm":
		err = kubecfg.DeleteController(parseController(), c)
	case "rollingupdate":
		err = kubecfg.Update(parseController(), c, *updatePeriod)
	case "run":
		if len(flag.Args()) != 4 {
			glog.Fatal("usage: kubecfg [OPTIONS] run <image> <replicas> <controller>")
		}
		image := flag.Arg(1)
		replicas, err := strconv.Atoi(flag.Arg(2))
		name := flag.Arg(3)
		if err != nil {
			glog.Fatalf("Error parsing replicas: %v", err)
		}
		err = kubecfg.RunController(image, name, replicas, c, *portSpec, *servicePort)
	case "resize":
		args := flag.Args()
		if len(args) < 3 {
			glog.Fatal("usage: kubecfg resize <controller> <replicas>")
		}
		name := args[1]
		replicas, err := strconv.Atoi(args[2])
		if err != nil {
			glog.Fatalf("Error parsing replicas: %v", err)
		}
		err = kubecfg.ResizeController(name, replicas, c)
	default:
		return false
	}
	if err != nil {
		glog.Fatalf("Error: %v", err)
	}
	return true
}
