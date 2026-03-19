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

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	hermesv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/api/v1alpha1"
	"github.com/xmbshwll/k8s-operator-hermes-agent/internal/controller"
	webhookv1alpha1 "github.com/xmbshwll/k8s-operator-hermes-agent/internal/webhook/v1alpha1"
	// +kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

type managerConfig struct {
	metricsAddr          string
	metricsCertPath      string
	metricsCertName      string
	metricsCertKey       string
	webhookCertPath      string
	webhookCertName      string
	webhookCertKey       string
	enableLeaderElection bool
	probeAddr            string
	secureMetrics        bool
	enableHTTP2          bool
	zapOptions           zap.Options
}

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(hermesv1alpha1.AddToScheme(scheme))
	// +kubebuilder:scaffold:scheme
}

func main() {
	cfg := parseManagerConfig()
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&cfg.zapOptions)))

	tlsOpts := buildTLSOptions(cfg.enableHTTP2)
	enableWebhooks := webhooksEnabled()
	webhookServer := newWebhookServer(cfg, tlsOpts, enableWebhooks)
	metricsOptions := newMetricsServerOptions(cfg, tlsOpts)
	managerOptions := newManagerOptions(cfg, metricsOptions, webhookServer, enableWebhooks)

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), managerOptions)
	if err != nil {
		setupLog.Error(err, "Failed to start manager")
		os.Exit(1)
	}

	if err := setupController(mgr); err != nil {
		setupLog.Error(err, "Failed to create controller", "controller", "HermesAgent")
		os.Exit(1)
	}
	if err := setupWebhooks(mgr, enableWebhooks); err != nil {
		setupLog.Error(err, "Failed to create webhook", "webhook", "HermesAgent")
		os.Exit(1)
	}
	// +kubebuilder:scaffold:builder

	if err := addHealthChecks(mgr); err != nil {
		setupLog.Error(err, "Failed to set up health checks")
		os.Exit(1)
	}

	setupLog.Info("Starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "Failed to run manager")
		os.Exit(1)
	}
}

func parseManagerConfig() managerConfig {
	cfg := managerConfig{
		zapOptions: zap.Options{Development: true},
	}

	flag.StringVar(&cfg.metricsAddr, "metrics-bind-address", "0", "The address the metrics endpoint binds to. "+
		"Use :8443 for HTTPS or :8080 for HTTP, or leave as 0 to disable the metrics service.")
	flag.StringVar(&cfg.probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&cfg.enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.BoolVar(&cfg.secureMetrics, "metrics-secure", true,
		"If set, the metrics endpoint is served securely via HTTPS. Use --metrics-secure=false to use HTTP instead.")
	flag.StringVar(&cfg.webhookCertPath, "webhook-cert-path", "", "The directory that contains the webhook certificate.")
	flag.StringVar(&cfg.webhookCertName, "webhook-cert-name", "tls.crt", "The name of the webhook certificate file.")
	flag.StringVar(&cfg.webhookCertKey, "webhook-cert-key", "tls.key", "The name of the webhook key file.")
	flag.StringVar(&cfg.metricsCertPath, "metrics-cert-path", "",
		"The directory that contains the metrics server certificate.")
	flag.StringVar(
		&cfg.metricsCertName,
		"metrics-cert-name",
		"tls.crt",
		"The name of the metrics server certificate file.",
	)
	flag.StringVar(&cfg.metricsCertKey, "metrics-cert-key", "tls.key", "The name of the metrics server key file.")
	flag.BoolVar(&cfg.enableHTTP2, "enable-http2", false,
		"If set, HTTP/2 will be enabled for the metrics and webhook servers")
	cfg.zapOptions.BindFlags(flag.CommandLine)
	flag.Parse()

	return cfg
}

func buildTLSOptions(enableHTTP2 bool) []func(*tls.Config) {
	if enableHTTP2 {
		return nil
	}

	return []func(*tls.Config){func(c *tls.Config) {
		setupLog.Info("Disabling HTTP/2")
		c.NextProtos = []string{"http/1.1"}
	}}
}

func webhooksEnabled() bool {
	return os.Getenv("ENABLE_WEBHOOKS") != "false"
}

func newWebhookServer(cfg managerConfig, tlsOpts []func(*tls.Config), enabled bool) webhook.Server {
	if !enabled {
		setupLog.Info("Admission webhooks are disabled")
		return nil
	}

	options := webhook.Options{TLSOpts: tlsOpts}
	if cfg.webhookCertPath == "" {
		return webhook.NewServer(options)
	}

	setupLog.Info("Initializing webhook certificate watcher using provided certificates",
		"webhook-cert-path", cfg.webhookCertPath,
		"webhook-cert-name", cfg.webhookCertName,
		"webhook-cert-key", cfg.webhookCertKey,
	)
	options.CertDir = cfg.webhookCertPath
	options.CertName = cfg.webhookCertName
	options.KeyName = cfg.webhookCertKey
	return webhook.NewServer(options)
}

func newMetricsServerOptions(cfg managerConfig, tlsOpts []func(*tls.Config)) metricsserver.Options {
	// Metrics endpoint is enabled in 'config/default/kustomization.yaml'. The Metrics options configure the server.
	// More info:
	// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/metrics/server
	// - https://book.kubebuilder.io/reference/metrics.html
	options := metricsserver.Options{
		BindAddress:   cfg.metricsAddr,
		SecureServing: cfg.secureMetrics,
		TLSOpts:       tlsOpts,
	}

	if cfg.secureMetrics {
		// FilterProvider is used to protect the metrics endpoint with authn/authz.
		// These configurations ensure that only authorized users and service accounts
		// can access the metrics endpoint. The RBAC are configured in 'config/rbac/kustomization.yaml'. More info:
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.23.1/pkg/metrics/filters#WithAuthenticationAndAuthorization
		options.FilterProvider = filters.WithAuthenticationAndAuthorization
	}

	// If the certificate is not specified, controller-runtime will automatically
	// generate self-signed certificates for the metrics server. That is convenient
	// for development and tests, but production installs should use cert-manager-
	// managed metrics certificates instead.
	//
	// For the repository kustomize flow, enable:
	// - [METRICS-WITH-CERTS] in config/default/kustomization.yaml
	// - [PROMETHEUS-WITH-CERTS] in config/prometheus/kustomization.yaml
	if cfg.metricsCertPath == "" {
		return options
	}

	setupLog.Info("Initializing metrics certificate watcher using provided certificates",
		"metrics-cert-path", cfg.metricsCertPath,
		"metrics-cert-name", cfg.metricsCertName,
		"metrics-cert-key", cfg.metricsCertKey,
	)
	options.CertDir = cfg.metricsCertPath
	options.CertName = cfg.metricsCertName
	options.KeyName = cfg.metricsCertKey
	return options
}

func newManagerOptions(
	cfg managerConfig,
	metricsOptions metricsserver.Options,
	webhookServer webhook.Server,
	enableWebhooks bool,
) ctrl.Options {
	options := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsOptions,
		HealthProbeBindAddress: cfg.probeAddr,
		LeaderElection:         cfg.enableLeaderElection,
		LeaderElectionID:       "73bc4e30.nous.ai",
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
	}
	if enableWebhooks {
		options.WebhookServer = webhookServer
	}
	return options
}

func setupController(mgr ctrl.Manager) error {
	return (&controller.HermesAgentReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		//nolint:staticcheck // controller-runtime still exposes the legacy recorder shape our reconciler uses
		Recorder: mgr.GetEventRecorderFor("hermesagent-controller"),
	}).SetupWithManager(mgr)
}

func setupWebhooks(mgr ctrl.Manager, enableWebhooks bool) error {
	if !enableWebhooks {
		return nil
	}
	return webhookv1alpha1.SetupHermesAgentWebhookWithManager(mgr)
}

func addHealthChecks(mgr ctrl.Manager) error {
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	return mgr.AddReadyzCheck("readyz", healthz.Ping)
}
