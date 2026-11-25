// Command secrets-manager runs the Educates secrets-manager, which copies
// Secrets between namespaces and injects them into service accounts, as the
// secrets.educates.dev resources ask. Outside a cluster it uses the current
// kubeconfig context.
//
// With leader election, on by default, it reconciles only while it holds the
// Lease secrets-manager in its pod's namespace; outside a cluster, run it
// with --leader-elect=false.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/educates/educates-training-platform/secrets-manager/internal/operator"
)

func main() {
	var probeAddress, logLevel string
	var leaderElection bool
	flag.StringVar(&probeAddress, "health-probe-bind-address", ":8081",
		"The address the liveness probe, /healthz, and the readiness probe, /readyz, bind to.")
	flag.StringVar(&logLevel, "log-level", "info", "The log level: debug, info, warn or error.")
	flag.BoolVar(&leaderElection, "leader-elect", true,
		"Reconcile only while holding the Lease "+operator.LeaseName+" in the pod's namespace, "+
			"so that of several replicas only one writes.")
	flag.Parse()

	level, err := parseLogLevel(logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	log := zap.New(zap.Level(level))
	ctrl.SetLogger(log)

	if err := run(probeAddress, leaderElection); err != nil {
		log.WithName("setup").Error(err, "The secrets-manager failed")
		os.Exit(1)
	}
}

func run(probeAddress string, leaderElection bool) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("loading the Kubernetes client configuration: %w", err)
	}
	probeListener, err := net.Listen("tcp", probeAddress)
	if err != nil {
		return err
	}

	ctx := ctrl.SetupSignalHandler()
	secretsManager, err := operator.New(ctx, operator.Options{
		RESTConfig:     restConfig,
		Logger:         ctrl.Log,
		ProbeListener:  probeListener,
		LeaderElection: leaderElection,
	})
	if err != nil {
		return err
	}
	return secretsManager.Run(ctx)
}

// parseLogLevel maps the log levels of the SecretsManager resource onto
// zap's. logr has no warning level, so at warn only errors are logged.
func parseLogLevel(level string) (zapcore.Level, error) {
	switch level {
	case "debug":
		return zapcore.DebugLevel, nil
	case "info":
		return zapcore.InfoLevel, nil
	case "warn":
		return zapcore.WarnLevel, nil
	case "error":
		return zapcore.ErrorLevel, nil
	}
	return 0, fmt.Errorf("unknown log level %q: use debug, info, warn or error", level)
}
