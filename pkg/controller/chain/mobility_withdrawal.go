// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
)

func readMobilityNativeStatus(store Store) (map[string]any, error) {
	reader, ok := store.(interface {
		ReadObjectStatus(string, string, string) (map[string]any, error)
	})
	if !ok || reader == nil {
		return nil, fmt.Errorf("native mobility ownership requires an error-aware status reader")
	}
	status, err := reader.ReadObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName)
	if err != nil {
		return nil, fmt.Errorf("read native mobility ownership: %w", err)
	}
	return status, nil
}

func matchingMobilityWithdrawalAcks(requests []dynamicconfig.MobilityWithdrawalRequest, absent func(dynamicconfig.MobilityWithdrawalRequest) bool) []dynamicconfig.MobilityWithdrawalRequest {
	acks := make([]dynamicconfig.MobilityWithdrawalRequest, 0, len(requests))
	for _, request := range requests {
		if absent(request) {
			acks = append(acks, request)
		}
	}
	return acks
}

// The real callback may also own ordinary resources. Invalidate a previous
// completion before entering any of those effects, not only its typed tail.
func invalidateMobilityNativeAck(store Store, field string) error {
	status, err := readMobilityNativeStatus(store)
	if err != nil {
		return err
	}
	if !statusValuePresent(status, field) {
		return nil
	}
	merger, ok := store.(objectStatusMerger)
	if !ok {
		return fmt.Errorf("native mobility acknowledgement merger unavailable")
	}
	return merger.MergeObjectStatus(api.RouterAPIVersion, "Router", dynamicconfig.MobilityNativeStatusName, map[string]any{field: []dynamicconfig.MobilityWithdrawalRequest{}})
}
