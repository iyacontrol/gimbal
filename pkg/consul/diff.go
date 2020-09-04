package consul

import (
	"reflect"

	v1 "k8s.io/api/core/v1"
)

func diffServices(desired, current []v1.Service) (add, update, del []v1.Service) {
	// Services that exist, but are no longer desired should be deleted
	for _, currentSvc := range current {
		if !containsSvc(currentSvc, desired) {
			del = append(del, currentSvc)
		}
	}

	// Services that are desired, but do not exist, should be added
	for _, desiredSvc := range desired {
		if !containsSvc(desiredSvc, current) {
			add = append(add, desiredSvc)
		}
	}

	for _, currentSvc := range current {
		for _, desiredSvc := range desired {
			if serviceEquals(&currentSvc, &desiredSvc) {
				if !serviceEqualsDetail(&currentSvc, &desiredSvc) {
					update = append(update, desiredSvc)
				}
				break
			}
		}
	}
	return add, update, del
}

func diffEndpoints(desired []Endpoints, current []Endpoints) (add, update, del []Endpoints) {
	for _, currentEp := range current {
		if !containsEndpoint(currentEp, desired) {
			del = append(del, currentEp)
		}
	}

	for _, desiredEp := range desired {
		if !containsEndpoint(desiredEp, current) {
			add = append(add, desiredEp)
		}
	}

	for _, currentEp := range current {
		for _, desiredEp := range desired {
			if endpointEquals(&currentEp, &desiredEp) {
				if !endpointEqualsDetail(&currentEp, &desiredEp) {
					update = append(update, desiredEp)
				}
				break
			}
		}
	}
	return add, update, del
}

func containsSvc(x v1.Service, xs []v1.Service) bool {
	for _, s := range xs {
		if serviceEquals(&x, &s) {
			return true
		}
	}
	return false
}

func containsEndpoint(x Endpoints, xs []Endpoints) bool {
	for _, e := range xs {
		if endpointEquals(&x, &e) {
			return true
		}
	}
	return false
}

func serviceEquals(o1, o2 *v1.Service) bool {
	return o1.GetName() == o2.GetName() &&
		o1.GetNamespace() == o2.GetNamespace()
}

func serviceEqualsDetail(o1, o2 *v1.Service) bool {
	return o1.GetName() == o2.GetName() &&
		o1.GetNamespace() == o2.GetNamespace() &&
		reflect.DeepEqual(o1.Spec.Ports, o2.Spec.Ports)
}

func endpointEquals(o1, o2 *Endpoints) bool {
	return o1.endpoints.GetName() == o2.endpoints.GetName() &&
		o1.endpoints.GetNamespace() == o2.endpoints.GetNamespace()
}

func endpointEqualsDetail(o1, o2 *Endpoints) bool {
	return o1.endpoints.GetName() == o2.endpoints.GetName() &&
		o1.endpoints.GetNamespace() == o2.endpoints.GetNamespace() &&
		reflect.DeepEqual(o1.endpoints.Subsets, o2.endpoints.Subsets)
}
