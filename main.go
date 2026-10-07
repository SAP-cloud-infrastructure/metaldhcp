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
	"github.com/coredhcp/coredhcp/plugins/netmask"
	"github.com/coredhcp/coredhcp/plugins/router"
	"github.com/coredhcp/coredhcp/plugins/serverid"
	"github.com/coredhcp/coredhcp/server"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/util/sets"

	"github.com/SAP-cloud-infrastructure/metaldhcp/internal/kubernetes"
	"github.com/SAP-cloud-infrastructure/metaldhcp/plugins/oob"
)

var desiredPlugins = []*plugins.Plugin{
	&dns.Plugin,
	&leasetime.Plugin,
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
