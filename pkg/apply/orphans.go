// SPDX-License-Identifier: BSD-3-Clause

package apply

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/imksoo/routerd/pkg/resource"
)

func freeBSDIPv4MaskPrefix(mask string) string {
	mask = strings.TrimPrefix(mask, "0x")
	if len(mask) != 8 {
		return ""
	}
	value, err := strconv.ParseUint(mask, 16, 32)
	if err != nil {
		return ""
	}
	prefix := 0
	for i := 31; i >= 0; i-- {
		if value&(1<<uint(i)) == 0 {
			break
		}
		prefix++
	}
	return fmt.Sprintf("%d", prefix)
}

func newIPv4FwmarkRuleArtifact(owner string, priority, mark, table int) resource.Artifact {
	return resource.Artifact{
		Kind:  "linux.ipv4.fwmarkRule",
		Name:  fmt.Sprintf("priority=%d,mark=0x%x,table=%d", priority, mark, table),
		Owner: owner,
		Attributes: map[string]string{
			"priority": fmt.Sprintf("%d", priority),
			"mark":     fmt.Sprintf("0x%x", mark),
			"table":    fmt.Sprintf("%d", table),
		},
	}
}

func newNftTableArtifact(owner, family, name string) resource.Artifact {
	return resource.Artifact{
		Kind:  "nft.table",
		Name:  family + "/" + name,
		Owner: owner,
		Attributes: map[string]string{
			"family": family,
			"name":   name,
		},
	}
}
