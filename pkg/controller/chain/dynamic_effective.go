// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
	"github.com/imksoo/routerd/pkg/hybrid"
	"github.com/imksoo/routerd/pkg/platform"
	"github.com/imksoo/routerd/pkg/sam"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type dynamicConfigPartLister interface {
	ListDynamicConfigParts() ([]routerstate.DynamicConfigPartRecord, error)
}

type dynamicRouteSAMView struct {
	EffectiveRouter *api.Router
	RouteRouter     *api.Router
	HybridLowerings []hybrid.HybridLowering
	SAMLowerings    []sam.DeliveryLowering
}

func buildDynamicRouteSAMView(startup *api.Router, store any, now time.Time, targetOS platform.OS) (dynamicRouteSAMView, error) {
	if startup == nil {
		return dynamicRouteSAMView{}, fmt.Errorf("startup router is required")
	}
	effective := *startup
	if lister, ok := store.(dynamicConfigPartLister); ok {
		records, err := lister.ListDynamicConfigParts()
		if err != nil {
			return dynamicRouteSAMView{}, fmt.Errorf("list dynamic config parts: %w", err)
		}
		if len(records) > 0 {
			parts, err := dynamicConfigPartsFromRecords(records)
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			policies, err := dynamicconfig.ExtractDynamicOverridePolicies(*startup)
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			merged, _, err := dynamicconfig.BuildEffectiveConfig(*startup, parts, policies, now.UTC())
			if err != nil {
				return dynamicRouteSAMView{}, err
			}
			effective = merged
		}
	}

	routeRouter := effective
	hybridLowerings := []hybrid.HybridLowering(nil)
	if hybrid.HasHybridRoutes(&effective) {
		expanded, lowerings, err := hybrid.ExpandHybridRoutes(routeRouter)
		if err != nil {
			return dynamicRouteSAMView{}, err
		}
		routeRouter = expanded
		hybridLowerings = lowerings
	}

	samLowerings := []sam.DeliveryLowering(nil)
	if targetOS == platform.OSLinux && sam.HasRemoteAddressClaims(&effective) {
		expanded, lowerings, err := sam.ExpandRemoteAddressClaimRoutesWithOptions(routeRouter, sam.PlanOptions{StatusReader: statusReaderFromStore(store)})
		if err != nil {
			return dynamicRouteSAMView{}, err
		}
		routeRouter = expanded
		samLowerings = lowerings
	}

	return dynamicRouteSAMView{
		EffectiveRouter: &effective,
		RouteRouter:     &routeRouter,
		HybridLowerings: hybridLowerings,
		SAMLowerings:    samLowerings,
	}, nil
}

func statusReaderFromStore(store any) sam.StatusReader {
	reader, _ := store.(sam.StatusReader)
	return reader
}

func dynamicConfigPartsFromRecords(records []routerstate.DynamicConfigPartRecord) ([]dynamicconfig.DynamicConfigPart, error) {
	parts := make([]dynamicconfig.DynamicConfigPart, 0, len(records))
	for _, record := range records {
		resources, err := decodeDynamicConfigResources(record.ResourcesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d resources: %w", record.Source, record.Generation, err)
		}
		directives, err := decodeDynamicConfigDirectives(record.DirectivesJSON)
		if err != nil {
			return nil, fmt.Errorf("%s generation %d directives: %w", record.Source, record.Generation, err)
		}
		parts = append(parts, dynamicconfig.DynamicConfigPart{
			TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
			Metadata: api.ObjectMeta{
				Name: fmt.Sprintf("%s-%d", record.Source, record.Generation),
			},
			Spec: dynamicconfig.DynamicConfigPartSpec{
				Source:     record.Source,
				Generation: record.Generation,
				ObservedAt: record.ObservedAt,
				ExpiresAt:  record.ExpiresAt,
				Digest:     record.Digest,
				Resources:  resources,
				Directives: directives,
			},
		})
	}
	return parts, nil
}

func decodeDynamicConfigResources(raw string) ([]api.Resource, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var resources []api.Resource
	if err := json.Unmarshal([]byte(raw), &resources); err != nil {
		return nil, err
	}
	return resources, nil
}

func decodeDynamicConfigDirectives(raw string) ([]dynamicconfig.DynamicConfigDirective, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var directives []dynamicconfig.DynamicConfigDirective
	if err := json.Unmarshal([]byte(raw), &directives); err != nil {
		return nil, err
	}
	return directives, nil
}
