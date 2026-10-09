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

package main

import (
	"crypto/tls"
	"flag"
	"os"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	configcontroller "github.com/educates/educates-training-platform/installer/operator/internal/controller/config"
	platformcontroller "github.com/educates/educates-training-platform/installer/operator/internal/controller/platform"
	"github.com/educates/educates-training-platform/installer/operator/internal/crds"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	// +kubebuilder:scaffold:imports
)

// operatorNamespaceEnv is the env var (downward-API populated by the
// chart's Deployment) telling the operator its own namespace. Required
// at runtime: it scopes the Secret cache and is the namespace where
// user-supplied Secrets referenced from EducatesClusterConfig are
// expected to live.
const operatorNamespaceEnv = "OPERATOR_NAMESPACE"

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(configv1alpha1.AddToScheme(scheme))
	utilruntime.Must(platformv1alpha1.AddToScheme(scheme))
	utilruntime.Must(cmv1.AddToScheme(scheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

// nolint:gocyclo
func main() {
	var metricsAddr string
	var metricsCertPath, metricsCertName, metricsCertKey string
	var webhookCertPath, webhookCertName, webhookCertKey string
	var enableLeaderElection bool
	var probeAddr string
	var secureMetrics bool
	var enableHTTP2 bool
	var tlsOpts []func(*tls.Config)
	flag.StringVar(&metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(&metricsCertName, "metrics-cert-name", "tls.crt", "The name of the metrics server certificate file.")
	flag.StringVar(&metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	// Wrap the zap logger with filteringLogSink so controller-runtime's
	// internal/source/kind.go retry-loop ERRORs (emitted whenever a
	// registered Source can no longer resolve its CRD-defined GVK)
	// are demoted to V(1) instead of dominating the log with stack
	// traces. See cmd/logsink.go for the rationale.
	baseLogger := zap.New(zap.UseFlagOptions(&opts))
	ctrl.SetLogger(logr.New(&filteringLogSink{inner: baseLogger.GetSink()}))

	// if the enable-http2 flag is false (the default), http/2 should be disabled
	// due to its vulnerabilities. More specifically, disabling http/2 will
	// prevent from being vulnerable to the HTTP/2 Stream Cancellation and
	// Rapid Reset CVEs. For more information see:
	// - https://github.com/advisories/GHSA-qppj-fm5r-hxr3
	// - https://github.com/advisories/GHSA-4374-p667-p6c8
	disableHTTP2 := func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}

	if !enableHTTP2 {
		tlsOpts = append(tlsOpts, disableHTTP2)
	}

	// Initial webhook TLS options
	webhookTLSOpts := tlsOpts
	webhookServerOptions := webhook.Options{
		TLSOpts: webhookTLSOpts,
	}

	if len(webhookCertPath) > 0 {
		setupLog.Info("Initializing webhook certificate watcher using provided certificates",
			"webhook-cert-path", webhookCertPath, "webhook-cert-name", webhookCertName, "webhook-cert-key", webhookCertKey)

		webhookServerOptions.CertDir = webhookCertPath
		webhookServerOptions.CertName = webhookCertName
		webhookServerOptions.KeyName = webhookCertKey
	}

	webhookServer := webhook.NewServer(webhookServerOptions)

	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	metricsServerOptions := metricsserver.Options{
		BindAddress:   metricsAddr,
		SecureServing: secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.3/pkg/metrics/filters#WithAuthenticationAndAuthorization
		metricsServerOptions.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. While convenient for development and testing,
	// this setup is not recommended for production.
	//
	// TODO(user): If you enable certManager, uncomment the following lines:
	// - [METRICS-WITH-CERTS] at config/default/kustomization.yaml to generate and use certificates
	// managed by cert-manager for the metrics server.
	// - [PROMETHEUS-WITH-CERTS] at config/prometheus/kustomization.yaml for TLS certification.
	if len(metricsCertPath) > 0 {
		setupLog.Info("Initializing metrics certificate watcher using provided certificates",
			"metrics-cert-path", metricsCertPath, "metrics-cert-name", metricsCertName, "metrics-cert-key", metricsCertKey)

		metricsServerOptions.CertDir = metricsCertPath
		metricsServerOptions.CertName = metricsCertName
		metricsServerOptions.KeyName = metricsCertKey
	}

	operatorNamespace := os.Getenv(operatorNamespaceEnv)
	if operatorNamespace == "" {
		setupLog.Error(nil, "Required environment variable is not set", "env", operatorNamespaceEnv)
		os.Exit(1)
	}

	restCfg := ctrl.GetConfigOrDie()

	// SetupSignalHandler can only be called once; share the context
	// between the boot-time Secret-namespace discovery and the main
	// manager.Start below.
	signalCtx := ctrl.SetupSignalHandler()

	// Reconcile the operator's own CRDs to the schemas embedded in this
	// binary before the manager starts. Helm never updates the chart's crds/
	// on `helm upgrade`, so an imperative upgrade would otherwise run new
	// operator code against stale CRDs (new spec fields pruned, new CEL rules
	// absent) while a declarative GitOps sync of the same chart gets the new
	// schema. Applying the embedded CRDs here keeps both install paths
	// identical at CRD-schema changes. A direct (uncached) client is used
	// because the manager's cache is not running yet.
	crdClient, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		setupLog.Error(err, "failed to create client for applying CRDs")
		os.Exit(1)
	}
	if err := crds.Apply(signalCtx, crdClient); err != nil {
		setupLog.Error(err, "failed to apply embedded CRDs")
		os.Exit(1)
	}
	setupLog.Info("Applied embedded CRDs")

	// Scope the Secret cache to: operator namespace, 'educates-secrets'
	// (v3 / CLI laptop convention), plus any cross-namespace refs the
	// current EducatesClusterConfig singleton points at. APIReader still
	// handles ad-hoc reads from elsewhere; the cache scope here only
	// affects watch-driven enqueue.
	//
	// Boot-time discovery: if the user later edits the ECC to point at
	// a new namespace, the operator pod needs to restart to pick up the
	// watch. The reconciler emits a Warning event in that case so it's
	// user-visible. Live re-scoping of the cache mid-process is a
	// follow-up (would require unwinding the manager / using a separate
	// informer pool).
	secretCacheNamespaces, err := discoverCachedSecretNamespaces(signalCtx, restCfg, scheme, operatorNamespace)
	if err != nil {
		setupLog.Error(err, "failed to discover cached Secret namespaces")
		os.Exit(1)
	}
	setupLog.Info("Secret cache scope", "namespaces", secretCacheNamespaces)
	namespaceConfigs := make(map[string]cache.Config, len(secretCacheNamespaces))
	for _, ns := range secretCacheNamespaces {
		namespaceConfigs[ns] = cache.Config{}
	}

	// Scope the Deployment informer to the namespaces the operator actually
	// reads Deployments in (the cluster services plus the platform namespace).
	// A cluster-wide Deployment cache would hold every Deployment — including
	// one-plus per workshop session — and run the watch map functions on
	// every session churn event for nothing. The map functions already filter
	// to these namespaces (see mapDeploymentToSingleton / the platform
	// mappers), so the reads are always in scope.
	deploymentCacheNamespaces := append(
		configcontroller.DeploymentWatchNamespaces(),
		platformcontroller.DeploymentWatchNamespaces()...,
	)
	setupLog.Info("Deployment cache scope", "namespaces", deploymentCacheNamespaces)
	deploymentNamespaceConfigs := make(map[string]cache.Config, len(deploymentCacheNamespaces))
	for _, ns := range deploymentCacheNamespaces {
		deploymentNamespaceConfigs[ns] = cache.Config{}
	}

	cacheOpts := cache.Options{
		ByObject: map[client.Object]cache.ByObject{
			&corev1.Secret{}:     {Namespaces: namespaceConfigs},
			&appsv1.Deployment{}: {Namespaces: deploymentNamespaceConfigs},
			// Nodes are read only to work out whether the cluster can mount
			// package images. A platform with many nodes would otherwise hold
			// every node object in memory, so the cache keeps just the Linux
			// nodes and, of those, only the fields the check reads.
			&corev1.Node{}: {
				Label:     labels.SelectorFromSet(labels.Set{"kubernetes.io/os": "linux"}),
				Transform: trimNodeForCache,
			},
		},
	}

	mgr, err := ctrl.NewManager(restCfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsServerOptions,
		WebhookServer:          webhookServer,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "91bedcac.educates.dev",
		Cache:                  cacheOpts,
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	cachedSecretNSSet := make(map[string]bool, len(secretCacheNamespaces))
	for _, ns := range secretCacheNamespaces {
		cachedSecretNSSet[ns] = true
	}
	if err := (&configcontroller.EducatesClusterConfigReconciler{
		Client:                 mgr.GetClient(),
		APIReader:              mgr.GetAPIReader(),
		Scheme:                 mgr.GetScheme(),
		OperatorNamespace:      operatorNamespace,
		CachedSecretNamespaces: cachedSecretNSSet,
		HelmClientFor: func(ns string) (*helm.Client, error) {
			return helm.NewClient(restCfg, ns)
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "config-educatesclusterconfig")
		os.Exit(1)
	}
	if err := (&platformcontroller.SecretsManagerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		HelmClientFor: func(ns string) (*helm.Client, error) {
			return helm.NewClient(restCfg, ns)
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "platform-secretsmanager")
		os.Exit(1)
	}
	if err := (&platformcontroller.LookupServiceReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		HelmClientFor: func(ns string) (*helm.Client, error) {
			return helm.NewClient(restCfg, ns)
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "platform-lookupservice")
		os.Exit(1)
	}
	if err := (&platformcontroller.SessionManagerReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		HelmClientFor: func(ns string) (*helm.Client, error) {
			return helm.NewClient(restCfg, ns)
		},
	}).SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "platform-sessionmanager")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "Failed to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(signalCtx); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}

// trimNodeForCache keeps only what the image volume check reads from a node,
// so a platform with many nodes does not hold their full objects in memory.
// Everything dropped here is unused by this operator.
func trimNodeForCache(object any) (any, error) {
	node, ok := object.(*corev1.Node)

	if !ok {
		return object, nil
	}

	trimmed := &corev1.Node{}
	trimmed.Name = node.Name
	trimmed.Labels = node.Labels
	trimmed.ResourceVersion = node.ResourceVersion
	trimmed.UID = node.UID
	trimmed.Status.NodeInfo.KubeletVersion = node.Status.NodeInfo.KubeletVersion
	trimmed.Status.NodeInfo.ContainerRuntimeVersion = node.Status.NodeInfo.ContainerRuntimeVersion

	return trimmed, nil
}
