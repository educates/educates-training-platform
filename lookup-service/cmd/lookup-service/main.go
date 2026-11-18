// Command lookup-service runs the Educates Lookup Service.
//
// It reads its configuration, the lookup.educates.dev resources, from the
// namespace OPERATOR_NAMESPACE names, educates-config by default, and keeps
// its token Secret in its own namespace, which POD_NAMESPACE names or, in a
// pod, its service account's. Outside a cluster it uses the current
// kubeconfig context.
package main

import (
	"flag"
	"fmt"
	"net"
	"os"

	"go.uber.org/zap/zapcore"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/educates/educates-training-platform/lookup-service/internal/service"
)

func main() {
	var apiAddress, probeAddress, logLevel string
	flag.StringVar(&apiAddress, "api-bind-address", ":8080", "The address the REST API binds to.")
	flag.StringVar(&probeAddress, "health-probe-bind-address", ":8081",
		"The address the liveness probe, /healthz, and the readiness probe, /readyz, bind to.")
	flag.StringVar(&logLevel, "log-level", "info", "The log level: debug, info, warn or error.")
	flag.Parse()

	level, err := parseLogLevel(logLevel)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	log := zap.New(zap.Level(level))
	ctrl.SetLogger(log)
	setupLog := log.WithName("setup")

	if err := run(apiAddress, probeAddress); err != nil {
		setupLog.Error(err, "The Lookup Service failed")
		os.Exit(1)
	}
}

func run(apiAddress, probeAddress string) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("loading the Kubernetes client configuration: %w", err)
	}
	apiListener, err := net.Listen("tcp", apiAddress)
	if err != nil {
		return err
	}
	probeListener, err := net.Listen("tcp", probeAddress)
	if err != nil {
		return err
	}

	ctx := ctrl.SetupSignalHandler()
	svc, err := service.New(ctx, service.Options{
		RESTConfig:    restConfig,
		APIListener:   apiListener,
		ProbeListener: probeListener,
		Logger:        ctrl.Log,
	})
	if err != nil {
		return err
	}
	return svc.Run(ctx)
}

// parseLogLevel maps the log levels of the LookupService resource onto zap's.
// logr has no warning level, so at warn only errors are logged.
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
