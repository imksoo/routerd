// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"fmt"
	"time"

	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/platform"
)

func currentMobilityNativePlan(source dynamicConfigPartLister, snapshot dynamicconfig.MobilityDataplanePlan) (dynamicconfig.MobilityDataplanePlan, error) {
	if source == nil {
		return snapshot, nil
	}
	records, err := source.ListDynamicConfigParts()
	if err != nil {
		return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("observe current Mobility native intent: %w", err)
	}
	return mobilityDataplanePlanFromRecords(records, time.Now().UTC(), platform.CurrentOS())
}
