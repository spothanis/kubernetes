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

package api

import (
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/watch"
	"github.com/fsouza/go-dockerclient"
)

// Common string formats
// ---------------------
// Many fields in this API have formatting requirements.  The commonly used
// formats are defined here.
//
// C_IDENTIFIER:  This is a string that conforms the definition of an "identifier"
//     in the C language.  This is captured by the following regex:
//         [A-Za-z_][A-Za-z0-9_]*
//     This defines the format, but not the length restriction, which should be
//     specified at the definition of any field of this type.
//
// DNS_LABEL:  This is a string, no more than 63 characters long, that conforms
//     to the definition of a "label" in RFCs 1035 and 1123.  This is captured
//     by the following regex:
//         [a-z0-9]([-a-z0-9]*[a-z0-9])?
//
// DNS_SUBDOMAIN:  This is a string, no more than 253 characters long, that conforms
//      to the definition of a "subdomain" in RFCs 1035 and 1123.  This is captured
//      by the following regex:
//         [a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*
//     or more simply:
//         DNS_LABEL(\.DNS_LABEL)*

// ContainerManifest corresponds to the Container Manifest format, documented at:
// https://developers.google.com/compute/docs/containers/container_vms#container_manifest
// This is used as the representation of Kubernetes workloads.
type ContainerManifest struct {
	// Required: This must be a supported version string, such as "v1beta1".
	Version string `yaml:"version" json:"version"`
	// Required: This must be a DNS_SUBDOMAIN.
	// TODO: ID on Manifest is deprecated and will be removed in the future.
	ID         string      `yaml:"id" json:"id"`
	Volumes    []Volume    `yaml:"volumes" json:"volumes"`
	Containers []Container `yaml:"containers" json:"containers"`
}

// ContainerManifestList is used to communicate container manifests to kubelet.
type ContainerManifestList struct {
	JSONBase `json:",inline" yaml:",inline"`
	Items    []ContainerManifest `json:"items,omitempty" yaml:"items,omitempty"`
}

// Volume represents a named volume in a pod that may be accessed by any containers in the pod.
type Volume struct {
	// Required: This must be a DNS_LABEL.  Each volume in a pod must have
	// a unique name.
	Name string `yaml:"name" json:"name"`
	// Source represents the location and type of a volume to mount.
	// This is optional for now. If not specified, the Volume is implied to be an EmptyDir.
	// This implied behavior is deprecated and will be removed in a future version.
	Source *VolumeSource `yaml:"source" json:"source"`
}

type VolumeSource struct {
	// Only one of the following sources may be specified
	// HostDirectory represents a pre-existing directory on the host machine that is directly
	// exposed to the container. This is generally used for system agents or other privileged
	// things that are allowed to see the host machine. Most containers will NOT need this.
	// TODO(jonesdl) We need to restrict who can use host directory mounts and
	// who can/can not mount host directories as read/write.
	HostDirectory *HostDirectory `yaml:"hostDir" json:"hostDir"`
	// EmptyDirectory represents a temporary directory that shares a pod's lifetime.
	EmptyDirectory *EmptyDirectory `yaml:"emptyDir" json:"emptyDir"`
}

// Bare host directory volume.
type HostDirectory struct {
	Path string `yaml:"path" json:"path"`
}

type EmptyDirectory struct{}

// Port represents a network port in a single container
type Port struct {
	// Optional: If specified, this must be a DNS_LABEL.  Each named port
	// in a pod must have a unique name.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
	// Optional: Defaults to ContainerPort.  If specified, this must be a
	// valid port number, 0 < x < 65536.
	HostPort int `yaml:"hostPort,omitempty" json:"hostPort,omitempty"`
	// Required: This must be a valid port number, 0 < x < 65536.
	ContainerPort int `yaml:"containerPort" json:"containerPort"`
	// Optional: Defaults to "TCP".
	Protocol string `yaml:"protocol,omitempty" json:"protocol,omitempty"`
	// Optional: What host IP to bind the external port to.
	HostIP string `yaml:"hostIP,omitempty" json:"hostIP,omitempty"`
}

// VolumeMount describes a mounting of a Volume within a container
type VolumeMount struct {
	// Required: This must match the Name of a Volume [above].
	Name string `yaml:"name" json:"name"`
	// Optional: Defaults to false (read-write).
	ReadOnly bool `yaml:"readOnly,omitempty" json:"readOnly,omitempty"`
	// Required.
	// Exactly one of the following must be set.  If both are set, prefer MountPath.
	// DEPRECATED: Path will be removed in a future version of the API.
	MountPath string `yaml:"mountPath,omitempty" json:"mountPath,omitempty"`
	Path      string `yaml:"path,omitempty" json:"path,omitempty"`
	// One of: "LOCAL" (local volume) or "HOST" (external mount from the host). Default: LOCAL.
	// DEPRECATED: MountType will be removed in a future version of the API.
	MountType string `yaml:"mountType,omitempty" json:"mountType,omitempty"`
}

// EnvVar represents an environment variable present in a Container
type EnvVar struct {
	// Required: This must be a C_IDENTIFIER.
	Name string `yaml:"name" json:"name"`
	// Optional: defaults to "".
	Value string `yaml:"value,omitempty" json:"value,omitempty"`
}

// HTTPGetProbe describes a liveness probe based on HTTP Get requests.
type HTTPGetProbe struct {
	// Path to access on the http server
	Path string `yaml:"path,omitempty" json:"path,omitempty"`
	// Name or number of the port to access on the container
	Port string `yaml:"port,omitempty" json:"port,omitempty"`
	// Host name to connect to.  Optional, default: "localhost"
	Host string `yaml:"host,omitempty" json:"host,omitempty"`
}

// TCPSocketProbe describes a liveness probe based on opening a socket
type TCPSocketProbe struct {
	// Port is the port to connect to. Required.
	Port int `yaml:"port,omitempty" json:"port,omitempty"`
}

// LivenessProbe describes a liveness probe to be examined to the container.
type LivenessProbe struct {
	// Type of liveness probe.  Current legal values "http", "tcp"
	Type string `yaml:"type,omitempty" json:"type,omitempty"`
	// HTTPGetProbe parameters, required if Type == 'http'
	HTTPGet *HTTPGetProbe `yaml:"httpGet,omitempty" json:"httpGet,omitempty"`
	// TCPSocketProbe parameter, required if Type == 'tcp'
	TCPSocket *TCPSocketProbe `yaml:"tcpSocket,omitempty" json:"tcpSocket,omitempty"`
	// Length of time before health checking is activated.  In seconds.
	InitialDelaySeconds int64 `yaml:"initialDelaySeconds,omitempty" json:"initialDelaySeconds,omitempty"`
}

// Container represents a single container that is expected to be run on the host.
type Container struct {
	// Required: This must be a DNS_LABEL.  Each container in a pod must
	// have a unique name.
	Name string `yaml:"name" json:"name"`
	// Required.
	Image string `yaml:"image" json:"image"`
	// Optional: Defaults to whatever is defined in the image.
	Command []string `yaml:"command,omitempty" json:"command,omitempty"`
	// Optional: Defaults to Docker's default.
	WorkingDir string   `yaml:"workingDir,omitempty" json:"workingDir,omitempty"`
	Ports      []Port   `yaml:"ports,omitempty" json:"ports,omitempty"`
	Env        []EnvVar `yaml:"env,omitempty" json:"env,omitempty"`
	// Optional: Defaults to unlimited.
	Memory int `yaml:"memory,omitempty" json:"memory,omitempty"`
	// Optional: Defaults to unlimited.
	CPU           int            `yaml:"cpu,omitempty" json:"cpu,omitempty"`
	VolumeMounts  []VolumeMount  `yaml:"volumeMounts,omitempty" json:"volumeMounts,omitempty"`
	LivenessProbe *LivenessProbe `yaml:"livenessProbe,omitempty" json:"livenessProbe,omitempty"`
}

// Event is the representation of an event logged to etcd backends
type Event struct {
	Event     string             `json:"event,omitempty"`
	Manifest  *ContainerManifest `json:"manifest,omitempty"`
	Container *Container         `json:"container,omitempty"`
	Timestamp int64              `json:"timestamp"`
}

// The below types are used by kube_client and api_server.

// JSONBase is shared by all objects sent to, or returned from the client
type JSONBase struct {
	Kind              string `json:"kind,omitempty" yaml:"kind,omitempty"`
	ID                string `json:"id,omitempty" yaml:"id,omitempty"`
	CreationTimestamp string `json:"creationTimestamp,omitempty" yaml:"creationTimestamp,omitempty"`
	SelfLink          string `json:"selfLink,omitempty" yaml:"selfLink,omitempty"`
	ResourceVersion   uint64 `json:"resourceVersion,omitempty" yaml:"resourceVersion,omitempty"`
	APIVersion        string `json:"apiVersion,omitempty" yaml:"apiVersion,omitempty"`
}

// PodStatus represents a status of a pod.
type PodStatus string

// These are the valid statuses of pods.
const (
	// PodWaiting means that we're waiting for the pod to begin running.
	PodWaiting PodStatus = "Waiting"
	// PodRunning means that the pod is up and running.
	PodRunning PodStatus = "Running"
	// PodTerminated means that the pod has stopped.
	PodTerminated PodStatus = "Terminated"
)

// PodInfo contains one entry for every container with available info.
type PodInfo map[string]docker.Container

// RestartPolicyType represents a restart policy for a pod.
type RestartPolicyType string

// Valid restart policies defined for a PodState.RestartPolicy.
const (
	RestartAlways    RestartPolicyType = "RestartAlways"
	RestartOnFailure RestartPolicyType = "RestartOnFailure"
	RestartNever     RestartPolicyType = "RestartNever"
)

type RestartPolicy struct {
	// Optional: Defaults to "RestartAlways".
	Type RestartPolicyType `yaml:"type,omitempty" json:"type,omitempty"`
}

// PodState is the state of a pod, used as either input (desired state) or output (current state)
type PodState struct {
	Manifest ContainerManifest `json:"manifest,omitempty" yaml:"manifest,omitempty"`
	Status   PodStatus         `json:"status,omitempty" yaml:"status,omitempty"`
	Host     string            `json:"host,omitempty" yaml:"host,omitempty"`
	HostIP   string            `json:"hostIP,omitempty" yaml:"hostIP,omitempty"`
	PodIP    string            `json:"podIP,omitempty" yaml:"podIP,omitempty"`

	// The key of this map is the *name* of the container within the manifest; it has one
	// entry per container in the manifest. The value of this map is currently the output
	// of `docker inspect`. This output format is *not* final and should not be relied
	// upon.
	// TODO: Make real decisions about what our info should look like. Re-enable fuzz test
	// when we have done this.
	Info          PodInfo       `json:"info,omitempty" yaml:"info,omitempty"`
	RestartPolicy RestartPolicy `json:"restartpolicy,omitempty" yaml:"restartpolicy,omitempty"`
}

// PodList is a list of Pods.
type PodList struct {
	JSONBase `json:",inline" yaml:",inline"`
	Items    []Pod `json:"items" yaml:"items,omitempty"`
}

// Pod is a collection of containers, used as either input (create, update) or as output (list, get)
type Pod struct {
	JSONBase     `json:",inline" yaml:",inline"`
	Labels       map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	DesiredState PodState          `json:"desiredState,omitempty" yaml:"desiredState,omitempty"`
	CurrentState PodState          `json:"currentState,omitempty" yaml:"currentState,omitempty"`
}

// ReplicationControllerState is the state of a replication controller, either input (create, update) or as output (list, get)
type ReplicationControllerState struct {
	Replicas        int               `json:"replicas" yaml:"replicas"`
	ReplicaSelector map[string]string `json:"replicaSelector,omitempty" yaml:"replicaSelector,omitempty"`
	PodTemplate     PodTemplate       `json:"podTemplate,omitempty" yaml:"podTemplate,omitempty"`
}

// ReplicationControllerList is a collection of replication controllers.
type ReplicationControllerList struct {
	JSONBase `json:",inline" yaml:",inline"`
	Items    []ReplicationController `json:"items,omitempty" yaml:"items,omitempty"`
}

// ReplicationController represents the configuration of a replication controller
type ReplicationController struct {
	JSONBase     `json:",inline" yaml:",inline"`
	DesiredState ReplicationControllerState `json:"desiredState,omitempty" yaml:"desiredState,omitempty"`
	Labels       map[string]string          `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// PodTemplate holds the information used for creating pods
type PodTemplate struct {
	DesiredState PodState          `json:"desiredState,omitempty" yaml:"desiredState,omitempty"`
	Labels       map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
}

// ServiceList holds a list of services
type ServiceList struct {
	JSONBase `json:",inline" yaml:",inline"`
	Items    []Service `json:"items" yaml:"items"`
}

// Service is a named abstraction of software service (for example, mysql) consisting of local port
// (for example 3306) that the proxy listens on, and the selector that determines which pods
// will answer requests sent through the proxy.
type Service struct {
	JSONBase `json:",inline" yaml:",inline"`
	Port     int `json:"port,omitempty" yaml:"port,omitempty"`

	// This service's labels.
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// This service will route traffic to pods having labels matching this selector.
	Selector                   map[string]string `json:"selector,omitempty" yaml:"selector,omitempty"`
	CreateExternalLoadBalancer bool              `json:"createExternalLoadBalancer,omitempty" yaml:"createExternalLoadBalancer,omitempty"`

	// ContainerPort is the name of the port on the container to direct traffic to.
	// Optional, if unspecified use the first port on the container.
	ContainerPort util.IntOrString `json:"containerPort,omitempty" yaml:"containerPort,omitempty"`
}

// Endpoints is a collection of endpoints that implement the actual service, for example:
// Name: "mysql", Endpoints: ["10.10.1.1:1909", "10.10.2.2:8834"]
type Endpoints struct {
	JSONBase  `json:",inline" yaml:",inline"`
	Endpoints []string `json:"endpoints,omitempty" yaml:"endpoints,omitempty"`
}

// Minion is a worker node in Kubernetenes.
// The name of the minion according to etcd is in JSONBase.ID.
type Minion struct {
	JSONBase `json:",inline" yaml:",inline"`
	// Queried from cloud provider, if available.
	HostIP string `json:"hostIP,omitempty" yaml:"hostIP,omitempty"`
}

// MinionList is a list of minions.
type MinionList struct {
	JSONBase `json:",inline" yaml:",inline"`
	Items    []Minion `json:"minions,omitempty" yaml:"minions,omitempty"`
}

// Binding is written by a scheduler to cause a pod to be bound to a host.
type Binding struct {
	JSONBase `json:",inline" yaml:",inline"`
	PodID    string `json:"podID" yaml:"podID"`
	Host     string `json:"host" yaml:"host"`
}

// Status is a return value for calls that don't return other objects.
// TODO: this could go in apiserver, but I'm including it here so clients needn't
// import both.
type Status struct {
	JSONBase `json:",inline" yaml:",inline"`
	// One of: "success", "failure", "working" (for operations not yet completed)
	Status string `json:"status,omitempty" yaml:"status,omitempty"`
	// A human readable description of the status of this operation.
	Message string `json:"message,omitempty" yaml:"message,omitempty"`
	// A machine readable description of why this operation is in the
	// "failure" or "working" status. If this value is empty there
	// is no information available. A Reason clarifies an HTTP status
	// code but does not override it.
	Reason ReasonType `json:"reason,omitempty" yaml:"reason,omitempty"`
	// Extended data associated with the reason.  Each reason may define its
	// own extended details. This field is optional and the data returned
	// is not guaranteed to conform to any schema except that defined by
	// the reason type.
	Details *StatusDetails `json:"details,omitempty" yaml:"details,omitempty"`
	// Suggested HTTP return code for this status, 0 if not set.
	Code int `json:"code,omitempty" yaml:"code,omitempty"`
}

// StatusDetails is a set of additional properties that MAY be set by the
// server to provide additional information about a response. The Reason
// field of a Status object defines what attributes will be set. Clients
// must ignore fields that do not match the defined type of each attribute,
// and should assume that any attribute may be empty, invalid, or under
// defined.
type StatusDetails struct {
	// The ID attribute of the resource associated with the status ReasonType
	// (when there is a single ID which can be described).
	ID string `json:"id,omitempty" yaml:"id,omitempty"`
	// The kind attribute of the resource associated with the status ReasonType.
	// On some operations may differ from the requested resource Kind.
	Kind string `json:"kind,omitempty" yaml:"kind,omitempty"`
}

// Values of Status.Status
const (
	StatusSuccess = "success"
	StatusFailure = "failure"
	StatusWorking = "working"
)

// ReasonType is an enumeration of possible failure causes.  Each ReasonType
// must map to a single HTTP status code, but multiple reasons may map
// to the same HTTP status code.
// TODO: move to apiserver
type ReasonType string

const (
	// ReasonTypeUnknown means the server has declined to indicate a specific reason.
	// The details field may contain other information about this error.
	// Status code 500.
	ReasonTypeUnknown ReasonType = ""

	// ReasonTypeWorking means the server is processing this request and will complete
	// at a future time.
	// Details (optional):
	//   "kind" string - the name of the resource being referenced ("operation" today)
	//   "id"   string - the identifier of the Operation resource where updates
	//                   will be returned
	// Headers (optional):
	//   "Location" - HTTP header populated with a URL that can retrieved the final
	//                status of this operation.
	// Status code 202
	ReasonTypeWorking ReasonType = "working"

	// ResourceTypeNotFound means one or more resources required for this operation
	// could not be found.
	// Details (optional):
	//   "kind" string - the kind attribute of the missing resource
	//                   on some operations may differ from the requested
	//                   resource.
	//   "id"   string - the identifier of the missing resource
	// Status code 404
	ReasonTypeNotFound ReasonType = "not_found"

	// ReasonTypeAlreadyExists means the resource you are creating already exists.
	// Details (optional):
	//   "kind" string - the kind attribute of the conflicting resource
	//   "id"   string - the identifier of the conflicting resource
	// Status code 409
	ReasonTypeAlreadyExists ReasonType = "already_exists"

	// ResourceTypeConflict means the requested update operation cannot be completed
	// due to a conflict in the operation. The client may need to alter the request.
	// Each resource may define custom details that indicate the nature of the
	// conflict.
	// Status code 409
	ReasonTypeConflict ReasonType = "conflict"
)

// ServerOp is an operation delivered to API clients.
type ServerOp struct {
	JSONBase `yaml:",inline" json:",inline"`
}

// ServerOpList is a list of operations, as delivered to API clients.
type ServerOpList struct {
	JSONBase `yaml:",inline" json:",inline"`
	Items    []ServerOp `yaml:"items,omitempty" json:"items,omitempty"`
}

// WatchEvent objects are streamed from the api server in response to a watch request.
type WatchEvent struct {
	// The type of the watch event; added, modified, or deleted.
	Type watch.EventType

	// For added or modified objects, this is the new object; for deleted objects,
	// it's the state of the object immediately prior to its deletion.
	Object APIObject
}

// APIObject has appropriate encoder and decoder functions, such that on the wire, it's
// stored as a []byte, but in memory, the contained object is accessable as an interface{}
// via the Get() function. Only objects having a JSONBase may be stored via APIObject.
// The purpose of this is to allow an API object of type known only at runtime to be
// embedded within other API objects.
type APIObject struct {
	Object interface{}
}
