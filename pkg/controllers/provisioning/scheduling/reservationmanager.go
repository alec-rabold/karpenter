/*
Copyright The Kubernetes Authors.

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

package scheduling

import (
	"fmt"

	"k8s.io/apimachinery/pkg/util/sets"

	v1 "sigs.k8s.io/karpenter/pkg/apis/v1"
	"sigs.k8s.io/karpenter/pkg/cloudprovider"
)

type ReservationManager struct {
	reservations map[string]sets.Set[string] // hostname -> set[reservation id]
	capacity     map[string]int              // reservation id -> count
}

func NewReservationManager(instanceTypes map[string][]*cloudprovider.InstanceType) *ReservationManager {
	capacity := map[string]int{}
	for _, its := range instanceTypes {
		for _, it := range its {
			for _, o := range it.Offerings {
				if o.CapacityType() != v1.CapacityTypeReserved {
					continue
				}
				// If we have multiple offerings with the same reservation ID, track the one with the least capacity. This could be
				// the result of multiple nodepools referencing the same capacity reservation, and there being an update to the
				// capacity between calls to GetInstanceTypes.
				if current, ok := capacity[o.ReservationID()]; !ok || current > o.ReservationCapacity {
					capacity[o.ReservationID()] = o.ReservationCapacity
				}
			}
		}
	}
	return &ReservationManager{
		reservations: map[string]sets.Set[string]{},
		capacity:     capacity,
	}
}

func (rm *ReservationManager) Reserve(hostname string, offering *cloudprovider.Offering) bool {
	reservations, ok := rm.reservations[hostname]
	if ok && reservations.Has(offering.ReservationID()) {
		return true
	}
	if !ok {
		reservations = sets.New[string]()
		rm.reservations[hostname] = reservations
	}
	capacity, ok := rm.capacity[offering.ReservationID()]
	if !ok {
		// Note: this panic should never occur, and would indicate a serious bug in the scheduling code.
		panic(fmt.Sprintf("attempted to reserve non-existent offering with reservation id %q", offering.ReservationID()))
	}
	if capacity == 0 {
		return false
	}
	rm.capacity[offering.ReservationID()] -= 1
	reservations.Insert(offering.ReservationID())
	return true
}

func (rm *ReservationManager) Release(hostname string, offerings ...*cloudprovider.Offering) {
	for _, o := range offerings {
		if reservations, ok := rm.reservations[hostname]; ok && reservations.Has(o.ReservationID()) {
			reservations.Delete(o.ReservationID())
			rm.capacity[o.ReservationID()] += 1
		}
	}
}
