// SPDX-FileCopyrightText: SAP SE or an SAP affiliate company and IronCore contributors
// SPDX-License-Identifier: MIT

package main

import (
	"flag"
	"fmt"
	"net/http"
	"os"

	"github.com/coredhcp/coredhcp/config"
	"github.com/coredhcp/coredhcp/logger"
	"github.com/coredhcp/coredhcp/plugins"
	"github.com/coredhcp/coredhcp/plugins/dns"
	"github.com/coredhcp/coredhcp/plugins/leasetime"
	"github.com/coredhcp/coredhcp/plugins/nbp"
	"github.com/coredhcp/coredhcp/plugins/netmask"
	"github.com/coredhcp/coredhcp/plugins/router"
	"github.com/coredhcp/coredhcp/plugins/serverid"
	"github.com/coredhcp/coredhcp/server"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	k8sclientset "k8s.io/client-go/kubernetes"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/kubernetes"
	"github.com/SAP-cloud-infrastructure/metaldhcp/plugins/oob"
)

var desiredPlugins = []*plugins.Plugin{
	&dns.Plugin,
	&leasetime.Plugin,
	&nbp.Plugin,
	&netmask.Plugin,
	&router.Plugin,
	&serverid.Plugin,
	&oob.Plugin,
}

var (
	log                        = logger.GetLogger("main")
	pluginsRequiringKubernetes = sets.New[string]("oob")
)

func main() {
	var configFile, logLevel, healthAddr string
	var listPlugins bool

	flag.StringVar(&configFile, "config", "", "config file")
	flag.BoolVar(&listPlugins, "list-plugins", false, "list plugins")
	flag.StringVar(&logLevel, "loglevel", "info", "log level (debug, info, warning, error, fatal, panic)")
	flag.StringVar(&healthAddr, "health-addr", ":8080", "address for the /healthz HTTP endpoint")
	flag.Parse()

	if listPlugins {
		for _, p := range desiredPlugins {
			fmt.Println(p.Name)
		}
		os.Exit(0)
	}

	level, err := logrus.ParseLevel(logLevel)
	if err != nil {
		fmt.Println("Invalid log level specified: ", err)
		os.Exit(1)
	}
	log.Logger.SetLevel(level)

	cfg, err := config.Load(configFile)
	if err != nil {
		log.Fatalf("Failed to load configuration %s: %v", configFile, err)
	}

	for _, plugin := range desiredPlugins {
		if err := plugins.RegisterPlugin(plugin); err != nil {
			log.Fatalf("Failed to register plugin '%s': %v", plugin.Name, err)
		}
	}

	if shouldSetupKubeClient(cfg) {
		if err := kubernetes.InitClient(); err != nil {
			log.Fatalf("Failed to initialize kubernetes client: %v", err)
		}
		setupEventRecorder()
	}

	go func() {
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		if err := http.ListenAndServe(healthAddr, mux); err != nil {
			log.Errorf("Health server error: %v", err)
		}
	}()

	srv, err := server.Start(cfg)
	if err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
	if err := srv.Wait(); err != nil {
		log.Fatalf("Failed to wait server: %v", err)
	}
}

func shouldSetupKubeClient(cfg *config.Config) bool {
	configuredPlugins := sets.Set[string]{}
	if cfg.Server4 != nil {
		for _, plugin := range cfg.Server4.Plugins {
			configuredPlugins.Insert(plugin.Name)
		}
	}
	if cfg.Server6 != nil {
		for _, plugin := range cfg.Server6.Plugins {
			configuredPlugins.Insert(plugin.Name)
		}
	}
	return configuredPlugins.HasAny(pluginsRequiringKubernetes.UnsortedList()...)
}

// setupEventRecorder creates a Kubernetes event recorder backed by the live API and wires
// it into the oob plugin. POD_NAME and POD_NAMESPACE must be set via the Downward API.
func setupEventRecorder() {
	podName := os.Getenv("POD_NAME")
	podNamespace := os.Getenv("POD_NAMESPACE")
	if podName == "" || podNamespace == "" {
		log.Warning("POD_NAME or POD_NAMESPACE unset — Kubernetes events will not be emitted")
		return
	}

	cs, err := k8sclientset.NewForConfig(kubernetes.GetConfig())
	if err != nil {
		log.Warningf("Failed to create k8s clientset for event recorder: %v", err)
		return
	}

	broadcaster := record.NewBroadcaster()
	broadcaster.StartRecordingToSink(&typedcorev1.EventSinkImpl{Interface: cs.CoreV1().Events(podNamespace)})

	recorder := broadcaster.NewRecorder(
		k8sscheme.Scheme,
		corev1.EventSource{Component: "metaldhcp"},
	)

	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: podName, Namespace: podNamespace}}
	oob.SetRecorder(recorder, pod)
}
