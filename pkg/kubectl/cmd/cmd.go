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

package cmd

import (
	"fmt"
	"io"
	"os"

	"github.com/GoogleCloudPlatform/kubernetes/pkg/api"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/latest"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/meta"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/api/validation"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/client/clientcmd"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/kubectl"
	"github.com/GoogleCloudPlatform/kubernetes/pkg/runtime"

	"github.com/golang/glog"
	"github.com/spf13/cobra"
)

const (
	FlagMatchBinaryVersion = "match-server-version"
)

// Factory provides abstractions that allow the Kubectl command to be extended across multiple types
// of resources and different API sets.
type Factory struct {
	ClientConfig clientcmd.ClientConfig
	Mapper       meta.RESTMapper
	Typer        runtime.ObjectTyper
	Client       func(cmd *cobra.Command, mapping *meta.RESTMapping) (kubectl.RESTClient, error)
	Describer    func(cmd *cobra.Command, mapping *meta.RESTMapping) (kubectl.Describer, error)
	Printer      func(cmd *cobra.Command, mapping *meta.RESTMapping, noHeaders bool) (kubectl.ResourcePrinter, error)
	Validator    func(*cobra.Command) (validation.Schema, error)
}

// NewFactory creates a factory with the default Kubernetes resources defined
func NewFactory(clientConfig clientcmd.ClientConfig) *Factory {
	ret := &Factory{
		ClientConfig: clientConfig,
		Mapper:       latest.RESTMapper,
		Typer:        api.Scheme,
		Printer: func(cmd *cobra.Command, mapping *meta.RESTMapping, noHeaders bool) (kubectl.ResourcePrinter, error) {
			return kubectl.NewHumanReadablePrinter(noHeaders), nil
		},
	}

	ret.Validator = func(cmd *cobra.Command) (validation.Schema, error) {
		if GetFlagBool(cmd, "validate") {
			client, err := getClient(ret.ClientConfig, GetFlagBool(cmd, FlagMatchBinaryVersion))
			if err != nil {
				return nil, err
			}
			return &clientSwaggerSchema{client, api.Scheme}, nil
		} else {
			return validation.NullSchema{}, nil
		}
	}
	ret.Client = func(cmd *cobra.Command, mapping *meta.RESTMapping) (kubectl.RESTClient, error) {
		return getClient(ret.ClientConfig, GetFlagBool(cmd, FlagMatchBinaryVersion))
	}
	ret.Describer = func(cmd *cobra.Command, mapping *meta.RESTMapping) (kubectl.Describer, error) {
		client, err := getClient(ret.ClientConfig, GetFlagBool(cmd, FlagMatchBinaryVersion))
		if err != nil {
			return nil, err
		}
		describer, ok := kubectl.DescriberFor(mapping.Kind, client)
		if !ok {
			return nil, fmt.Errorf("no description has been implemented for %q", mapping.Kind)
		}
		return describer, nil
	}
	return ret
}

func (f *Factory) Run(out io.Writer) {
	// Parent command to which all subcommands are added.
	cmds := &cobra.Command{
		Use:   "kubectl",
		Short: "kubectl controls the Kubernetes cluster manager",
		Long: `kubectl controls the Kubernetes cluster manager.

Find more information at https://github.com/GoogleCloudPlatform/kubernetes.`,
		Run: runHelp,
	}

	f.ClientConfig = getClientConfig(cmds)

	// Globally persistent flags across all subcommands.
	// TODO Change flag names to consts to allow safer lookup from subcommands.
	// TODO Add a verbose flag that turns on glog logging. Probably need a way
	// to do that automatically for every subcommand.
	cmds.PersistentFlags().Bool(FlagMatchBinaryVersion, false, "Require server version to match client version")
	cmds.PersistentFlags().String("ns-path", os.Getenv("HOME")+"/.kubernetes_ns", "Path to the namespace info file that holds the namespace context to use for CLI requests.")
	cmds.PersistentFlags().StringP("namespace", "n", "", "If present, the namespace scope for this CLI request.")
	cmds.PersistentFlags().Bool("validate", false, "If true, use a schema to validate the input before sending it")

	cmds.AddCommand(f.NewCmdVersion(out))
	cmds.AddCommand(f.NewCmdProxy(out))

	cmds.AddCommand(f.NewCmdGet(out))
	cmds.AddCommand(f.NewCmdDescribe(out))
	cmds.AddCommand(f.NewCmdCreate(out))
	cmds.AddCommand(f.NewCmdCreateAll(out))
	cmds.AddCommand(f.NewCmdUpdate(out))
	cmds.AddCommand(f.NewCmdDelete(out))

	cmds.AddCommand(NewCmdNamespace(out))
	cmds.AddCommand(f.NewCmdLog(out))
	cmds.AddCommand(f.NewCmdRollingUpdate(out))

	if err := cmds.Execute(); err != nil {
		os.Exit(1)
	}
}

// getClientBuilder creates a clientcmd.ClientConfig that has a hierarchy like this:
//   1.  Use the kubeconfig builder.  The number of merges and overrides here gets a little crazy.  Stay with me.
//       1.  Merge together the kubeconfig itself.  This is done with the following hierarchy and merge rules:
//           1.  CommandLineLocation - this parsed from the command line, so it must be late bound
//           2.  EnvVarLocation
//           3.  CurrentDirectoryLocation
//           4.  HomeDirectoryLocation
//           Empty filenames are ignored.  Files with non-deserializable content produced errors.
//           The first file to set a particular value or map key wins and the value or map key is never changed.
//           This means that the first file to set CurrentContext will have its context preserved.  It also means
//           that if two files specify a "red-user", only values from the first file's red-user are used.  Even
//           non-conflicting entries from the second file's "red-user" are discarded.
//       2.  Determine the context to use based on the first hit in this chain
//           1.  command line argument - again, parsed from the command line, so it must be late bound
//           2.  CurrentContext from the merged kubeconfig file
//           3.  Empty is allowed at this stage
//       3.  Determine the cluster info and auth info to use.  At this point, we may or may not have a context.  They
//           are built based on the first hit in this chain.  (run it twice, once for auth, once for cluster)
//           1.  command line argument
//           2.  If context is present, then use the context value
//           3.  Empty is allowed
//       4.  Determine the actual cluster info to use.  At this point, we may or may not have a cluster info.  Build
//           each piece of the cluster info based on the chain:
//           1.  command line argument
//           2.  If cluster info is present and a value for the attribute is present, use it.
//           3.  If you don't have a server location, bail.
//       5.  Auth info is build using the same rules as cluster info, EXCEPT that you can only have one authentication
//           technique per auth info.  The following conditions result in an error:
//           1.  If there are two conflicting techniques specified from the command line, fail.
//           2.  If the command line does not specify one, and the auth info has conflicting techniques, fail.
//           3.  If the command line specifies one and the auth info specifies another, honor the command line technique.
//   2.  Use default values and potentially prompt for auth information
func getClientConfig(cmd *cobra.Command) clientcmd.ClientConfig {
	loadingRules := clientcmd.NewClientConfigLoadingRules()
	loadingRules.EnvVarPath = os.Getenv(clientcmd.RecommendedConfigPathEnvVar)
	cmd.PersistentFlags().StringVar(&loadingRules.CommandLinePath, "kubeconfig", "", "Path to the kubeconfig file to use for CLI requests.")

	overrides := &clientcmd.ConfigOverrides{}
	overrides.BindFlags(cmd.PersistentFlags(), clientcmd.RecommendedConfigOverrideFlags(""))
	clientConfig := clientcmd.NewInteractiveDeferredLoadingClientConfig(loadingRules, overrides, os.Stdin)

	return clientConfig
}

func checkErr(err error) {
	if err != nil {
		glog.FatalDepth(1, err)
	}
}

func usageError(cmd *cobra.Command, format string, args ...interface{}) {
	glog.Errorf(format, args...)
	glog.Errorf("See '%s -h' for help.", cmd.CommandPath())
	os.Exit(1)
}

func runHelp(cmd *cobra.Command, args []string) {
	cmd.Help()
}

// GetKubeNamespace returns the value of the namespace a
// user provided on the command line or use the default
// namespace.
func GetKubeNamespace(cmd *cobra.Command) string {
	result := api.NamespaceDefault
	if ns := GetFlagString(cmd, "namespace"); len(ns) > 0 {
		result = ns
		glog.V(2).Infof("Using namespace from -ns flag")
	} else {
		nsPath := GetFlagString(cmd, "ns-path")
		nsInfo, err := kubectl.LoadNamespaceInfo(nsPath)
		if err != nil {
			glog.Fatalf("Error loading current namespace: %v", err)
		}
		result = nsInfo.Namespace
	}
	glog.V(2).Infof("Using namespace %s", result)
	return result
}

// GetExplicitKubeNamespace returns the value of the namespace a
// user explicitly provided on the command line, or false if no
// such namespace was specified.
func GetExplicitKubeNamespace(cmd *cobra.Command) (string, bool) {
	if ns := GetFlagString(cmd, "namespace"); len(ns) > 0 {
		return ns, true
	}
	// TODO: determine when --ns-path is set but equal to the default
	// value and return its value and true.
	return "", false
}

type clientSwaggerSchema struct {
	c *client.Client
	t runtime.ObjectTyper
}

func (c *clientSwaggerSchema) ValidateBytes(data []byte) error {
	version, _, err := c.t.DataVersionAndKind(data)
	if err != nil {
		return err
	}
	schemaData, err := c.c.RESTClient.Get().
		AbsPath("/swaggerapi/api", version).
		Do().
		Raw()
	if err != nil {
		return err
	}
	schema, err := validation.NewSwaggerSchemaFromBytes(schemaData)
	if err != nil {
		return err
	}
	return schema.ValidateBytes(data)
}

// TODO Need to only run server version match once per client host creation
func getClient(clientConfig clientcmd.ClientConfig, matchServerVersion bool) (*client.Client, error) {
	config, err := clientConfig.ClientConfig()
	if err != nil {
		return nil, err
	}

	if matchServerVersion {
		err := client.MatchesServerVersion(config)
		if err != nil {
			return nil, err
		}
	}

	client, err := client.New(config)
	if err != nil {
		return nil, err
	}

	return client, nil
}
