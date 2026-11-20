// Command secrets-manager runs the Educates secrets-manager, which copies
// Secrets between namespaces and injects them into service accounts, as the
// secrets.educates.dev resources ask. Outside a cluster it uses the current
// kubeconfig context.
package main

import (
	"fmt"
	"os"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/educates/educates-training-platform/secrets-manager/internal/operator"
)

func main() {
	log := zap.New()
	ctrl.SetLogger(log)

	if err := run(); err != nil {
		log.WithName("setup").Error(err, "The secrets-manager failed")
		os.Exit(1)
	}
}

func run() error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("loading the Kubernetes client configuration: %w", err)
	}
	ctx := ctrl.SetupSignalHandler()
	secretsManager, err := operator.New(ctx, operator.Options{RESTConfig: restConfig, Logger: ctrl.Log})
	if err != nil {
		return err
	}
	return secretsManager.Run(ctx)
}
