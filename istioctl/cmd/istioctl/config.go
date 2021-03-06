// Copyright 2018 Istio Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"bytes"
	"fmt"

	"github.com/spf13/cobra"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"

	"istio.io/istio/pkg/log"
)

var (

	// TODO - Pull in remaining xDS information from pilot agent via curl and add to output
	// TODO - Add config-diff to get the difference between pilot's xDS API response and the proxy config
	// TODO - Add support for non-default proxy config locations
	// TODO - Add support for non-kube istio deployments
	configCmd = &cobra.Command{
		Use:   "proxy-config <pod-name> [<configuration-type>]",
		Short: "Retrieves local proxy configuration for the specified pod [kube only]",
		Long: `
Retrieves the local proxy configuration for the specified pod when running in Kubernetes.

Available configuration types:

	[clusters listeners routes static]

`,
		Example: `# Retrieve all config for productpage-v1-bb8d5cbc7-k7qbm pod
istioctl proxy-config productpage-v1-bb8d5cbc7-k7qbm

# Retrieve cluster config for productpage-v1-bb8d5cbc7-k7qbm pod
istioctl proxy-config productpage-v1-bb8d5cbc7-k7qbm clusters

# Retrieve static config for productpage-v1-bb8d5cbc7-k7qbm pod in the application namespace
istioctl proxy-config -n application productpage-v1-bb8d5cbc7-k7qbm static`,
		Aliases: []string{"pc"},
		Args:    cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			podName := args[0]
			var configType string
			if len(args) > 1 {
				configType = args[1]
			} else {
				configType = "all"
			}
			log.Infof("Retrieving %v proxy config for %q", configType, podName)

			ns := namespace
			if ns == v1.NamespaceAll {
				ns = defaultNamespace
			}
			debug, err := callPilotAgentDebug(podName, ns, configType)
			if err != nil {
				return err
			}
			fmt.Println(debug)
			return nil
		},
	}
)

func init() {
	rootCmd.AddCommand(configCmd)
}

func createCoreV1Client() (*rest.RESTClient, error) {
	config, err := defaultRestConfig()
	if err != nil {
		return nil, err
	}
	return rest.RESTClientFor(config)
}

func defaultRestConfig() (*rest.Config, error) {
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	config.APIPath = "/api"
	config.GroupVersion = &v1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	return config, nil
}

func callPilotAgentDebug(podName, podNamespace, configType string) (string, error) {
	cmd := []string{"/usr/local/bin/pilot-agent", "debug", configType}
	if stdout, stderr, err := podExec(podName, podNamespace, cmd); err != nil {
		return "", err
	} else if stderr.String() != "" {
		return "", fmt.Errorf("unable to call pilot-agent debug: %v", stderr.String())
	} else {
		return stdout.String(), nil
	}
}

func podExec(podName, podNamespace string, command []string) (*bytes.Buffer, *bytes.Buffer, error) {
	client, err := createCoreV1Client()
	if err != nil {
		return nil, nil, err
	}

	req := client.Post().
		Resource("pods").
		Name(podName).
		Namespace(podNamespace).
		SubResource("exec").
		Param("container", "istio-proxy").
		VersionedParams(&v1.PodExecOptions{
			Container: "istio-proxy",
			Command:   command,
			Stdin:     false,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	config, err := defaultRestConfig()
	if err != nil {
		return nil, nil, err
	}

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		return nil, nil, err
	}

	var stdout, stderr bytes.Buffer
	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	return &stdout, &stderr, err
}
