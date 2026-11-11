/*
Copyright 2026.

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

package helm

import (
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

// restClientGetter is the minimal genericclioptions.RESTClientGetter that
// Helm's action.Configuration.Init needs to talk to a cluster from a
// pre-built *rest.Config (which is what controller-runtime hands us via
// mgr.GetConfig()). The Helm CLI's own getter assumes a kubeconfig file
// and a $KUBECONFIG environment; from inside an operator we already have
// a resolved client config, so the file-loading layers in cli.New() are
// the wrong shape.
//
// This is a well-known pattern in operator code; see e.g.
// kubernetes-sigs operator examples and helm-controller in flux2.
type restClientGetter struct {
	cfg       *rest.Config
	namespace string
}

func newRESTClientGetter(cfg *rest.Config, namespace string) *restClientGetter {
	return &restClientGetter{cfg: cfg, namespace: namespace}
}

// ToRESTConfig returns a copy of the config: Helm's resource builder sets
// GroupVersion and APIPath on the config it gets before building each
// REST client, and every Helm client shares cfg, so concurrent Helm
// operations would otherwise build clients for each other's API groups.
func (g *restClientGetter) ToRESTConfig() (*rest.Config, error) {
	return rest.CopyConfig(g.cfg), nil
}

func (g *restClientGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	dc, err := discovery.NewDiscoveryClientForConfig(g.cfg)
	if err != nil {
		return nil, err
	}
	return memory.NewMemCacheClient(dc), nil
}

func (g *restClientGetter) ToRESTMapper() (meta.RESTMapper, error) {
	dc, err := g.ToDiscoveryClient()
	if err != nil {
		return nil, err
	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(dc)
	return restmapper.NewShortcutExpander(mapper, dc, nil), nil
}

func (g *restClientGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig {
	// Helm uses this only to resolve a default namespace and to decode
	// the kubeconfig for `helm env`/`helm version` flows we don't hit
	// from in-process. A minimal in-memory ClientConfig satisfies the
	// genericclioptions.RESTClientGetter interface without us having to
	// fabricate a kubeconfig file on disk.
	return clientcmd.NewDefaultClientConfig(clientcmdapi.Config{}, &clientcmd.ConfigOverrides{
		Context: clientcmdapi.Context{Namespace: g.namespace},
	})
}

// compile-time assertion that we satisfy the interface.
var _ genericclioptions.RESTClientGetter = (*restClientGetter)(nil)
