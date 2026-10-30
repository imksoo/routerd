// SPDX-License-Identifier: BSD-3-Clause

package dynamicconfig

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/netip"
	"sort"
	"strings"
)

// MobilityWithdrawalRequest binds native-effect completion to one operator
// transfer and its exact typed desired plan. An empty effect plan is still a
// request; absence of a status row is never an acknowledgement.
type MobilityWithdrawalRequest struct {
	ID          string `json:"id" yaml:"id"`
	GroupRef    string `json:"groupRef" yaml:"groupRef"`
	PoolRef     string `json:"poolRef" yaml:"poolRef"`
	PoolPrefix  string `json:"poolPrefix" yaml:"poolPrefix"`
	Address     string `json:"address" yaml:"address"`
	FromNodeRef string `json:"fromNodeRef" yaml:"fromNodeRef"`
	ToNodeRef   string `json:"toNodeRef" yaml:"toNodeRef"`
	PlanDigest  string `json:"planDigest" yaml:"planDigest"`
}

const (
	MobilityNativeStatusName      = "sam-dataplane"
	MobilitySAMWithdrawalAcks     = "samWithdrawalAcks"
	MobilityRouteWithdrawalAcks   = "routeWithdrawalAcks"
	MobilityAddressWithdrawalAcks = "addressWithdrawalAcks"
)

// MobilityNativePlanDigest excludes withdrawal metadata and observation-only
// plans. Copies preserve ownership of the caller's slices while sorting.
func MobilityNativePlanDigest(plan MobilityDataplanePlan) string {
	plan.Withdrawals = nil
	plan.Captures = append([]LocalCaptureIntent(nil), plan.Captures...)
	plan.Routes = append([]MobilityIPv4RouteIntent(nil), plan.Routes...)
	plan.StaticAddresses = append([]MobilityIPv4AddressIntent(nil), plan.StaticAddresses...)
	sort.Slice(plan.Captures, func(i, j int) bool { return plan.Captures[i].ID < plan.Captures[j].ID })
	sort.Slice(plan.Routes, func(i, j int) bool { return plan.Routes[i].ID < plan.Routes[j].ID })
	sort.Slice(plan.StaticAddresses, func(i, j int) bool { return plan.StaticAddresses[i].ID < plan.StaticAddresses[j].ID })
	data, _ := json.Marshal(plan)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func ValidateMobilityWithdrawalRequest(request MobilityWithdrawalRequest) error {
	for _, value := range []string{request.ID, request.GroupRef, request.PoolRef, request.FromNodeRef, request.ToNodeRef} {
		if value == "" || value != strings.TrimSpace(value) {
			return fmt.Errorf("withdrawal identity is required and must be canonical")
		}
	}
	if request.FromNodeRef == request.ToNodeRef {
		return fmt.Errorf("withdrawal source and target are equal")
	}
	pool, err := strictIPv4Prefix(request.PoolPrefix)
	if err != nil {
		return fmt.Errorf("withdrawal poolPrefix: %w", err)
	}
	if _, err := scopedIPv4HostPrefix(request.Address, pool); err != nil {
		return fmt.Errorf("withdrawal address: %w", err)
	}
	if !strings.HasPrefix(request.PlanDigest, "sha256:") || len(request.PlanDigest) != 71 {
		return fmt.Errorf("withdrawal native plan digest is required")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(request.PlanDigest, "sha256:")); err != nil {
		return fmt.Errorf("withdrawal native plan digest: %w", err)
	}
	return nil
}

// MobilityEffectContainsAddress compares a native address or route scope with
// the transferred host. Broad routes remain relevant; another pool's effects
// are filtered by its owning caller before this check.
func MobilityEffectContainsAddress(effect, address string) bool {
	host, err := netip.ParsePrefix(address)
	if err != nil {
		return false
	}
	prefix, err := netip.ParsePrefix(effect)
	if err == nil {
		return prefix.Contains(host.Addr())
	}
	ip, err := netip.ParseAddr(effect)
	return err == nil && ip == host.Addr()
}
