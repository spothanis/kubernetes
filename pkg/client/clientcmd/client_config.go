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

package clientcmd

import (
	"io"
	"os"

	"github.com/imdario/mergo"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/clientauth"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/util"
)

var (
	// TODO: eventually apiserver should start on 443 and be secure by default
	defaultCluster = Cluster{Server: "http://localhost:8080"}
	envVarCluster  = Cluster{Server: os.Getenv("KUBERNETES_MASTER")}
)

// ClientConfig is used to make it easy to get an api server client
type ClientConfig interface {
	// ClientConfig returns a complete client config
	ClientConfig() (*client.Config, error)
}

// DirectClientConfig is a ClientConfig interface that is backed by a Config, options overrides, and an optional fallbackReader for auth information
type DirectClientConfig struct {
	config         Config
	contextName    string
	overrides      *ConfigOverrides
	fallbackReader io.Reader
}

// NewDefaultClientConfig creates a DirectClientConfig using the config.CurrentContext as the context name
func NewDefaultClientConfig(config Config, overrides *ConfigOverrides) ClientConfig {
	return DirectClientConfig{config, config.CurrentContext, overrides, nil}
}

// NewNonInteractiveClientConfig creates a DirectClientConfig using the passed context name and does not have a fallback reader for auth information
func NewNonInteractiveClientConfig(config Config, contextName string, overrides *ConfigOverrides) ClientConfig {
	return DirectClientConfig{config, contextName, overrides, nil}
}

// NewInteractiveClientConfig creates a DirectClientConfig using the passed context name and a reader in case auth information is not provided via files or flags
func NewInteractiveClientConfig(config Config, contextName string, overrides *ConfigOverrides, fallbackReader io.Reader) ClientConfig {
	return DirectClientConfig{config, contextName, overrides, fallbackReader}
}

// ClientConfig implements ClientConfig
func (config DirectClientConfig) ClientConfig() (*client.Config, error) {
	if err := config.ConfirmUsable(); err != nil {
		return nil, err
	}

	configAuthInfo := config.getAuthInfo()
	configClusterInfo := config.getCluster()

	clientConfig := &client.Config{}
	clientConfig.Host = configClusterInfo.Server
	clientConfig.Version = configClusterInfo.APIVersion

	// only try to read the auth information if we are secure
	if client.IsConfigTransportTLS(*clientConfig) {
		var err error

		// mergo is a first write wins for map value and a last writing wins for interface values
		userAuthPartialConfig, err := getUserIdentificationPartialConfig(configAuthInfo, config.fallbackReader)
		if err != nil {
			return nil, err
		}
		mergo.Merge(clientConfig, userAuthPartialConfig)

		serverAuthPartialConfig, err := getServerIdentificationPartialConfig(configAuthInfo, configClusterInfo)
		if err != nil {
			return nil, err
		}
		mergo.Merge(clientConfig, serverAuthPartialConfig)
	}

	return clientConfig, nil
}

// clientauth.Info object contain both user identification and server identification.  We want different precedence orders for
// both, so we have to split the objects and merge them separately
// we want this order of precedence for the server identification
// 1.  configClusterInfo (the final result of command line flags and merged .kubeconfig files)
// 2.  configAuthInfo.auth-path (this file can contain information that conflicts with #1, and we want #1 to win the priority)
// 3.  load the ~/.kubernetes_auth file as a default
func getServerIdentificationPartialConfig(configAuthInfo AuthInfo, configClusterInfo Cluster) (*client.Config, error) {
	mergedConfig := &client.Config{}

	defaultAuthPathInfo, err := NewDefaultAuthLoader().LoadAuth(os.Getenv("HOME") + "/.kubernetes_auth")
	// if the error is anything besides a does not exist, then fail.  Not existing is ok
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	if defaultAuthPathInfo != nil {
		defaultAuthPathConfig := makeServerIdentificationConfig(*defaultAuthPathInfo)
		mergo.Merge(mergedConfig, defaultAuthPathConfig)
	}

	if len(configAuthInfo.AuthPath) > 0 {
		authPathInfo, err := NewDefaultAuthLoader().LoadAuth(configAuthInfo.AuthPath)
		if err != nil {
			return nil, err
		}
		authPathConfig := makeServerIdentificationConfig(*authPathInfo)
		mergo.Merge(mergedConfig, authPathConfig)
	}

	// configClusterInfo holds the information identify the server provided by .kubeconfig
	configClientConfig := &client.Config{}
	configClientConfig.CAFile = configClusterInfo.CertificateAuthority
	configClientConfig.Insecure = configClusterInfo.InsecureSkipTLSVerify
	mergo.Merge(mergedConfig, configClientConfig)

	return mergedConfig, nil
}

// clientauth.Info object contain both user identification and server identification.  We want different precedence orders for
// both, so we have to split the objects and merge them separately
// we want this order of precedence for user identifcation
// 1.  configAuthInfo minus auth-path (the final result of command line flags and merged .kubeconfig files)
// 2.  configAuthInfo.auth-path (this file can contain information that conflicts with #1, and we want #1 to win the priority)
// 3.  if there is not enough information to idenfity the user, load try the ~/.kubernetes_auth file
// 4.  if there is not enough information to identify the user, prompt if possible
func getUserIdentificationPartialConfig(configAuthInfo AuthInfo, fallbackReader io.Reader) (*client.Config, error) {
	mergedConfig := &client.Config{}

	if len(configAuthInfo.AuthPath) > 0 {
		authPathInfo, err := NewDefaultAuthLoader().LoadAuth(configAuthInfo.AuthPath)
		if err != nil {
			return nil, err
		}
		authPathConfig := makeUserIdentificationConfig(*authPathInfo)
		mergo.Merge(mergedConfig, authPathConfig)
	}

	// blindly overwrite existing values based on precedence
	if len(configAuthInfo.Token) > 0 {
		mergedConfig.BearerToken = configAuthInfo.Token
	}
	if len(configAuthInfo.ClientCertificate) > 0 {
		mergedConfig.CertFile = configAuthInfo.ClientCertificate
		mergedConfig.KeyFile = configAuthInfo.ClientKey
	}

	// if there isn't sufficient information to authenticate the user to the server, merge in ~/.kubernetes_auth.
	if !canIdentifyUser(*mergedConfig) {
		defaultAuthPathInfo, err := NewDefaultAuthLoader().LoadAuth(os.Getenv("HOME") + "/.kubernetes_auth")
		// if the error is anything besides a does not exist, then fail.  Not existing is ok
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if defaultAuthPathInfo != nil {
			defaultAuthPathConfig := makeUserIdentificationConfig(*defaultAuthPathInfo)
			previouslyMergedConfig := mergedConfig
			mergedConfig = &client.Config{}
			mergo.Merge(mergedConfig, defaultAuthPathConfig)
			mergo.Merge(mergedConfig, previouslyMergedConfig)
		}
	}

	// if there still isn't enough information to authenticate the user, try prompting
	if !canIdentifyUser(*mergedConfig) && (fallbackReader != nil) {
		prompter := NewPromptingAuthLoader(fallbackReader)
		promptedAuthInfo := prompter.Prompt()

		promptedConfig := makeUserIdentificationConfig(*promptedAuthInfo)
		previouslyMergedConfig := mergedConfig
		mergedConfig = &client.Config{}
		mergo.Merge(mergedConfig, promptedConfig)
		mergo.Merge(mergedConfig, previouslyMergedConfig)
	}

	return mergedConfig, nil
}

// makeUserIdentificationFieldsConfig returns a client.Config capable of being merged using mergo for only user identification information
func makeUserIdentificationConfig(info clientauth.Info) *client.Config {
	config := &client.Config{}
	config.Username = info.User
	config.Password = info.Password
	config.CertFile = info.CertFile
	config.KeyFile = info.KeyFile
	config.BearerToken = info.BearerToken
	return config
}

// makeUserIdentificationFieldsConfig returns a client.Config capable of being merged using mergo for only server identification information
func makeServerIdentificationConfig(info clientauth.Info) client.Config {
	config := client.Config{}
	config.CAFile = info.CAFile
	if info.Insecure != nil {
		config.Insecure = *info.Insecure
	}
	return config
}

func canIdentifyUser(config client.Config) bool {
	return len(config.Username) > 0 ||
		len(config.CertFile) > 0 ||
		len(config.BearerToken) > 0

}

// ConfirmUsable looks a particular context and determines if that particular part of the config is useable.  There might still be errors in the config,
// but no errors in the sections requested or referenced.  It does not return early so that it can find as many errors as possible.
func (config DirectClientConfig) ConfirmUsable() error {
	validationErrors := make([]error, 0)
	validationErrors = append(validationErrors, validateAuthInfo(config.getAuthInfoName(), config.getAuthInfo())...)
	validationErrors = append(validationErrors, validateClusterInfo(config.getClusterName(), config.getCluster())...)

	return util.SliceToError(validationErrors)
}

func (config DirectClientConfig) getContextName() string {
	if len(config.overrides.CurrentContext) != 0 {
		return config.overrides.CurrentContext
	}
	if len(config.contextName) != 0 {
		return config.contextName
	}

	return config.config.CurrentContext
}

func (config DirectClientConfig) getAuthInfoName() string {
	if len(config.overrides.AuthInfoName) != 0 {
		return config.overrides.AuthInfoName
	}
	return config.getContext().AuthInfo
}

func (config DirectClientConfig) getClusterName() string {
	if len(config.overrides.ClusterName) != 0 {
		return config.overrides.ClusterName
	}
	return config.getContext().Cluster
}

func (config DirectClientConfig) getContext() Context {
	return config.config.Contexts[config.getContextName()]
}

func (config DirectClientConfig) getAuthInfo() AuthInfo {
	authInfos := config.config.AuthInfos
	authInfoName := config.getAuthInfoName()

	var mergedAuthInfo AuthInfo
	if configAuthInfo, exists := authInfos[authInfoName]; exists {
		mergo.Merge(&mergedAuthInfo, configAuthInfo)
	}
	mergo.Merge(&mergedAuthInfo, config.overrides.AuthInfo)

	return mergedAuthInfo
}

func (config DirectClientConfig) getCluster() Cluster {
	clusterInfos := config.config.Clusters
	clusterInfoName := config.getClusterName()

	var mergedClusterInfo Cluster
	mergo.Merge(&mergedClusterInfo, defaultCluster)
	mergo.Merge(&mergedClusterInfo, envVarCluster)
	if configClusterInfo, exists := clusterInfos[clusterInfoName]; exists {
		mergo.Merge(&mergedClusterInfo, configClusterInfo)
	}
	mergo.Merge(&mergedClusterInfo, config.overrides.ClusterInfo)

	return mergedClusterInfo
}
