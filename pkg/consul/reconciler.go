package consul

import (
	"errors"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/hashicorp/consul/api"
	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	localmetrics "github.com/projectcontour/gimbal/pkg/metrics"
	"github.com/projectcontour/gimbal/pkg/sync"
	"github.com/projectcontour/gimbal/pkg/translator"
)

// Endpoints represents a v1.Endpoints + upstream name to facilicate metrics
type Endpoints struct {
	endpoints    v1.Endpoints
	upstreamName string
}

type Service struct {
	Name  string
	Port  int
	Nodes []string
}

// Reconciler is an implementation of a registry backend for consul.
type Reconciler struct {
	client *api.Client
	dc     string

	logger *logrus.Logger
	// GimbalKubeClient is the client of the Kubernetes cluster where Gimbal is running
	gimbalKubeClient kubernetes.Interface

	metrics     localmetrics.DiscovererMetrics
	backendName string

	// Interval between reconciliation loops
	syncPeriod time.Duration
	syncqueue  sync.Queue

	tagFilter string

	namespace string
}

func NewReconciler(log *logrus.Logger, metrics localmetrics.DiscovererMetrics, backendName string,
	gimbalKubeClient kubernetes.Interface, cfgFile string, syncPeriod time.Duration, queueWorkers int, filter, namespace string) (*Reconciler, error) {
	yamlFile, err := ioutil.ReadFile(cfgFile)
	if err != nil {
		return nil, err
	}
	cfg := &ConsulConfig{}
	err = yaml.Unmarshal(yamlFile, cfg)
	if err != nil {
		return nil, err
	}

	consulCfg := &api.Config{Address: cfg.Addr, Scheme: cfg.Scheme, Token: cfg.Token}
	if cfg.Scheme == "https" {
		consulCfg.TLSConfig.KeyFile = cfg.TLS.KeyFile
		consulCfg.TLSConfig.CertFile = cfg.TLS.CertFile
		consulCfg.TLSConfig.CAFile = cfg.TLS.CAFile
		consulCfg.TLSConfig.CAPath = cfg.TLS.CAPath
		consulCfg.TLSConfig.InsecureSkipVerify = cfg.TLS.InsecureSkipVerify
	}

	// create a reusable client
	c, err := api.NewClient(consulCfg)
	if err != nil {
		return nil, err
	}

	// ping the agent
	dc, err := datacenter(c)
	if err != nil {
		return nil, err
	}

	return &Reconciler{
		client:           c,
		dc:               dc,
		logger:           log,
		metrics:          metrics,
		backendName:      backendName,
		gimbalKubeClient: gimbalKubeClient,
		syncPeriod:       syncPeriod,
		syncqueue:        sync.NewQueue(log, gimbalKubeClient, queueWorkers, metrics),
		tagFilter:        filter,
		namespace:        namespace,
	}, nil
}

func (r *Reconciler) Run(stopC <-chan struct{}) {

	go r.syncqueue.Run(stopC)

	ticker := time.NewTicker(r.syncPeriod)
	defer ticker.Stop()

	// Perform an initial reconciliation
	r.reconcile()

	// Perform reconciliation on every tick
	for {
		select {
		case <-stopC:
			r.logger.Infof("Stop Cosnul discoverer")
			return
		case <-ticker.C:
			r.reconcile()
		}
	}
}

// datacenter returns the datacenter of the local agent
func datacenter(c *api.Client) (string, error) {
	self, err := c.Agent().Self()
	if err != nil {
		return "", err
	}

	cfg, ok := self["Config"]
	if !ok {
		return "", errors.New("consul: self.Config not found")
	}
	dc, ok := cfg["Datacenter"].(string)
	if !ok {
		return "", errors.New("consul: self.Datacenter not found")
	}
	return dc, nil
}

func (r *Reconciler) reconcile() {
	// Calculate cycle time
	start := time.Now()

	log := r.logger
	log.Info("reconciling consul services")

	services, _, err := r.client.Catalog().Services(&api.QueryOptions{})
	if err != nil {
		log.Errorf("can not get services from consul, err: %s", err.Error())
		return
	}

	var svcs []Service

	for name, tags := range services {
		if !contains(tags, r.tagFilter) {
			continue
		}

		//
		servicesData, _, err := r.client.Health().Service(name, "", true, &api.QueryOptions{})
		if err != nil {
			log.Errorf("can not get services(%s) from consul, err: %s", name, err.Error())
			continue
		}

		svc := Service{
			Name: name,
		}

		var nodes []string

		for _, entry := range servicesData {
			nodes = append(nodes, entry.Service.Address)
			if svc.Port == 0 {
				svc.Port = entry.Service.Port
			}
		}
		svc.Nodes = nodes

		svcs = append(svcs, svc)

	}

	// Get all services and endpoints that exist in the corresponding namespace
	clusterLabelSelector := fmt.Sprintf("%s=%s", translator.GimbalLabelBackend, r.backendName)
	currentServices, err := r.gimbalKubeClient.CoreV1().Services(r.namespace).List(metav1.ListOptions{LabelSelector: clusterLabelSelector})
	if err != nil {
		r.metrics.GenericMetricError("ListServicesInNamespace")
		log.Errorf("error listing services in namespace %q: %v", r.namespace, err)
	}

	currentk8sEndpoints, err := r.gimbalKubeClient.CoreV1().Endpoints(r.namespace).List(metav1.ListOptions{LabelSelector: clusterLabelSelector})
	if err != nil {
		r.metrics.GenericMetricError("ListEndpointsInNamespace")
		log.Errorf("error listing endpoints in namespace:%q: %v", r.namespace, err)
	}

	// Convert the k8s list to type []Endpoints so make comparison easier
	currentEndpoints := []Endpoints{}
	for _, v := range currentk8sEndpoints.Items {
		currentEndpoints = append(currentEndpoints, Endpoints{endpoints: v, upstreamName: ""})
	}

	// Reconcile current state with desired state
	desiredSvcs := kubeServices(r.backendName, r.namespace, svcs)
	r.reconcileSvcs(desiredSvcs, currentServices.Items)

	desiredEndpoints := kubeEndpoints(r.backendName, r.namespace, svcs)
	r.reconcileEndpoints(desiredEndpoints, currentEndpoints)

	// Log upstream /invalid services to prometheus
	totalUpstreamServices := len(svcs)
	totalInvalidServices := totalUpstreamServices - len(svcs)
	r.metrics.DiscovererUpstreamServicesMetric(r.namespace, totalUpstreamServices)
	r.metrics.DiscovererInvalidServicesMetric(r.namespace, totalInvalidServices)

	// Log to Prometheus the cycle duration
	r.metrics.CycleDurationMetric(time.Since(start))
}

func (r *Reconciler) reconcileSvcs(desiredSvcs, currentSvcs []v1.Service) {
	add, up, del := diffServices(desiredSvcs, currentSvcs)
	for _, svc := range add {
		s := svc
		r.syncqueue.Enqueue(sync.AddServiceAction(&s))
	}
	for _, svc := range up {
		s := svc
		r.syncqueue.Enqueue(sync.UpdateServiceAction(&s))
	}
	for _, svc := range del {
		s := svc
		r.syncqueue.Enqueue(sync.DeleteServiceAction(&s))
	}
}

func (r *Reconciler) reconcileEndpoints(desired []Endpoints, current []Endpoints) {
	add, up, del := diffEndpoints(desired, current)
	for _, ep := range add {
		e := ep
		r.syncqueue.Enqueue(sync.AddEndpointsAction(&e.endpoints, e.upstreamName))
	}
	for _, ep := range up {
		e := ep
		r.syncqueue.Enqueue(sync.UpdateEndpointsAction(&e.endpoints, e.upstreamName))
	}
	for _, ep := range del {
		e := ep
		r.syncqueue.Enqueue(sync.DeleteEndpointsAction(&e.endpoints, e.upstreamName))
	}
}

func contains(s []string, e string) bool {
	for _, v := range s {
		if e == v {
			return true
		}
	}
	return false
}
