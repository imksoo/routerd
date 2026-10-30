// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/platform"
)

const vrfNativeStatusName = "vrf-native"

type vrfPlan struct {
	Name    string
	Table   int
	Members []string
}
type vrfPort struct {
	Name  string
	Index int
}
type vrfReceipt struct {
	Resource    string    `json:"resource"`
	Plan        vrfPlan   `json:"plan"`
	Token       string    `json:"token"`
	Scope       string    `json:"scope"`
	Index       int       `json:"index"`
	Pending     bool      `json:"pending"`
	Withdrawing bool      `json:"withdrawing"`
	Ports       []vrfPort `json:"ports"`
}
type vrfLink struct {
	Exists, Up               bool
	Name, Kind, Token, Scope string
	Index, Table, Master     int
}

// One concrete kernel boundary owns VRF links and declared member associations.
// The controller's existing SQLite boundary owns pending and recovery receipts.
type vrfNative interface {
	Scope(context.Context) (string, error)
	Observe(context.Context, string) (vrfLink, error)
	Create(context.Context, vrfPlan, int, string) error
	Cookie(context.Context, vrfLink, string) error
	Up(context.Context, vrfLink, string) error
	Attach(context.Context, vrfPort, vrfLink, string) error
	Delete(context.Context, vrfLink, string) error
}

type VRFController struct {
	Router *api.Router
	Store  Store
	DryRun bool
	OS     platform.OS
	Native vrfNative
}

func (c VRFController) currentOS() platform.OS {
	if c.OS != "" {
		return c.OS
	}
	return platform.CurrentOS()
}
func (c VRFController) native() vrfNative {
	if c.Native != nil {
		return c.Native
	}
	return newVRFNative()
}
func sameVRFPlan(left, right vrfPlan) bool {
	return left.Name == right.Name && left.Table == right.Table && slices.Equal(left.Members, right.Members)
}
func sameVRFReceipt(left, right vrfReceipt) bool {
	return left.Resource == right.Resource && sameVRFPlan(left.Plan, right.Plan) && left.Token == right.Token && left.Scope == right.Scope && left.Index == right.Index && left.Pending == right.Pending && left.Withdrawing == right.Withdrawing && slices.Equal(left.Ports, right.Ports)
}
func (c VRFController) plan(resource api.Resource) (vrfPlan, error) {
	spec, err := resource.VRFSpec()
	if err != nil {
		return vrfPlan{}, err
	}
	plan := vrfPlan{Name: firstNonEmpty(spec.IfName, resource.Metadata.Name), Table: spec.RouteTable}
	if plan.Name == "" || plan.Table < 1 || uint64(plan.Table) > math.MaxUint32 {
		return plan, errors.New("VRF requires a native name and positive 32-bit route table")
	}
	for _, member := range spec.Members {
		name := api.ResolveInterfaceIfName(c.Router, member)
		if name == "" || name == plan.Name || slices.Contains(plan.Members, name) {
			return plan, errors.New("VRF member identity is invalid or duplicated")
		}
		plan.Members = append(plan.Members, name)
	}
	sort.Strings(plan.Members)
	return plan, nil
}
func (c VRFController) currentPlan(name string) (vrfPlan, error) {
	if c.Router != nil {
		for _, resource := range c.Router.Spec.Resources {
			if resource.Kind == "VRF" && resource.Metadata.Name == name {
				return c.plan(resource)
			}
		}
	}
	return vrfPlan{}, errors.New("VRF parent declaration is no longer current")
}

func (c VRFController) readReceipts() ([]vrfReceipt, error) {
	if c.Store == nil {
		return nil, errors.New("VRF journal is unavailable")
	}
	status, err := c.Store.ReadObjectStatus(api.RouterAPIVersion, "Router", vrfNativeStatusName)
	if err != nil || status == nil {
		return nil, err
	}
	var records []vrfReceipt
	switch values := status["owners"].(type) {
	case []vrfReceipt:
		records = append([]vrfReceipt(nil), values...)
		for i := range records {
			records[i].Plan.Members = append([]string(nil), records[i].Plan.Members...)
			records[i].Ports = append([]vrfPort(nil), records[i].Ports...)
		}
	case []any:
		for _, value := range values {
			fields, ok := value.(map[string]any)
			if !ok {
				return nil, errors.New("invalid VRF receipt")
			}
			var record vrfReceipt
			for key, target := range map[string]*string{"resource": &record.Resource, "token": &record.Token, "scope": &record.Scope} {
				*target, ok = fields[key].(string)
				if !ok {
					return nil, fmt.Errorf("invalid VRF receipt %s", key)
				}
			}
			record.Pending, ok = fields["pending"].(bool)
			if !ok {
				return nil, errors.New("invalid VRF pending state")
			}
			record.Withdrawing, ok = fields["withdrawing"].(bool)
			if !ok {
				return nil, errors.New("invalid VRF withdrawal state")
			}
			index, ok := fields["index"].(float64)
			if !ok || index != math.Trunc(index) || index < 0 || index > math.MaxInt32 {
				return nil, errors.New("invalid VRF native index")
			}
			record.Index = int(index)
			plan, ok := fields["plan"].(map[string]any)
			if !ok {
				return nil, errors.New("invalid VRF plan")
			}
			record.Plan.Name, ok = plan["Name"].(string)
			if !ok {
				return nil, errors.New("invalid VRF native name")
			}
			table, ok := plan["Table"].(float64)
			if !ok || table != math.Trunc(table) || table < 1 || table > math.MaxUint32 {
				return nil, errors.New("invalid VRF route table")
			}
			record.Plan.Table = int(table)
			members, ok := plan["Members"].([]any)
			if !ok {
				return nil, errors.New("invalid VRF members")
			}
			for _, member := range members {
				name, ok := member.(string)
				if !ok || name == "" {
					return nil, errors.New("invalid VRF member")
				}
				record.Plan.Members = append(record.Plan.Members, name)
			}
			ports, ok := fields["ports"].([]any)
			if !ok {
				return nil, errors.New("invalid VRF member receipts")
			}
			for _, port := range ports {
				fields, ok := port.(map[string]any)
				if !ok {
					return nil, errors.New("invalid VRF member receipt")
				}
				name, named := fields["Name"].(string)
				index, indexed := fields["Index"].(float64)
				if !named || !indexed || name == "" || index < 1 || index > math.MaxInt32 || index != math.Trunc(index) {
					return nil, errors.New("invalid VRF member identity")
				}
				record.Ports = append(record.Ports, vrfPort{Name: name, Index: int(index)})
			}
			records = append(records, record)
		}
	default:
		return nil, errors.New("invalid VRF native journal")
	}
	seen := map[string]bool{}
	for _, record := range records {
		token, err := hex.DecodeString(record.Token)
		if err != nil || len(token) != 16 || record.Resource == "" || record.Scope == "" || record.Plan.Name == "" || record.Plan.Table < 1 || uint64(record.Plan.Table) > math.MaxUint32 || record.Index < 0 || !record.Pending && record.Index < 1 || seen["name:"+record.Plan.Name] || seen["resource:"+record.Resource] {
			return nil, errors.New("invalid VRF ownership receipt")
		}
		seen["name:"+record.Plan.Name], seen["resource:"+record.Resource] = true, true
	}
	return records, nil
}
func (c VRFController) saveReceipts(records []vrfReceipt) error {
	if records == nil {
		records = []vrfReceipt{}
	}
	for i := range records {
		if records[i].Plan.Members == nil {
			records[i].Plan.Members = []string{}
		}
		if records[i].Ports == nil {
			records[i].Ports = []vrfPort{}
		}
	}
	return c.Store.SaveObjectStatus(api.RouterAPIVersion, "Router", vrfNativeStatusName, map[string]any{"owners": records})
}
func (c VRFController) saveReceipt(record vrfReceipt) error {
	records, err := c.readReceipts()
	if err != nil {
		return err
	}
	for i, current := range records {
		if current.Resource == record.Resource {
			records[i] = record
			return c.saveReceipts(records)
		}
	}
	return c.saveReceipts(append(records, record))
}
func (c VRFController) checkScope(ctx context.Context, record vrfReceipt) error {
	scope, err := c.native().Scope(ctx)
	if err != nil {
		return err
	}
	if scope == "" || scope != record.Scope {
		return errors.New("VRF ownership belongs to another kernel scope")
	}
	return ctx.Err()
}
func checkVRFIdentity(record vrfReceipt, link vrfLink) error {
	if link.Scope == "" || link.Scope != record.Scope {
		return errors.New("VRF native scope differs from its ownership receipt")
	}
	if link.Exists && (link.Name != record.Plan.Name || link.Kind != "vrf" || link.Table != record.Plan.Table || link.Token != record.Token || record.Index > 0 && link.Index != record.Index) {
		return errors.New("VRF native identity differs from its ownership receipt")
	}
	return nil
}
func (c VRFController) checkCurrent(ctx context.Context, record vrfReceipt) (vrfLink, error) {
	var absent vrfLink
	plan, err := c.currentPlan(record.Resource)
	if err != nil {
		return absent, err
	}
	if !sameVRFPlan(plan, record.Plan) {
		return absent, errors.New("VRF intent changed while applying")
	}
	link, err := c.native().Observe(ctx, record.Plan.Name)
	if err != nil {
		return absent, err
	}
	if err := checkVRFIdentity(record, link); err != nil {
		return absent, err
	}
	records, err := c.readReceipts()
	if err != nil {
		return absent, err
	}
	found := false
	for _, current := range records {
		if sameVRFReceipt(current, record) {
			found = true
			break
		}
	}
	if !found {
		return absent, errors.New("VRF receipt changed while observing")
	}
	if err := c.checkScope(ctx, record); err != nil {
		return absent, err
	}
	plan, err = c.currentPlan(record.Resource)
	if err != nil {
		return absent, err
	}
	if !sameVRFPlan(plan, record.Plan) {
		return absent, errors.New("VRF intent changed after observation waits")
	}
	return link, ctx.Err()
}

func (c VRFController) Reconcile(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Router == nil || c.Store == nil || c.currentOS() != platform.OSLinux {
		return nil
	}
	plans := map[string]vrfPlan{}
	for _, resource := range c.Router.Spec.Resources {
		if resource.Kind != "VRF" {
			continue
		}
		plan, err := c.plan(resource)
		if err != nil {
			return err
		}
		plans[resource.Metadata.Name] = plan
	}
	records, err := c.readReceipts()
	if err != nil {
		return err
	}
	current := map[string]vrfReceipt{}
	for _, record := range records {
		if err := c.checkScope(ctx, record); err != nil {
			return err
		}
		plan, retained := plans[record.Resource]
		link, err := c.native().Observe(ctx, record.Plan.Name)
		if err != nil {
			return err
		}
		if err := checkVRFIdentity(record, link); err != nil {
			return err
		}
		if !retained || record.Withdrawing || !sameVRFPlan(plan, record.Plan) || record.Index > 0 && !link.Exists {
			if !c.DryRun {
				if err := c.withdraw(ctx, record); err != nil {
					return err
				}
			}
			continue
		}
		current[record.Resource] = record
	}
	names := make([]string, 0, len(plans))
	for name := range plans {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		plan := plans[name]
		if c.DryRun {
			if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "VRF", name, map[string]any{"phase": "Planned", "ifname": plan.Name, "routeTable": plan.Table}); err != nil {
				return err
			}
			continue
		}
		record, owned := current[name]
		if !owned {
			link, err := c.native().Observe(ctx, plan.Name)
			if err != nil {
				return err
			}
			if link.Exists {
				return fmt.Errorf("VRF/%s refuses an unowned native interface", name)
			}
			var token [16]byte
			if _, err := rand.Read(token[:]); err != nil {
				return err
			}
			record = vrfReceipt{Resource: name, Plan: plan, Token: hex.EncodeToString(token[:]), Scope: link.Scope, Pending: true}
			if err := c.checkScope(ctx, record); err != nil {
				return err
			}
			if err := c.saveReceipt(record); err != nil {
				return err
			}
		}
		if err := c.apply(ctx, &record); err != nil {
			return err
		}
		if err := c.Store.SaveObjectStatus(api.NetAPIVersion, "VRF", name, map[string]any{"phase": "Up", "ifname": plan.Name, "index": record.Index, "routeTable": plan.Table, "members": append([]string(nil), plan.Members...)}); err != nil {
			return err
		}
	}
	return nil
}
func (c VRFController) apply(ctx context.Context, record *vrfReceipt) error {
	link, err := c.checkCurrent(ctx, *record)
	if err != nil {
		return err
	}
	if !link.Exists {
		if record.Index > 0 {
			return errors.New("owned VRF disappeared before activation")
		}
		digest := sha256.Sum256([]byte(record.Token))
		index := int(binary.BigEndian.Uint32(digest[:4])&0x3fffffff) + 1024
		if err := c.native().Create(ctx, record.Plan, index, record.Scope); err != nil {
			return err
		}
		// The pending receipt must remain current before the successful create
		// acknowledgement can authorize cookie installation on its exact index.
		records, err := c.readReceipts()
		if err != nil {
			return err
		}
		found := false
		for _, current := range records {
			if sameVRFReceipt(current, *record) {
				found = true
				break
			}
		}
		if !found {
			return errors.New("VRF pending receipt changed before cookie installation")
		}
		if err := c.checkScope(ctx, *record); err != nil {
			return err
		}
		link = vrfLink{Exists: true, Name: record.Plan.Name, Index: index, Table: record.Plan.Table, Kind: "vrf", Scope: record.Scope}
		if err := c.native().Cookie(ctx, link, record.Token); err != nil {
			return err
		}
		record.Index = index
		link, err = c.native().Observe(ctx, record.Plan.Name)
		if err != nil {
			return err
		}
		if err := checkVRFIdentity(*record, link); err != nil {
			return err
		}
		if !link.Exists {
			return errors.New("VRF creation has no native readback")
		}
		if err := c.checkScope(ctx, *record); err != nil {
			return err
		}
		if err := c.saveReceipt(*record); err != nil {
			return err
		}
	} else if record.Index == 0 {
		record.Index = link.Index
		if err := c.saveReceipt(*record); err != nil {
			return err
		}
	}
	if !record.Pending && link.Up {
		healthy, err := c.membersMatch(ctx, *record)
		if err != nil {
			return err
		}
		if healthy {
			_, err := c.checkCurrent(ctx, *record)
			return err
		}
	}
	var ports []vrfPort
	for _, name := range record.Plan.Members {
		port, err := c.native().Observe(ctx, name)
		if err != nil {
			return err
		}
		if !port.Exists || port.Scope != record.Scope || port.Master != 0 && port.Master != record.Index {
			return errors.New("VRF member is absent or has another native owner")
		}
		ports = append(ports, vrfPort{Name: name, Index: port.Index})
	}
	record.Ports, record.Pending = ports, true
	if err := c.checkScope(ctx, *record); err != nil {
		return err
	}
	if err := c.saveReceipt(*record); err != nil {
		return err
	}
	for _, port := range record.Ports {
		link, err = c.checkCurrent(ctx, *record)
		if err != nil {
			return err
		}
		if !link.Exists {
			return errors.New("VRF disappeared before member attachment")
		}
		if err := c.native().Attach(ctx, port, link, record.Token); err != nil {
			return err
		}
	}
	link, err = c.checkCurrent(ctx, *record)
	if err != nil {
		return err
	}
	if !link.Exists {
		return errors.New("VRF disappeared before activation")
	}
	if err := c.native().Up(ctx, link, record.Token); err != nil {
		return err
	}
	link, err = c.checkCurrent(ctx, *record)
	if err != nil {
		return err
	}
	if !link.Exists || !link.Up {
		return errors.New("VRF activation has no native readback")
	}
	healthy, err := c.membersMatch(ctx, *record)
	if err != nil {
		return err
	}
	if !healthy {
		return errors.New("VRF member activation has no exact native readback")
	}
	if _, err := c.checkCurrent(ctx, *record); err != nil {
		return err
	}
	record.Pending = false
	return c.saveReceipt(*record)
}
func (c VRFController) membersMatch(ctx context.Context, record vrfReceipt) (bool, error) {
	if len(record.Ports) != len(record.Plan.Members) {
		return false, nil
	}
	for i, port := range record.Ports {
		if port.Name != record.Plan.Members[i] {
			return false, nil
		}
		observed, err := c.native().Observe(ctx, port.Name)
		if err != nil {
			return false, err
		}
		if !observed.Exists || observed.Scope != record.Scope || observed.Index != port.Index || observed.Master != record.Index {
			return false, nil
		}
	}
	return true, c.checkScope(ctx, record)
}
func (c VRFController) withdraw(ctx context.Context, record vrfReceipt) error {
	if err := c.checkScope(ctx, record); err != nil {
		return err
	}
	link, err := c.native().Observe(ctx, record.Plan.Name)
	if err != nil {
		return err
	}
	if err := checkVRFIdentity(record, link); err != nil {
		return err
	}
	record.Withdrawing = true
	if err := c.saveReceipt(record); err != nil {
		return err
	}
	link, err = c.native().Observe(ctx, record.Plan.Name)
	if err != nil {
		return err
	}
	if err := checkVRFIdentity(record, link); err != nil {
		return err
	}
	if err := c.checkScope(ctx, record); err != nil {
		return err
	}
	if link.Exists {
		if err := c.native().Delete(ctx, link, record.Token); err != nil {
			return err
		}
		after, err := c.native().Observe(ctx, record.Plan.Name)
		if err != nil {
			return err
		}
		if after.Scope != record.Scope || after.Exists {
			return errors.New("VRF withdrawal has no exact absence readback")
		}
	}
	records, err := c.readReceipts()
	if err != nil {
		return err
	}
	remaining := make([]vrfReceipt, 0, len(records))
	for _, current := range records {
		if current.Resource == record.Resource {
			if !sameVRFReceipt(current, record) {
				return errors.New("VRF receipt changed during withdrawal")
			}
			continue
		}
		remaining = append(remaining, current)
	}
	if err := c.checkScope(ctx, record); err != nil {
		return err
	}
	if err := c.saveReceipts(remaining); err != nil {
		return err
	}
	return c.Store.SaveObjectStatus(api.NetAPIVersion, "VRF", record.Resource, map[string]any{"phase": "Absent", "reason": "OwnedGenerationWithdrawn"})
}
