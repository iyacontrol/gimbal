package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sirupsen/logrus"

	"github.com/projectcontour/gimbal/pkg/buildinfo"
	"github.com/projectcontour/gimbal/pkg/consul"
	"github.com/projectcontour/gimbal/pkg/k8s"
	localmetrics "github.com/projectcontour/gimbal/pkg/metrics"
	"github.com/projectcontour/gimbal/pkg/signals"
	"github.com/projectcontour/gimbal/pkg/util"
)

var (
	printVersion            bool
	gimbalKubeCfgFile       string
	gimbaKubeNamespace      string
	discovererConsulCfgFile string
	backendName             string
	debug                   bool
	reconciliationPeriod    time.Duration
	numProcessThreads       int
	prometheusListenPort    int
	discovererMetrics       localmetrics.DiscovererMetrics
	discoverererTagFilter   string
	gimbalKubeClientQPS     float64
	gimbalKubeClientBurst   int
)

func init() {
	flag.BoolVar(&printVersion, "version", false, "Show version and quit")
	flag.StringVar(&gimbalKubeCfgFile, "gimbal-kubecfg-file", "", "Location of kubecfg file for access to gimbal system kubernetes api, defaults to service account tokens")
	flag.StringVar(&gimbaKubeNamespace, "gimbal-kube-namespace", "consul", "consul discoverer will deal with svc and ep in this namespace, default is consul")
	flag.StringVar(&discovererConsulCfgFile, "discover-consulcfg-file", "", "Location of consulcfg file for access to remote discover consul api")
	flag.StringVar(&backendName, "backend-name", "", "Name of backend (must be unique)")
	flag.BoolVar(&debug, "debug", false, "Enable debug logging.")
	flag.StringVar(&discoverererTagFilter, "discoverer-consul-tag-filter", "gateway", "The tag filter is used get some services which tag contains tag filetr from consul")
	flag.DurationVar(&reconciliationPeriod, "reconciliation-period", 30*time.Second, "The interval of time between reconciliation loop runs.")
	flag.IntVar(&numProcessThreads, "num-threads", 2, "Specify number of threads to use when processing queue items.")
	flag.IntVar(&prometheusListenPort, "prometheus-listen-address", 8080, "The address to listen on for Prometheus HTTP requests")
	flag.Float64Var(&gimbalKubeClientQPS, "gimbal-client-qps", 5, "The maximum queries per second (QPS) that can be performed on the Gimbal Kubernetes API server")
	flag.IntVar(&gimbalKubeClientBurst, "gimbal-client-burst", 10, "The maximum number of queries that can be performed on the Gimbal Kubernetes API server during a burst")
	flag.Parse()
}
func main() {
	var log = logrus.New()
	log.Formatter = util.GetFormatter()

	if printVersion {
		fmt.Println("consul-discoverer")
		fmt.Printf("Version: %s\n", buildinfo.Version)
		fmt.Printf("Git commit: %s\n", buildinfo.GitSHA)
		fmt.Printf("Git tree state: %s\n", buildinfo.GitTreeState)
		os.Exit(0)
	}

	log.Info("Gimbal Consul Discoverer Starting up...")
	log.Infof("Version: %s", buildinfo.Version)
	log.Infof("Reconciliation period: %v", reconciliationPeriod)
	log.Infof("Gimbal kubernetes client QPS: %v", gimbalKubeClientQPS)
	log.Infof("Gimbal kubernetes client burst: %d", gimbalKubeClientBurst)

	// Init prometheus metrics
	discovererMetrics = localmetrics.NewMetrics("consul", backendName)
	discovererMetrics.RegisterPrometheus(true)

	// Log info metric
	discovererMetrics.DiscovererInfoMetric(buildinfo.Version)

	if debug {
		log.Level = logrus.DebugLevel
	}

	// Verify cluster name is passed
	if util.IsInvalidBackendName(backendName) {
		log.Fatalf("The Consul name must be provided using the `--backend-name` flag or the one passed is invalid")
	}
	log.Infof("BackendName is: %s", backendName)

	// Discovered cluster is passed
	if discovererConsulCfgFile == "" {
		log.Fatalf("`discover-consulcfg-file` arg is required!")
	}

	// Init
	gimbalKubeClient, err := k8s.NewClientWithQPS(gimbalKubeCfgFile, log, float32(gimbalKubeClientQPS), gimbalKubeClientBurst)
	if err != nil {
		log.Fatal("Could not init k8sclient! ", err)
	}

	discoverer, err := consul.NewReconciler(log, discovererMetrics, backendName, gimbalKubeClient, discovererConsulCfgFile, reconciliationPeriod, numProcessThreads, discoverererTagFilter, gimbaKubeNamespace)
	if err != nil {
		log.Fatal("Could not init Consulclient! ", err)
	}
	// set up signals so we handle the first shutdown signal gracefully
	stopCh := signals.SetupSignalHandler()

	go func() {
		// Expose the registered metrics via HTTP.
		http.Handle("/metrics", promhttp.HandlerFor(discovererMetrics.Registry, promhttp.HandlerOpts{}))
		srv := &http.Server{Addr: fmt.Sprintf(":%d", prometheusListenPort)}
		log.Info("Listening for Prometheus metrics on port: ", prometheusListenPort)
		if err := srv.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
		<-stopCh
		log.Info("Shutting down Prometheus server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Fatal(err)
		}
	}()

	log.Info("Starting Consul discoverer")
	go discoverer.Run(stopCh)

	<-stopCh
	log.Info("Stopped Consul discoverer")
}
