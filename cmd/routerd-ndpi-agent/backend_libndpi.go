// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && cgo && libndpi

package main

/*
#cgo pkg-config: libndpi
#include <stdlib.h>
#include <ndpi_api.h>

static struct ndpi_detection_module_struct* routerd_ndpi_init(void) {
	NDPI_PROTOCOL_BITMASK all;
	struct ndpi_detection_module_struct *mod = ndpi_init_detection_module(ndpi_no_prefs);
	if(mod == NULL) return NULL;
	NDPI_BITMASK_SET_ALL(all);
	ndpi_set_protocol_detection_bitmask2(mod, &all);
	ndpi_finalize_initialization(mod);
	return mod;
}

static struct ndpi_flow_struct* routerd_ndpi_new_flow(void) {
	return (struct ndpi_flow_struct*)calloc(1, sizeof(struct ndpi_flow_struct));
}

static ndpi_protocol routerd_ndpi_process(struct ndpi_detection_module_struct *mod,
					  struct ndpi_flow_struct *flow,
					  const unsigned char *packet,
					  unsigned short packetlen,
					  unsigned long long now_ms) {
	return ndpi_detection_process_packet(mod, flow, packet, packetlen, now_ms);
}

static ndpi_protocol routerd_ndpi_giveup(struct ndpi_detection_module_struct *mod,
					 struct ndpi_flow_struct *flow) {
	u_int8_t guessed = 0;
	return ndpi_detection_giveup(mod, flow, 1, &guessed);
}

static int routerd_ndpi_confidence(struct ndpi_flow_struct *flow) {
	return (int)flow->confidence;
}

static const char* routerd_ndpi_hostname(struct ndpi_flow_struct *flow) {
	return flow->host_server_name;
}
*/
import "C"

import (
	"context"
	"encoding/binary"
	"errors"
	"net/netip"
	"strings"
	"sync"
	"time"

	"routerd/pkg/dpi"
)

type libndpiBackend struct {
	mu                  sync.Mutex
	mod                 *C.struct_ndpi_detection_module_struct
	flows               map[string]*C.struct_ndpi_flow_struct
	firstPayloadPackets int
}

func newBackend(opts options) ndpiBackend {
	mod := C.routerd_ndpi_init()
	if mod == nil {
		return initFailedBackend{reason: "libndpi initialization failed"}
	}
	return &libndpiBackend{mod: mod, flows: map[string]*C.struct_ndpi_flow_struct{}, firstPayloadPackets: opts.firstPayloadPackets}
}

func backendExpectedLoaded() bool {
	return true
}

type initFailedBackend struct {
	reason string
}

func (b initFailedBackend) Status() backendStatus {
	return backendStatus{Reason: b.reason}
}

func (b initFailedBackend) Classify(_ context.Context, _ string, req dpi.ClassifyRequest, _ *flowState) (dpi.ClassifyResult, error) {
	result := metadataOnlyResult(req)
	result.Engine = "ndpi-agent"
	result.Source = "ndpi-agent"
	result.Reason = "libndpi_init_failed"
	return result, nil
}

func (b initFailedBackend) Forget(string) {}

func (b initFailedBackend) Close() {}

func (b *libndpiBackend) Status() backendStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mod == nil {
		return backendStatus{Reason: "libndpi backend is closed"}
	}
	return backendStatus{
		Loaded:  true,
		Version: C.GoString(C.ndpi_revision()),
	}
}

func (b *libndpiBackend) Classify(_ context.Context, key string, req dpi.ClassifyRequest, state *flowState) (dpi.ClassifyResult, error) {
	packet, err := packetForNDPI(req)
	if err != nil {
		result := metadataOnlyResult(req)
		result.Engine = "ndpi-agent"
		result.Source = "ndpi-agent"
		result.Reason = "packet_unavailable_for_libndpi"
		return result, nil
	}
	if len(packet) > 0xffff {
		return dpi.ClassifyResult{}, errors.New("packet exceeds libndpi packet length limit")
	}
	cpacket := C.CBytes(packet)
	defer C.free(cpacket)

	nowMS := uint64(time.Now().UnixMilli())
	if state != nil && !state.lastSeen.IsZero() {
		nowMS = uint64(state.lastSeen.UnixMilli())
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.mod == nil {
		return dpi.ClassifyResult{}, errors.New("libndpi backend is closed")
	}
	flow := b.flows[key]
	if flow == nil {
		flow = C.routerd_ndpi_new_flow()
		if flow == nil {
			return dpi.ClassifyResult{}, errors.New("libndpi flow allocation failed")
		}
		b.flows[key] = flow
	}
	proto := C.routerd_ndpi_process(b.mod, flow, (*C.uchar)(cpacket), C.ushort(len(packet)), C.ulonglong(nowMS))
	if state != nil && state.packets >= b.firstPayloadPackets && proto.app_protocol == C.NDPI_PROTOCOL_UNKNOWN && proto.master_protocol == C.NDPI_PROTOCOL_UNKNOWN {
		proto = C.routerd_ndpi_giveup(b.mod, flow)
	}
	return b.resultFromProtocol(req, flow, proto), nil
}

func (b *libndpiBackend) Forget(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	flow := b.flows[key]
	if flow == nil {
		return
	}
	C.ndpi_free_flow(flow)
	delete(b.flows, key)
}

func (b *libndpiBackend) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for key, flow := range b.flows {
		C.ndpi_free_flow(flow)
		delete(b.flows, key)
	}
	if b.mod != nil {
		C.ndpi_exit_detection_module(b.mod)
		b.mod = nil
	}
}

func (b *libndpiBackend) resultFromProtocol(req dpi.ClassifyRequest, flow *C.struct_ndpi_flow_struct, proto C.ndpi_protocol) dpi.ClassifyResult {
	result := metadataOnlyResult(req)
	result.Engine = "ndpi-agent"
	result.Source = "ndpi-agent"
	masterProto := proto.master_protocol
	appProto := proto.app_protocol
	if masterProto != C.NDPI_PROTOCOL_UNKNOWN {
		result.MasterProtocol = strings.ToLower(C.GoString(C.ndpi_get_proto_name(b.mod, C.u_int16_t(masterProto))))
	}
	if appProto != C.NDPI_PROTOCOL_UNKNOWN {
		result.ApplicationProtocol = strings.ToLower(C.GoString(C.ndpi_get_proto_name(b.mod, C.u_int16_t(appProto))))
	}
	detectedProto := appProto
	if detectedProto == C.NDPI_PROTOCOL_UNKNOWN {
		detectedProto = masterProto
	}
	if detectedProto == C.NDPI_PROTOCOL_UNKNOWN {
		result.Reason = "libndpi_unknown"
		return dpi.FinalizeResult(result)
	}
	result.DetectedProtocol = strings.ToLower(C.GoString(C.ndpi_get_proto_name(b.mod, C.u_int16_t(detectedProto))))
	result.AppName = result.DetectedProtocol
	result.AppCategory = strings.ToLower(C.GoString(C.ndpi_category_get_name(b.mod, C.ndpi_get_proto_category(b.mod, proto))))
	result.AppConfidence = confidencePercent(int(C.routerd_ndpi_confidence(flow)))
	host := C.GoString(C.routerd_ndpi_hostname(flow))
	if host != "" {
		switch result.AppName {
		case "tls", "quic":
			result.TLSSNI = host
		case "http":
			result.HTTPHost = host
		default:
			result.TLSSNI = host
		}
	}
	result.Reason = "libndpi_protocol_detection"
	return dpi.FinalizeResult(result)
}

func confidencePercent(confidence int) int {
	switch confidence {
	case int(C.NDPI_CONFIDENCE_DPI):
		return 95
	case int(C.NDPI_CONFIDENCE_DPI_CACHE):
		return 85
	case int(C.NDPI_CONFIDENCE_MATCH_BY_IP):
		return 65
	case int(C.NDPI_CONFIDENCE_MATCH_BY_PORT):
		return 45
	default:
		return 0
	}
}

func packetForNDPI(req dpi.ClassifyRequest) ([]byte, error) {
	if len(req.Packet) > 0 {
		return append([]byte(nil), req.Packet...), nil
	}
	meta := dpi.Classify(req)
	src, err := netip.ParseAddr(meta.SrcAddress)
	if err != nil {
		return nil, err
	}
	dst, err := netip.ParseAddr(meta.DstAddress)
	if err != nil {
		return nil, err
	}
	if !src.Is4() || !dst.Is4() {
		return nil, errors.New("cannot synthesize non-IPv4 packet")
	}
	payload := req.L4Payload
	if len(payload) == 0 {
		payload = req.Payload
	}
	switch strings.ToLower(meta.TransportProtocol) {
	case "tcp":
		return synthesizeIPv4TCP(src, dst, meta.SrcPort, meta.DstPort, payload), nil
	case "udp":
		return synthesizeIPv4UDP(src, dst, meta.SrcPort, meta.DstPort, payload), nil
	default:
		return nil, errors.New("unsupported transport protocol")
	}
}

func synthesizeIPv4TCP(src, dst netip.Addr, srcPort, dstPort int, payload []byte) []byte {
	l4Len := 20 + len(payload)
	packet := make([]byte, 20+l4Len)
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = 6
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], src.AsSlice())
	copy(packet[16:20], dst.AsSlice())
	binary.BigEndian.PutUint16(packet[20:22], uint16(srcPort))
	binary.BigEndian.PutUint16(packet[22:24], uint16(dstPort))
	packet[32] = 0x50
	packet[33] = 0x18
	copy(packet[40:], payload)
	return packet
}

func synthesizeIPv4UDP(src, dst netip.Addr, srcPort, dstPort int, payload []byte) []byte {
	l4Len := 8 + len(payload)
	packet := make([]byte, 20+l4Len)
	packet[0] = 0x45
	packet[8] = 64
	packet[9] = 17
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[12:16], src.AsSlice())
	copy(packet[16:20], dst.AsSlice())
	binary.BigEndian.PutUint16(packet[20:22], uint16(srcPort))
	binary.BigEndian.PutUint16(packet[22:24], uint16(dstPort))
	binary.BigEndian.PutUint16(packet[24:26], uint16(l4Len))
	copy(packet[28:], payload)
	return packet
}
