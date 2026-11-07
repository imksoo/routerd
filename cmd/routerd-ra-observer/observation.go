// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"encoding/json"
	"time"

	"github.com/imksoo/routerd/pkg/raobserver"
)

// This is recent diagnostic history, not forwarding or prefix authority. A
// quiet router remains visible for ten minutes regardless of its router lifetime.
const routerObservationTTL = 10 * time.Minute
const maxObservedRouters = 64
const maxObservationBytes = 3072

func (d *daemon) now() time.Time {
	if d.clock != nil {
		return d.clock().UTC()
	}
	return time.Now().UTC()
}

func (d *daemon) expireRoutersLocked(now time.Time) {
	for key, router := range d.routers {
		if !now.Before(router.LastSeen.Add(routerObservationTTL)) {
			delete(d.routers, key)
			d.historyExpired++
		}
	}
}

func (d *daemon) evictOldestRouterLocked() {
	oldest := ""
	for key, router := range d.routers {
		if oldest == "" || router.LastSeen.Before(d.routers[oldest].LastSeen) || router.LastSeen.Equal(d.routers[oldest].LastSeen) && key < oldest {
			oldest = key
		}
	}
	delete(d.routers, oldest)
	d.historyEvicted++
}

func advertisementItems(adv raobserver.Advertisement) int {
	n := len(adv.Prefixes) + len(adv.Routes) + len(adv.RDNSS)
	for _, option := range adv.RDNSS {
		n += len(option.Servers)
	}
	return n
}

// Parsed fixed fields are bounded by native MAC/IPv6 widths. Cap independent
// options before measuring JSON, then trim whole options to the external status
// budget. The status embeds this JSON string twice; 64 records of 3072 bytes,
// including worst-case escaping, leave room beneath its 1 MiB reader limit.
func boundedAdvertisement(adv raobserver.Advertisement) (raobserver.Advertisement, int) {
	original := advertisementItems(adv)
	adv.Prefixes = append([]raobserver.PrefixOption(nil), adv.Prefixes[:min(len(adv.Prefixes), 16)]...)
	adv.Routes = append([]raobserver.RouteOption(nil), adv.Routes[:min(len(adv.Routes), 16)]...)
	adv.RDNSS = append([]raobserver.RDNSSOption(nil), adv.RDNSS[:min(len(adv.RDNSS), 4)]...)
	for i := range adv.RDNSS {
		servers := adv.RDNSS[i].Servers
		adv.RDNSS[i].Servers = append([]string(nil), servers[:min(len(servers), 16)]...)
	}
	for {
		observation := raobserver.UpdateObservation(raobserver.RouterObservation{}, adv, time.Date(9999, 12, 31, 23, 59, 59, 999999999, time.UTC))
		observation.Count = ^uint64(0)
		encoded, _ := json.Marshal(observation)
		if len(encoded) <= maxObservationBytes {
			break
		}
		switch {
		case len(adv.RDNSS) > 0:
			adv.RDNSS = adv.RDNSS[:len(adv.RDNSS)-1]
		case len(adv.Routes) > 0:
			adv.Routes = adv.Routes[:len(adv.Routes)-1]
		case len(adv.Prefixes) > 0:
			adv.Prefixes = adv.Prefixes[:len(adv.Prefixes)-1]
		default:
			return adv, original - advertisementItems(adv)
		}
	}
	return adv, original - advertisementItems(adv)
}
