/*
Copyright 2022 The Kruise Authors.

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
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync/atomic"
	"time"

	ackv1alpha1 "github.com/aws-controllers-k8s/elbv2-controller/apis/v1alpha1"
	"github.com/go-logr/logr"
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/rest"
	elbv2api "sigs.k8s.io/aws-load-balancer-controller/apis/elbv2/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	"github.com/openkruise/kruise-game/cloudprovider"
	aliv1beta1 "github.com/openkruise/kruise-game/cloudprovider/alibabacloud/apis/v1beta1"
	cpmanager "github.com/openkruise/kruise-game/cloudprovider/manager"
	kruisegameclientset "github.com/openkruise/kruise-game/pkg/client/clientset/versioned"
	kruisegamevisions "github.com/openkruise/kruise-game/pkg/client/informers/externalversions"
	controller "github.com/openkruise/kruise-game/pkg/controllers"
	"github.com/openkruise/kruise-game/pkg/externalscaler"
	"github.com/openkruise/kruise-game/pkg/logging"
	"github.com/openkruise/kruise-game/pkg/metrics"
	"github.com/openkruise/kruise-game/pkg/tracing"
	"github.com/openkruise/kruise-game/pkg/util/client"
	"github.com/openkruise/kruise-game/pkg/webhook"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	ctrlwebhook "sigs.k8s.io/controller-runtime/pkg/webhook"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")

	apiServerSustainedQPSFlag, apiServerBurstQPSFlag int
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(gamekruiseiov1alpha1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1beta1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1alpha1.AddToScheme(scheme))

	utilruntime.Must(aliv1beta1.AddToScheme(scheme))

	utilruntime.Must(ackv1alpha1.AddToScheme(scheme))
	utilruntime.Must(elbv2api.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var namespace string
	var syncPeriodStr string
	var scaleServerAddr string
	logOptions := logging.NewOptions()
	logOptions.AddFlags(flag.CommandLine)
	tracingOptions := tracing.NewOptions()
	tracingOptions.AddFlags(flag.CommandLine)
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&namespace, "namespace", "",
		"Namespace if specified restricts the manager's cache to watch objects in the desired namespace. Defaults to all namespaces.")
	flag.StringVar(&syncPeriodStr, "sync-period", "", "Determines the minimum frequency at which watched resources are reconciled.")
	flag.StringVar(&scaleServerAddr, "scale-server-bind-address", ":6000", "The address the scale server endpoint binds to.")
	flag.IntVar(&apiServerSustainedQPSFlag, "api-server-qps", 0, "Maximum sustained queries per second to send to the API server")
	flag.IntVar(&apiServerBurstQPSFlag, "api-server-qps-burst", 0, "Maximum burst queries per second to send to the API server")

	// Add cloud provider flags
	cloudprovider.InitCloudProviderFlags()

	flag.Parse()

	logResult, err := logOptions.Apply(flag.CommandLine)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	setupLog = ctrl.Log.WithName("setup")
	if logResult.Warning != "" {
		setupLog.Info(logResult.Warning)
	}

	// Initialize tracing (non-blocking - falls back to no-op on failure)
	if err := tracingOptions.Apply(); err != nil {
		setupLog.Info("Tracing initialization failed, using no-op tracer", tracing.FieldError, err.Error())
	} else if tracingOptions.Enabled {
		setupLog.Info("Tracing initialized successfully", tracing.FieldCollector, tracingOptions.CollectorEndpoint)

		// Send a hello-world trace to verify the pipeline
		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			tracer := otel.Tracer("okg-controller-manager")
			_, span := tracer.Start(ctx, "controller-startup-test")
			span.SetAttributes(
				attribute.String("test.type", "smoke-test"),
				attribute.String("test.purpose", "verify-tracing-pipeline"),
			)
			setupLog.Info("Sent hello-world trace span", tracing.FieldSpanName, "controller-startup-test")
			span.End()

			// Give exporter time to send the span
			time.Sleep(2 * time.Second)
		}()

		// Register shutdown hook
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := tracing.Shutdown(ctx); err != nil {
				setupLog.Error(err, "failed to shutdown tracer")
			}
		}()
	}

	// syncPeriod parsed
	var syncPeriod *time.Duration
	if syncPeriodStr != "" {
		d, err := time.ParseDuration(syncPeriodStr)
		if err != nil {
			setupLog.Error(err, "invalid sync period flag")
		} else {
			syncPeriod = &d
		}
	}

	restConfig := ctrl.GetConfigOrDie()
	setRestConfig(restConfig)
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "game-kruise-manager",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		LeaderElectionReleaseOnCancel: true,

		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
		Cache: cache.Options{
			SyncPeriod:        syncPeriod,
			DefaultNamespaces: getCacheNamespacesFromFlag(namespace),
		},
		WebhookServer: webhook.NewServer(ctrlwebhook.Options{
			Host:    "0.0.0.0",
			Port:    webhook.GetPort(),
			CertDir: webhook.GetCertDir(),
		}, enableLeaderElection),
		NewCache: client.NewCache,
	})
	if err != nil {
		setupLog.Error(err, "unable to start kruise-game-manager")
		os.Exit(1)
	}

	cloudProviderManager, err := cpmanager.NewProviderManager()
	if err != nil {
		setupLog.Error(err, "unable to set up cloud provider manager")
		os.Exit(1)
	}

	// create webhook server
	wss := webhook.NewWebhookServer(mgr, cloudProviderManager)
	// validate webhook server
	if err := wss.SetupWithManager(mgr).Initialize(mgr.GetConfig()); err != nil {
		setupLog.Error(err, "unable to set up webhook server")
		os.Exit(1)
	}

	//+kubebuilder:scaffold:builder
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}

	// Add a readiness check that confirms the manager is ready to serve traffic if leader election is enabled.
	var (
		isLeader                  atomic.Bool
		isCloudManagerInitialized atomic.Bool
	)
	if enableLeaderElection {
		if err := mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
			<-mgr.Elected()
			isLeader.Store(true)
			<-ctx.Done()
			return nil
		})); err != nil {
			setupLog.Error(err, "unable to add leader check runnable")
			os.Exit(1)
		}
	}

	if err := mgr.AddReadyzCheck("readyz", func(req *http.Request) error {
		if !enableLeaderElection {
			// If leader election is not enabled, we can always return ready
			return healthz.Ping(req)
		}
		if isLeader.Load() && isCloudManagerInitialized.Load() {
			// If the manager is the leader and initialized, we can return nil to indicate readiness
			return nil
		}
		return fmt.Errorf("not ready yet")
	}); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("setup controllers", tracing.FieldEvent, "controller.setup")
	if err = controller.SetupWithManager(mgr); err != nil {
		setupLog.Error(err, "unable to setup controllers")
		os.Exit(1)
	}

	signal := ctrl.SetupSignalHandler()
	go func() {
		setupLog.Info("waiting for cache sync to init cloud provider manager", tracing.FieldEvent, "cache.wait_for_sync")
		if mgr.GetCache().WaitForCacheSync(signal) {
			setupLog.Info("cache synced, cloud provider manager start to init", tracing.FieldEvent, "cache.synced")
			if enableLeaderElection {
				setupLog.Info("waiting for leader election to init cloud provider manager", tracing.FieldEvent, "leader_election.wait_for_leadership")
				<-mgr.Elected()
				setupLog.Info("leader election completed, initializing cloud provider manager", tracing.FieldEvent, "leader_election.completed")
			}
			setupLog.Info("initializing cloud provider manager", tracing.FieldEvent, "cloud_provider_manager.init_start")
			cloudProviderManager.Init(mgr.GetClient())
			setupLog.Info("cloud provider manager initialized successfully", tracing.FieldEvent, "cloud_provider_manager.init_completed")
			isCloudManagerInitialized.Store(true)
		}
	}()

	kruisegameInformerFactory := kruisegamevisions.NewSharedInformerFactory(kruisegameclientset.NewForConfigOrDie(restConfig), 30*time.Second)
	metricsController, err := metrics.NewController(kruisegameInformerFactory)
	if err != nil {
		setupLog.Error(err, "unable to create metrics controller")
		os.Exit(1)
	}
	kruisegameInformerFactory.Start(signal.Done())
	go func() {
		if metricsController.Run(signal) != nil {
			setupLog.Error(err, "unable to setup metrics controller")
			os.Exit(1)
		}
	}()

	externalScaler := externalscaler.NewExternalScaler(mgr.GetClient())
	go func() {
		grpcServer := grpc.NewServer()
		lis, _ := net.Listen("tcp", scaleServerAddr)
		externalscaler.RegisterExternalScalerServer(grpcServer, externalScaler)
		if err := grpcServer.Serve(lis); err != nil {
			setupLog.Error(err, "unable to setup ExternalScalerServer")
			os.Exit(1)
		}
	}()

	logServiceReadySummary(setupLog, serviceSummary{
		MetricsAddr:     metricsAddr,
		HealthAddr:      probeAddr,
		Namespace:       namespace,
		SyncPeriodRaw:   syncPeriodStr,
		LeaderElection:  enableLeaderElection,
		LogFormat:       logResult.Format,
		LogJSONPreset:   logResult.JSONPreset,
		ScaleServerAddr: scaleServerAddr,
	})

	setupLog.Info("starting kruise-game-manager", tracing.FieldEvent, "service.start")

	if err := mgr.Start(signal); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}

func setRestConfig(c *rest.Config) {
	if apiServerSustainedQPSFlag > 0 {
		c.QPS = float32(apiServerSustainedQPSFlag)
	}
	if apiServerBurstQPSFlag > 0 {
		c.Burst = apiServerBurstQPSFlag
	}
}

func getCacheNamespacesFromFlag(ns string) map[string]cache.Config {
	if ns == "" {
		return nil
	}
	return map[string]cache.Config{
		ns: {},
	}
}

type serviceSummary struct {
	MetricsAddr     string
	HealthAddr      string
	Namespace       string
	SyncPeriodRaw   string
	LeaderElection  bool
	LogFormat       string
	LogJSONPreset   logging.JSONPreset
	ScaleServerAddr string
}

func logServiceReadySummary(logger logr.Logger, summary serviceSummary) {
	fields := []interface{}{
		tracing.FieldEvent, "service.ready",
		"leader_election", summary.LeaderElection,
	}
	if summary.MetricsAddr != "" {
		fields = append(fields, "metrics.bind_address", summary.MetricsAddr)
	}
	if summary.HealthAddr != "" {
		fields = append(fields, "healthz.bind_address", summary.HealthAddr)
	}
	if summary.Namespace != "" {
		fields = append(fields, "namespace_scope", summary.Namespace)
	}
	if summary.SyncPeriodRaw != "" {
		fields = append(fields, "sync_period", summary.SyncPeriodRaw)
	}
	if summary.LogFormat != "" {
		fields = append(fields, "log.format", summary.LogFormat)
	}
	if summary.LogJSONPreset != "" {
		fields = append(fields, "log.json_preset", string(summary.LogJSONPreset))
	}
	if summary.ScaleServerAddr != "" {
		fields = append(fields, "scale_server.bind_address", summary.ScaleServerAddr)
	}
	logger.Info("service configuration snapshot", fields...)
}
