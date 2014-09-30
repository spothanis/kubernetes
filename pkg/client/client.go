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

package client

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/latest"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/labels"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/version"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
)

// Interface holds the methods for clients of Kubernetes,
// an interface to allow mock testing.
// TODO: these should return/take pointers.
type Interface interface {
	PodInterface
	ReplicationControllerInterface
	ServiceInterface
	VersionInterface
	MinionInterface
}

// PodInterface has methods to work with Pod resources.
type PodInterface interface {
	ListPods(selector labels.Selector) (*api.PodList, error)
	GetPod(id string) (*api.Pod, error)
	DeletePod(id string) error
	CreatePod(*api.Pod) (*api.Pod, error)
	UpdatePod(*api.Pod) (*api.Pod, error)
}

// ReplicationControllerInterface has methods to work with ReplicationController resources.
type ReplicationControllerInterface interface {
	ListReplicationControllers(selector labels.Selector) (*api.ReplicationControllerList, error)
	GetReplicationController(id string) (*api.ReplicationController, error)
	CreateReplicationController(*api.ReplicationController) (*api.ReplicationController, error)
	UpdateReplicationController(*api.ReplicationController) (*api.ReplicationController, error)
	DeleteReplicationController(string) error
	WatchReplicationControllers(label, field labels.Selector, resourceVersion uint64) (watch.Interface, error)
}

// ServiceInterface has methods to work with Service resources.
type ServiceInterface interface {
	ListServices(selector labels.Selector) (*api.ServiceList, error)
	GetService(id string) (*api.Service, error)
	CreateService(*api.Service) (*api.Service, error)
	UpdateService(*api.Service) (*api.Service, error)
	DeleteService(string) error
	WatchServices(label, field labels.Selector, resourceVersion uint64) (watch.Interface, error)
}

// EndpointsInterface has methods to work with Endpoints resources
type EndpointsInterface interface {
	ListEndpoints(selector labels.Selector) (*api.EndpointsList, error)
	GetEndpoints(id string) (*api.Endpoints, error)
	WatchEndpoints(label, field labels.Selector, resourceVersion uint64) (watch.Interface, error)
}

// VersionInterface has a method to retrieve the server version.
type VersionInterface interface {
	ServerVersion() (*version.Info, error)
}

type MinionInterface interface {
	ListMinions() (*api.MinionList, error)
}

// Client is the actual implementation of a Kubernetes client.
type Client struct {
	*RESTClient
}

// New creates a Kubernetes client. This client works with pods, replication controllers
// and services. It allows operations such as list, get, update and delete on these objects.
// host must be a host string, a host:port combo, or an http or https URL.  Passing a prefix
// to a URL will prepend the server path. The API version to use may be specified or left
// empty to use the client preferred version. Returns an error if host cannot be converted to
// a valid URL.
func New(ctx api.Context, host, version string, auth *AuthInfo) (*Client, error) {
	if version == "" {
		// Clients default to the preferred code API version
		// TODO: implement version negotation (highest version supported by server)
		version = latest.Version
	}
	versionInterfaces, err := latest.InterfacesFor(version)
	if err != nil {
		return nil, fmt.Errorf("API version '%s' is not recognized (valid values: %s)", version, strings.Join(latest.Versions, ", "))
	}
	prefix := fmt.Sprintf("/api/%s/", version)
	restClient, err := NewRESTClient(ctx, host, auth, prefix, versionInterfaces.Codec)
	if err != nil {
		return nil, fmt.Errorf("API URL '%s' is not valid: %v", host, err)
	}
	return &Client{restClient}, nil
}

// NewOrDie creates a Kubernetes client and panics if the provided host is invalid.
func NewOrDie(ctx api.Context, host, version string, auth *AuthInfo) *Client {
	client, err := New(ctx, host, version, auth)
	if err != nil {
		panic(err)
	}
	return client
}

// StatusErr might get returned from an api call if your request is still being processed
// and hence the expected return data is not available yet.
type StatusErr struct {
	Status api.Status
}

func (s *StatusErr) Error() string {
	return fmt.Sprintf("Status: %v (%#v)", s.Status.Status, s.Status)
}

// AuthInfo is used to store authorization information.
type AuthInfo struct {
	User     string
	Password string
	CAFile   string
	CertFile string
	KeyFile  string
}

// RESTClient holds common code used to work with API resources that follow the
// Kubernetes API pattern.
// Host is the http://... base for the URL
type RESTClient struct {
	ctx        api.Context
	host       string
	prefix     string
	secure     bool
	auth       *AuthInfo
	httpClient *http.Client
	Sync       bool
	PollPeriod time.Duration
	Timeout    time.Duration
	Codec      runtime.Codec
}

// NewRESTClient creates a new RESTClient. This client performs generic REST functions
// such as Get, Put, Post, and Delete on specified paths.
func NewRESTClient(ctx api.Context, host string, auth *AuthInfo, path string, c runtime.Codec) (*RESTClient, error) {
	prefix, err := normalizePrefix(host, path)
	if err != nil {
		return nil, err
	}
	base := *prefix
	base.Path = ""
	base.RawQuery = ""
	base.Fragment = ""

	var config *tls.Config
	if auth != nil && len(auth.CertFile) != 0 {
		cert, err := tls.LoadX509KeyPair(auth.CertFile, auth.KeyFile)
		if err != nil {
			return nil, err
		}
		data, err := ioutil.ReadFile(auth.CAFile)
		if err != nil {
			return nil, err
		}
		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(data)
		config = &tls.Config{
			Certificates: []tls.Certificate{
				cert,
			},
			RootCAs:    certPool,
			ClientCAs:  certPool,
			ClientAuth: tls.RequireAndVerifyClientCert,
		}
	} else {
		config = &tls.Config{
			InsecureSkipVerify: true,
		}
	}

	return &RESTClient{
		ctx:    ctx,
		host:   base.String(),
		prefix: prefix.Path,
		secure: prefix.Scheme == "https",
		auth:   auth,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: config,
			},
		},
		Sync:       false,
		PollPeriod: time.Second * 2,
		Timeout:    time.Second * 20,
		Codec:      c,
	}, nil
}

// normalizePrefix ensures the passed initial value is valid.
func normalizePrefix(host, prefix string) (*url.URL, error) {
	if host == "" {
		return nil, fmt.Errorf("host must be a URL or a host:port pair")
	}
	base := host
	hostURL, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	if hostURL.Scheme == "" {
		hostURL, err = url.Parse("http://" + base)
		if err != nil {
			return nil, err
		}
		if hostURL.Path != "" && hostURL.Path != "/" {
			return nil, fmt.Errorf("host must be a URL or a host:port pair: %s", base)
		}
	}
	hostURL.Path += prefix

	return hostURL, nil
}

// Secure returns true if the client is configured for secure connections.
func (c *RESTClient) Secure() bool {
	return c.secure
}

// doRequest executes a request, adds authentication (if auth != nil), and HTTPS
// cert ignoring.
func (c *RESTClient) doRequest(request *http.Request) ([]byte, error) {
	if c.auth != nil {
		request.SetBasicAuth(c.auth.User, c.auth.Password)
	}
	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return body, err
	}

	// Did the server give us a status response?
	isStatusResponse := false
	var status api.Status
	if err := latest.Codec.DecodeInto(body, &status); err == nil && status.Status != "" {
		isStatusResponse = true
	}

	switch {
	case response.StatusCode == http.StatusConflict:
		// Return error given by server, if there was one.
		if isStatusResponse {
			return nil, &StatusErr{status}
		}
		fallthrough
	case response.StatusCode < http.StatusOK || response.StatusCode > http.StatusPartialContent:
		return nil, fmt.Errorf("request [%#v] failed (%d) %s: %s", request, response.StatusCode, response.Status, string(body))
	}

	// If the server gave us a status back, look at what it was.
	if isStatusResponse && status.Status != api.StatusSuccess {
		// "Working" requests need to be handled specially.
		// "Failed" requests are clearly just an error and it makes sense to return them as such.
		return nil, &StatusErr{status}
	}
	return body, err
}

// ListPods takes a selector, and returns the list of pods that match that selector.
func (c *Client) ListPods(selector labels.Selector) (result *api.PodList, err error) {
	result = &api.PodList{}
	err = c.Get().Path("pods").SelectorParam("labels", selector).Do().Into(result)
	return
}

// GetPod takes the id of the pod, and returns the corresponding Pod object, and an error if it occurs
func (c *Client) GetPod(id string) (result *api.Pod, err error) {
	result = &api.Pod{}
	err = c.Get().Path("pods").Path(id).Do().Into(result)
	return
}

// DeletePod takes the id of the pod, and returns an error if one occurs
func (c *Client) DeletePod(id string) error {
	return c.Delete().Path("pods").Path(id).Do().Error()
}

// CreatePod takes the representation of a pod.  Returns the server's representation of the pod, and an error, if it occurs.
func (c *Client) CreatePod(pod *api.Pod) (result *api.Pod, err error) {
	result = &api.Pod{}
	err = c.Post().Path("pods").Body(pod).Do().Into(result)
	return
}

// UpdatePod takes the representation of a pod to update.  Returns the server's representation of the pod, and an error, if it occurs.
func (c *Client) UpdatePod(pod *api.Pod) (result *api.Pod, err error) {
	result = &api.Pod{}
	if pod.ResourceVersion == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", pod)
		return
	}
	err = c.Put().Path("pods").Path(pod.ID).Body(pod).Do().Into(result)
	return
}

// ListReplicationControllers takes a selector, and returns the list of replication controllers that match that selector.
func (c *Client) ListReplicationControllers(selector labels.Selector) (result *api.ReplicationControllerList, err error) {
	result = &api.ReplicationControllerList{}
	err = c.Get().Path("replicationControllers").SelectorParam("labels", selector).Do().Into(result)
	return
}

// GetReplicationController returns information about a particular replication controller.
func (c *Client) GetReplicationController(id string) (result *api.ReplicationController, err error) {
	result = &api.ReplicationController{}
	err = c.Get().Path("replicationControllers").Path(id).Do().Into(result)
	return
}

// CreateReplicationController creates a new replication controller.
func (c *Client) CreateReplicationController(controller *api.ReplicationController) (result *api.ReplicationController, err error) {
	result = &api.ReplicationController{}
	err = c.Post().Path("replicationControllers").Body(controller).Do().Into(result)
	return
}

// UpdateReplicationController updates an existing replication controller.
func (c *Client) UpdateReplicationController(controller *api.ReplicationController) (result *api.ReplicationController, err error) {
	result = &api.ReplicationController{}
	if controller.ResourceVersion == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", controller)
		return
	}
	err = c.Put().Path("replicationControllers").Path(controller.ID).Body(controller).Do().Into(result)
	return
}

// DeleteReplicationController deletes an existing replication controller.
func (c *Client) DeleteReplicationController(id string) error {
	return c.Delete().Path("replicationControllers").Path(id).Do().Error()
}

// WatchReplicationControllers returns a watch.Interface that watches the requested controllers.
func (c *Client) WatchReplicationControllers(label, field labels.Selector, resourceVersion uint64) (watch.Interface, error) {
	return c.Get().
		Path("watch").
		Path("replicationControllers").
		UintParam("resourceVersion", resourceVersion).
		SelectorParam("labels", label).
		SelectorParam("fields", field).
		Watch()
}

// ListServices takes a selector, and returns the list of services that match that selector
func (c *Client) ListServices(selector labels.Selector) (result *api.ServiceList, err error) {
	result = &api.ServiceList{}
	err = c.Get().Path("services").SelectorParam("labels", selector).Do().Into(result)
	return
}

// GetService returns information about a particular service.
func (c *Client) GetService(id string) (result *api.Service, err error) {
	result = &api.Service{}
	err = c.Get().Path("services").Path(id).Do().Into(result)
	return
}

// CreateService creates a new service.
func (c *Client) CreateService(svc *api.Service) (result *api.Service, err error) {
	result = &api.Service{}
	err = c.Post().Path("services").Body(svc).Do().Into(result)
	return
}

// UpdateService updates an existing service.
func (c *Client) UpdateService(svc *api.Service) (result *api.Service, err error) {
	result = &api.Service{}
	if svc.ResourceVersion == 0 {
		err = fmt.Errorf("invalid update object, missing resource version: %v", svc)
		return
	}
	err = c.Put().Path("services").Path(svc.ID).Body(svc).Do().Into(result)
	return
}

// DeleteService deletes an existing service.
func (c *Client) DeleteService(id string) error {
	return c.Delete().Path("services").Path(id).Do().Error()
}

// WatchServices returns a watch.Interface that watches the requested services.
func (c *Client) WatchServices(label, field labels.Selector, resourceVersion uint64) (watch.Interface, error) {
	return c.Get().
		Path("watch").
		Path("services").
		UintParam("resourceVersion", resourceVersion).
		SelectorParam("labels", label).
		SelectorParam("fields", field).
		Watch()
}

// ListEndpoints takes a selector, and returns the list of endpoints that match that selector
func (c *Client) ListEndpoints(selector labels.Selector) (result *api.EndpointsList, err error) {
	result = &api.EndpointsList{}
	err = c.Get().Path("endpoints").SelectorParam("labels", selector).Do().Into(result)
	return
}

// GetEndpoints returns information about the endpoints for a particular service.
func (c *Client) GetEndpoints(id string) (result *api.Endpoints, err error) {
	result = &api.Endpoints{}
	err = c.Get().Path("endpoints").Path(id).Do().Into(result)
	return
}

// WatchEndpoints returns a watch.Interface that watches the requested endpoints for a service.
func (c *Client) WatchEndpoints(label, field labels.Selector, resourceVersion uint64) (watch.Interface, error) {
	return c.Get().
		Path("watch").
		Path("endpoints").
		UintParam("resourceVersion", resourceVersion).
		SelectorParam("labels", label).
		SelectorParam("fields", field).
		Watch()
}

func (c *Client) CreateEndpoints(endpoints *api.Endpoints) (*api.Endpoints, error) {
	result := &api.Endpoints{}
	err := c.Post().Path("endpoints").Body(endpoints).Do().Into(result)
	return result, err
}

func (c *Client) UpdateEndpoints(endpoints *api.Endpoints) (*api.Endpoints, error) {
	result := &api.Endpoints{}
	if endpoints.ResourceVersion == 0 {
		return nil, fmt.Errorf("invalid update object, missing resource version: %v", endpoints)
	}
	err := c.Put().
		Path("endpoints").
		Path(endpoints.ID).
		Body(endpoints).
		Do().
		Into(result)
	return result, err
}

// ServerVersion retrieves and parses the server's version.
func (c *Client) ServerVersion() (*version.Info, error) {
	body, err := c.Get().AbsPath("/version").Do().Raw()
	if err != nil {
		return nil, err
	}
	var info version.Info
	err = json.Unmarshal(body, &info)
	if err != nil {
		return nil, fmt.Errorf("Got '%s': %v", string(body), err)
	}
	return &info, nil
}

// ListMinions lists all the minions in the cluster.
func (c *Client) ListMinions() (result *api.MinionList, err error) {
	result = &api.MinionList{}
	err = c.Get().Path("minions").Do().Into(result)
	return
}
