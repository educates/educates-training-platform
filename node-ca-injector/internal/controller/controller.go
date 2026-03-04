package controller

import (
	"context"
	"encoding/json"
	"os"
	"sort"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	configMapName      = "educates-registry-hosts"
	registryLabel      = "training.educates.dev/application"
	registryLabelValue = "registry"
)

type RegistryHostsReconciler struct {
	client.Client
	OperatorNamespace string
}

// Reconcile is called for every Ingress event. It lists all registry Ingresses
// and rebuilds the ConfigMap with the full set of hosts.
func (r *RegistryHostsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	selector, err := labels.Parse(registryLabel + "=" + registryLabelValue)
	if err != nil {
		return ctrl.Result{}, err
	}

	var ingressList networkingv1.IngressList
	if err := r.List(ctx, &ingressList, &client.ListOptions{
		LabelSelector: selector,
	}); err != nil {
		logger.Error(err, "failed to list registry ingresses")
		return ctrl.Result{}, err
	}

	hosts := make([]string, 0)
	seen := make(map[string]bool)
	for _, ing := range ingressList.Items {
		for _, rule := range ing.Spec.Rules {
			if rule.Host != "" && !seen[rule.Host] {
				hosts = append(hosts, rule.Host)
				seen[rule.Host] = true
			}
		}
	}
	sort.Strings(hosts)

	hostsJSON, err := json.Marshal(hosts)
	if err != nil {
		return ctrl.Result{}, err
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: r.OperatorNamespace,
		},
	}

	existing := &corev1.ConfigMap{}
	err = r.Get(ctx, client.ObjectKeyFromObject(cm), existing)
	if client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, err
	}

	if err != nil {
		// ConfigMap not found, create it
		cm.Data = map[string]string{"hosts.json": string(hostsJSON)}
		logger.Info("creating registry hosts configmap", "hosts", len(hosts))
		return ctrl.Result{}, r.Create(ctx, cm)
	}

	// ConfigMap exists, update if changed
	if existing.Data["hosts.json"] != string(hostsJSON) {
		existing.Data = map[string]string{"hosts.json": string(hostsJSON)}
		logger.Info("updating registry hosts configmap", "hosts", len(hosts))
		return ctrl.Result{}, r.Update(ctx, existing)
	}

	return ctrl.Result{}, nil
}

func (r *RegistryHostsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1.Ingress{}).
		Complete(r)
}

func Run() error {
	log.SetLogger(zap.New())
	logger := log.Log.WithName("node-ca-injector-controller")

	namespace := os.Getenv("OPERATOR_NAMESPACE")
	if namespace == "" {
		namespace = "educates"
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Cache: cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				&corev1.ConfigMap{}: {
					Namespaces: map[string]cache.Config{
						namespace: {},
					},
				},
			},
		},
	})
	if err != nil {
		logger.Error(err, "unable to create manager")
		return err
	}

	reconciler := &RegistryHostsReconciler{
		Client:            mgr.GetClient(),
		OperatorNamespace: namespace,
	}

	if err := reconciler.SetupWithManager(mgr); err != nil {
		logger.Error(err, "unable to setup controller")
		return err
	}

	logger.Info("starting controller", "namespace", namespace)
	return mgr.Start(ctrl.SetupSignalHandler())
}
