package consul

import (
	"strconv"
	"strings"

	"github.com/projectcontour/gimbal/pkg/translator"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// returns a kubernetes service for each load balancer in the slice
func kubeServices(backendName, tenantName string, services []Service) []v1.Service {
	var svcs []v1.Service
	for _, service := range services {
		svc := v1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: tenantName,
				Name:      translator.BuildDiscoveredName(backendName, serviceName(service)),
				Labels:    translator.AddGimbalLabels(backendName, serviceName(service), consulLabels(service)),
			},
			Spec: v1.ServiceSpec{
				Type:      v1.ServiceTypeClusterIP,
				ClusterIP: "None",
				Ports: []v1.ServicePort{
					{
						Name: portName(service.Port),
						Port: int32(service.Port),
						// The K8s API server sets this field on service creation. By setting
						// this ourselves, we prevent the discoverer from thinking it needs to
						// perform an update every time it compares the translated object with
						// the one that exists in gimbal.
						TargetPort: intstr.FromInt(service.Port),
						Protocol:   v1.ProtocolTCP, // only support TCP
					},
				},
			},
		}
		svcs = append(svcs, svc)
	}
	return svcs
}

// returns a kubernetes endpoints resource for each consul servie in the slice
func kubeEndpoints(backendName, tenantName string, services []Service) []Endpoints {
	endpoints := []Endpoints{}
	for _, service := range services {
		ep := v1.Endpoints{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: tenantName,
				Name:      translator.BuildDiscoveredName(backendName, serviceName(service)),
				Labels:    translator.AddGimbalLabels(backendName, serviceName(service), consulLabels(service)),
			},
		}
		// compute endpoint susbsets for each listener
		subsets := map[string]v1.EndpointSubset{}

		// We want to group all members that are listening on the same port
		// into a single EndpointSubset. We achieve this by using a map of
		// subsets, keyed by the listening port.
		for _, node := range service.Nodes {
			s := subsets[service.Name]
			// Add the port if we haven't added it yet to the EndpointSubset
			if len(s.Ports) == 0 {
				s.Ports = append(s.Ports, v1.EndpointPort{Name: portName(service.Port), Port: int32(service.Port), Protocol: v1.ProtocolTCP})
			}
			s.Addresses = append(s.Addresses, v1.EndpointAddress{IP: node}) // TODO: can address be something other than an IP address?
			subsets[service.Name] = s
		}

		// Add the subsets to the Endpoint
		for _, s := range subsets {
			ep.Subsets = append(ep.Subsets, s)
		}

		endpoints = append(endpoints, Endpoints{endpoints: ep, upstreamName: serviceNameOriginal(service)})
	}

	return endpoints

}

func consulLabels(service Service) map[string]string {
	// Sanitize the load balancer name according to the kubernetes label value
	// requirements: "Valid label values must be 63 characters or less and must
	// be empty or begin and end with an alphanumeric character ([a-z0-9A-Z])
	// with dashes (-), underscores (_), dots (.), and alphanumerics between."
	// name := service.Name
	// if name != "" {
	// 	// 1. replace unallowed chars with a dash
	// 	reg := regexp.MustCompile(`[^a-zA-Z0-9\-._]`)
	// 	name = reg.ReplaceAllString(service.Name, "-")

	// 	// 2. prepend/append a special marker if first/last char is not an alphanum
	// 	if !isalphanum(name[0]) {
	// 		name = "consul" + name
	// 	}
	// 	if !isalphanum(name[len(name)-1]) {
	// 		name = name + "consul"
	// 	}
	// 	// 3. shorten if necessary
	// 	name = translator.ShortenKubernetesLabelValue(name)
	// }
	// return map[string]string{
	// 	"gimbal.projectcontour.io/consul-service-id": name,
	// }

	return map[string]string{}
}

func isalphanum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// get the lb Name or ID if name is empty
func serviceNameOriginal(service Service) string {
	return strings.ToLower(service.Name)
}

// use the load balancer ID as the service name
// context: heptio/gimbal #216
func serviceName(service Service) string {
	return strings.ToLower(service.Name)
}

func portName(port int) string {
	p := strconv.Itoa(port)
	return "port-" + p
}
