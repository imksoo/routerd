// SPDX-License-Identifier: BSD-3-Clause

// Package codec translates DynamicConfigPart values at the SQLite persistence
// boundary. Keeping this adapter below controllers prevents each producer and
// consumer from maintaining its own JSON field mapping.
package codec

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	routerstate "github.com/imksoo/routerd/pkg/state"
	"gopkg.in/yaml.v3"
)

const (
	mobilityPoolPlanGeneration = int64(1)
	maxMobilityPoolPlanLease   = 10 * time.Minute
)

// ActiveMobilityPoolPlanRecord is one canonical, live record on the reserved
// MobilityPool typed channel. The record envelope is validated before its
// payload is decoded so a malformed SQLite row cannot become a host effect.
type ActiveMobilityPoolPlanRecord struct {
	Record routerstate.DynamicConfigPartRecord
	Source dynamicconfig.MobilityPoolPlanSource
}

// ActiveMobilityPoolPlanRecords returns the one valid active record per exact
// typed source. Invalid or duplicate records invalidate their complete Pool so
// a stale node cannot coexist with a newer plan. Expired records are ordinary
// withdrawal state and are ignored rather than treated as invalid.
func ActiveMobilityPoolPlanRecords(records []routerstate.DynamicConfigPartRecord, now time.Time) ([]ActiveMobilityPoolPlanRecord, map[string]bool) {
	now = now.UTC()
	bySource := map[string]ActiveMobilityPoolPlanRecord{}
	invalidPools := map[string]bool{}
	for _, record := range records {
		source, ok := dynamicconfig.ParseMobilityPoolPlanSource(record.Source)
		if !ok {
			continue
		}
		if !record.ExpiresAt.IsZero() && !now.Before(record.ExpiresAt.UTC()) {
			continue
		}
		if !validMobilityPoolPlanEnvelope(record, now) {
			invalidPools[source.PoolRef] = true
			continue
		}
		if _, duplicate := bySource[record.Source]; duplicate {
			invalidPools[source.PoolRef] = true
			continue
		}
		bySource[record.Source] = ActiveMobilityPoolPlanRecord{Record: record, Source: source}
	}
	out := make([]ActiveMobilityPoolPlanRecord, 0, len(bySource))
	for _, record := range bySource {
		if !invalidPools[record.Source.PoolRef] {
			out = append(out, record)
		}
	}
	return out, invalidPools
}

func validMobilityPoolPlanEnvelope(record routerstate.DynamicConfigPartRecord, now time.Time) bool {
	if record.Generation != mobilityPoolPlanGeneration || strings.TrimSpace(record.Status) != "active" ||
		record.ObservedAt.IsZero() || record.ExpiresAt.IsZero() || strings.TrimSpace(record.Digest) == "" {
		return false
	}
	observedAt := record.ObservedAt.UTC()
	expiresAt := record.ExpiresAt.UTC()
	return now.Before(expiresAt) && expiresAt.After(observedAt) && expiresAt.Sub(observedAt) <= maxMobilityPoolPlanLease
}

// Encode converts a DynamicConfigPart into its persisted representation. Empty
// optional fields remain empty so existing SQLite NULL semantics are retained.
func Encode(part dynamicconfig.DynamicConfigPart) (routerstate.DynamicConfigPartRecord, error) {
	resources, err := json.Marshal(part.Spec.Resources)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	directives, err := json.Marshal(part.Spec.Directives)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	actionPlans, err := encodeOptional(part.Spec.ActionPlans)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	mobilityDataplane, err := encodeMobilityDataplanePlan(part.Spec.MobilityDataplane)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	arpObserverIntents, err := encodeOptional(part.Spec.ARPObserverIntents)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	fibVerdicts, err := encodeOptional(part.Spec.FIBVerdicts)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	return routerstate.DynamicConfigPartRecord{
		Source:                 part.Spec.Source,
		Generation:             part.Spec.Generation,
		ObservedAt:             part.Spec.ObservedAt,
		ExpiresAt:              part.Spec.ExpiresAt,
		Digest:                 part.Spec.Digest,
		ResourcesJSON:          string(resources),
		DirectivesJSON:         string(directives),
		ActionPlansJSON:        actionPlans,
		MobilityDataplaneJSON:  mobilityDataplane,
		ARPObserverIntentsJSON: arpObserverIntents,
		FIBVerdictsJSON:        fibVerdicts,
		Status:                 "active",
	}, nil
}

func encodeMobilityDataplanePlan(plan dynamicconfig.MobilityDataplanePlan) (string, error) {
	if plan.IsEmpty() {
		return "", nil
	}
	data, err := json.Marshal(plan)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func encodeOptional(value any) (string, error) {
	if value == nil {
		return "", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	if string(data) == "null" || string(data) == "[]" {
		return "", nil
	}
	return string(data), nil
}

// Decode reconstructs one DynamicConfigPart from a persisted record.
func Decode(record routerstate.DynamicConfigPartRecord) (dynamicconfig.DynamicConfigPart, error) {
	resources, err := DecodeGenericResources(record)
	if err != nil {
		return dynamicconfig.DynamicConfigPart{}, recordDecodeError(record, "resources", err)
	}
	directives, err := DecodeGenericDirectives(record)
	if err != nil {
		return dynamicconfig.DynamicConfigPart{}, recordDecodeError(record, "directives", err)
	}
	return dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{Name: fmt.Sprintf("%s-%d", record.Source, record.Generation)},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     record.Source,
			Generation: record.Generation,
			ObservedAt: record.ObservedAt,
			ExpiresAt:  record.ExpiresAt,
			Digest:     record.Digest,
			Resources:  resources,
			Directives: directives,
		},
	}, nil
}

// DecodeGenericResources returns the generic effective-config resources in a
// persisted record. MobilityPool is a dedicated typed plan channel, so an old
// generic payload under that source is deliberately inert even before its next
// reconcile replaces the row.
func DecodeGenericResources(record routerstate.DynamicConfigPartRecord) ([]api.Resource, error) {
	if dynamicconfig.IsMobilityPoolReservedSource(record.Source) {
		return nil, nil
	}
	return DecodeResources(record.ResourcesJSON)
}

// DecodeGenericDirectives is the directive counterpart to
// DecodeGenericResources. MobilityPool effects may not use generic directives.
func DecodeGenericDirectives(record routerstate.DynamicConfigPartRecord) ([]dynamicconfig.DynamicConfigDirective, error) {
	if dynamicconfig.IsMobilityPoolReservedSource(record.Source) {
		return nil, nil
	}
	return DecodeDirectives(record.DirectivesJSON)
}

func recordDecodeError(record routerstate.DynamicConfigPartRecord, field string, err error) error {
	return fmt.Errorf("%s generation %d %s: %w", record.Source, record.Generation, field, err)
}

// DecodeAll reconstructs every supplied DynamicConfigPart record in order.
func DecodeAll(records []routerstate.DynamicConfigPartRecord) ([]dynamicconfig.DynamicConfigPart, error) {
	parts := make([]dynamicconfig.DynamicConfigPart, 0, len(records))
	for _, record := range records {
		part, err := Decode(record)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

// DecodeResources decodes persisted JSON resources through YAML so Resource
// specs regain their concrete API types while retaining JSON compatibility.
func DecodeResources(raw string) ([]api.Resource, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var resources []api.Resource
	if err := yaml.Unmarshal([]byte(raw), &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

// DecodeDirectives decodes persisted dynamic directives.
func DecodeDirectives(raw string) ([]dynamicconfig.DynamicConfigDirective, error) {
	return DecodeSlice[dynamicconfig.DynamicConfigDirective](raw)
}

// DecodeActionPlans decodes persisted display-only provider action plans.
func DecodeActionPlans(raw string) ([]dynamicconfig.ActionPlan, error) {
	return DecodeSlice[dynamicconfig.ActionPlan](raw)
}

// DecodeMobilityDataplanePlan decodes the one typed local-dataplane plan
// persisted for a mobility DynamicConfigPart. The old local capture-intent
// column is deliberately not consulted: active plans must be produced again
// by the current mobility planner rather than reconstructed from legacy data.
func DecodeMobilityDataplanePlan(raw string) (dynamicconfig.MobilityDataplanePlan, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return dynamicconfig.MobilityDataplanePlan{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &fields); err != nil {
		return dynamicconfig.MobilityDataplanePlan{}, err
	}
	if fields == nil {
		return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("mobility dataplane plan must be a JSON object")
	}
	_, hasPoolPrefix := fields["poolPrefix"]
	_, hasCaptures := fields["captures"]
	_, hasRoutes := fields["routes"]
	_, hasStaticAddresses := fields["staticAddresses"]
	for field := range fields {
		switch field {
		case "poolPrefix", "captures", "routes", "staticAddresses":
		default:
			return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("mobility dataplane plan has unknown field %q", field)
		}
	}
	if !hasCaptures && !hasRoutes && !hasStaticAddresses {
		return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("mobility dataplane plan has no known fields")
	}
	if !hasPoolPrefix || isJSONNull(fields["poolPrefix"]) {
		return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("mobility dataplane plan requires poolPrefix")
	}
	for _, field := range []string{"captures", "routes", "staticAddresses"} {
		if value, ok := fields[field]; ok && isJSONNull(value) {
			return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("mobility dataplane plan field %q must not be null", field)
		}
	}
	var plan dynamicconfig.MobilityDataplanePlan
	if err := decodeStrictJSON(raw, &plan); err != nil {
		return dynamicconfig.MobilityDataplanePlan{}, err
	}
	if plan.IsEmpty() {
		return dynamicconfig.MobilityDataplanePlan{}, fmt.Errorf("nonempty mobility dataplane plan has no effects")
	}
	return plan, nil
}

// DecodeARPObserverIntents decodes the daemon-bootstrap channel strictly. The
// consumer must reject unknown fields and null arrays before a record can be
// converted into a supervised observer, because on-demand ARP can emit frames.
func DecodeARPObserverIntents(raw string) ([]dynamicconfig.ARPObserverIntent, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var intents []dynamicconfig.ARPObserverIntent
	if err := decodeStrictJSON(raw, &intents); err != nil {
		return nil, err
	}
	if intents == nil {
		return nil, fmt.Errorf("ARP observer intents must be a JSON array, not null")
	}
	return intents, nil
}

// DecodeMobilityFIBVerdicts decodes the typed BGP admission channel without
// the permissive map/slice coercions used by legacy dynamic payloads. A
// MobilityPool FIB plan is safety data: unknown fields, null rows, duplicate
// JSON keys, and a second JSON value are all rejected before semantic
// validation decides whether its Pool may contribute routes.
func DecodeMobilityFIBVerdicts(raw string) ([]dynamicconfig.FIBVerdict, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	if err := rejectDuplicateJSONKeys(raw); err != nil {
		return nil, err
	}
	var rows []json.RawMessage
	if err := decodeStrictJSON(raw, &rows); err != nil {
		return nil, err
	}
	if rows == nil {
		return nil, fmt.Errorf("Mobility FIB verdicts must be a JSON array, not null")
	}
	verdicts := make([]dynamicconfig.FIBVerdict, 0, len(rows))
	for index, row := range rows {
		if isJSONNull(row) {
			return nil, fmt.Errorf("Mobility FIB verdict %d must not be null", index)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(row, &fields); err != nil || fields == nil {
			if err != nil {
				return nil, fmt.Errorf("Mobility FIB verdict %d: %w", index, err)
			}
			return nil, fmt.Errorf("Mobility FIB verdict %d must be a JSON object", index)
		}
		for field, value := range fields {
			switch field {
			case "poolRef", "scope", "address", "action", "class", "ownerNode", "reason":
			default:
				return nil, fmt.Errorf("Mobility FIB verdict %d has unknown field %q", index, field)
			}
			if isJSONNull(value) {
				return nil, fmt.Errorf("Mobility FIB verdict %d field %q must not be null", index, field)
			}
		}
		var verdict dynamicconfig.FIBVerdict
		if err := decodeStrictJSON(string(row), &verdict); err != nil {
			return nil, fmt.Errorf("Mobility FIB verdict %d: %w", index, err)
		}
		verdicts = append(verdicts, verdict)
	}
	return verdicts, nil
}

func isJSONNull(raw json.RawMessage) bool {
	return bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

func decodeStrictJSON[T any](raw string, value *T) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected second JSON value")
		}
		return err
	}
	return nil
}

// rejectDuplicateJSONKeys walks a JSON document before decoding it into Go
// structs. encoding/json otherwise accepts duplicate object fields with the
// last value winning, which is not an acceptable ambiguity for a persisted
// routing policy.
func rejectDuplicateJSONKeys(raw string) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	if err := walkJSONValue(decoder); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("unexpected second JSON value")
		}
		return err
	}
	return nil
}

func walkJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("JSON object key is not a string")
			}
			if seen[key] {
				return fmt.Errorf("JSON object has duplicate field %q", key)
			}
			seen[key] = true
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim('}') {
			return fmt.Errorf("malformed JSON object")
		}
	case '[':
		for decoder.More() {
			if err := walkJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil {
			return err
		}
		if closing != json.Delim(']') {
			return fmt.Errorf("malformed JSON array")
		}
	default:
		return fmt.Errorf("unexpected JSON delimiter %q", delimiter)
	}
	return nil
}

// DecodeSlice decodes one optional JSON array from a persisted dynamic-config
// payload. Consumers select their own failure policy, while JSON handling
// stays at this persistence boundary.
func DecodeSlice[T any](raw string) ([]T, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var values []T
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}
