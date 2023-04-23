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
	"flag"
	kruiseV1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	kruiseV1beta1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/openkruise/kruise-game/cloudprovider"
	cpmanager "github.com/openkruise/kruise-game/cloudprovider/manager"
	kruisegameclientset "github.com/openkruise/kruise-game/pkg/client/clientset/versioned"
	kruisegamevisions "github.com/openkruise/kruise-game/pkg/client/informers/externalversions"
	controller "github.com/openkruise/kruise-game/pkg/controllers"
	"github.com/openkruise/kruise-game/pkg/metrics"
	"github.com/openkruise/kruise-game/pkg/webhook"
	"os"
	"time"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	aliv1 "github.com/openkruise/kruise-game/cloudprovider/alibabacloud/apis/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	gamekruiseiov1alpha1 "github.com/openkruise/kruise-game/apis/v1alpha1"
	utilclient "github.com/openkruise/kruise-game/pkg/util/client"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(gamekruiseiov1alpha1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1beta1.AddToScheme(scheme))
	utilruntime.Must(kruiseV1alpha1.AddToScheme(scheme))

	utilruntime.Must(aliv1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var namespace string
	var syncPeriodStr string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8082", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&namespace, "namespace", "",
		"Namespace if specified restricts the manager's cache to watch objects in the desired namespace. Defaults to all namespaces.")
	flag.StringVar(&syncPeriodStr, "sync-period", "", "Determines the minimum frequency at which watched resources are reconciled.")

	// Add cloud provider flags
	cloudprovider.InitCloudProviderFlags()

	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

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
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     metricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "game-kruise-manager",
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
		Namespace:  namespace,
		SyncPeriod: syncPeriod,
		NewClient:  utilclient.NewClient,
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
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	signal := ctrl.SetupSignalHandler()
	go func() {
		setupLog.Info("setup controllers")
		if err = controller.SetupWithManager(mgr); err != nil {
			setupLog.Error(err, "unable to setup controllers")
			os.Exit(1)
		}

		setupLog.Info("waiting for cache sync")
		if mgr.GetCache().WaitForCacheSync(signal) {
			setupLog.Info("cache synced, cloud provider manager start to init")
			cloudProviderManager.Init(mgr.GetClient())
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

	setupLog.Info("starting kruise-game-manager")

	if err := mgr.Start(signal); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
