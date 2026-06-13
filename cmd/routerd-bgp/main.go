// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"syscall"

	gobgpapi "github.com/osrg/gobgp/v3/api"
	gobgpserver "github.com/osrg/gobgp/v3/pkg/server"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/imksoo/routerd/pkg/bgpdaemon"
	"github.com/imksoo/routerd/pkg/version"
)

const defaultSocketPath = "/run/routerd/bgp/gobgp.sock"
const defaultControlSocketPath = "/run/routerd/bgp/control.sock"
const defaultStatePath = "/var/lib/routerd/bgp/applied.json"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	command := "daemon"
	if len(args) > 0 && args[0] == "version" {
		fmt.Println(version.String())
		return nil
	}
	if len(args) > 0 && args[0] != "daemon" {
		return fmt.Errorf("unknown command %q", args[0])
	}
	if len(args) > 0 {
		args = args[1:]
	}
	fs := flag.NewFlagSet(command, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	socketPath := fs.String("socket", defaultSocketPath, "GoBGP gRPC Unix socket path")
	controlSocketPath := fs.String("control-socket", defaultControlSocketPath, "routerd-bgp control Unix socket path")
	statePath := fs.String("state-file", defaultStatePath, "applied BGP state JSON path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *socketPath == "" {
		return fmt.Errorf("--socket is required")
	}
	if *controlSocketPath == "" {
		return fmt.Errorf("--control-socket is required")
	}
	if *statePath == "" {
		return fmt.Errorf("--state-file is required")
	}
	if err := os.MkdirAll(filepath.Dir(*socketPath), 0755); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*controlSocketPath), 0755); err != nil {
		return fmt.Errorf("create control socket directory: %w", err)
	}
	_ = os.Remove(*socketPath)
	_ = os.Remove(*controlSocketPath)

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	server := gobgpserver.NewBgpServer(gobgpserver.GrpcListenAddress("unix://" + *socketPath))
	go server.Serve()
	if err := restoreApplied(context.Background(), server, *statePath, logger); err != nil {
		return err
	}
	control, err := serveControlSocket(*controlSocketPath, *statePath, server)
	if err != nil {
		return err
	}
	logger.Info("routerd-bgp daemon started", "socket", *socketPath, "controlSocket", *controlSocketPath, "stateFile", *statePath, "version", version.String())

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	logger.Info("routerd-bgp daemon stopping")
	_ = control.Shutdown(context.Background())
	server.Stop()
	_ = os.Remove(*socketPath)
	_ = os.Remove(*controlSocketPath)
	return nil
}

type pathServer interface {
	AddPath(context.Context, *gobgpapi.AddPathRequest) (*gobgpapi.AddPathResponse, error)
	DeletePath(context.Context, *gobgpapi.DeletePathRequest) error
}

type policyPathServer interface {
	pathServer
	SetPolicies(context.Context, *gobgpapi.SetPoliciesRequest) error
	SetPolicyAssignment(context.Context, *gobgpapi.SetPolicyAssignmentRequest) error
	ResetPeer(context.Context, *gobgpapi.ResetPeerRequest) error
}

func serveControlSocket(socketPath, statePath string, paths pathServer) (*http.Server, error) {
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen control socket: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/applied", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			config, _, err := bgpdaemon.ReadApplied(statePath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, config)
		case http.MethodPut:
			var config bgpdaemon.AppliedConfig
			if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := bgpdaemon.Validate(config); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := bgpdaemon.WriteApplied(statePath, config); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, bgpdaemon.Normalize(config))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v1/paths", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			config, _, err := bgpdaemon.ReadApplied(statePath)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			source := strings.TrimSpace(r.URL.Query().Get("source"))
			out := config.Paths
			if source != "" {
				out = nil
				for _, path := range config.Paths {
					if path.Source == source {
						out = append(out, path)
					}
				}
			}
			writeJSON(w, out)
		case http.MethodPost:
			path, err := decodePathRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			applied, updated, err := upsertDynamicPath(r.Context(), paths, statePath, path)
			if err != nil {
				http.Error(w, err.Error(), httpStatusForPathError(err))
				return
			}
			if updated == nil {
				writeJSON(w, applied)
				return
			}
			writeJSON(w, updated)
		case http.MethodDelete:
			path, err := decodeDeletePathRequest(r)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			config, err := deleteDynamicPath(r.Context(), paths, statePath, path)
			if err != nil {
				http.Error(w, err.Error(), httpStatusForPathError(err))
				return
			}
			writeJSON(w, config)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	server := &http.Server{Handler: mux}
	go func() {
		if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "routerd-bgp control socket failed: %v\n", err)
		}
	}()
	return server, nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func restoreApplied(ctx context.Context, server *gobgpserver.BgpServer, statePath string, logger *slog.Logger) error {
	applied, ok, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		return fmt.Errorf("read applied BGP state: %w", err)
	}
	if !ok {
		return nil
	}
	if err := bgpdaemon.Validate(applied); err != nil {
		return fmt.Errorf("validate applied BGP state: %w", err)
	}
	if err := server.StartBgp(ctx, &gobgpapi.StartBgpRequest{Global: appliedGlobal(applied.Global)}); err != nil {
		return fmt.Errorf("restore BGP global: %w", err)
	}
	logger.Info("loaded applied BGP state; controller will restore peers and paths", "peers", len(applied.Peers), "paths", len(applied.Paths), "advertisements", len(applied.Advertisements), "hash", bgpdaemon.Hash(applied))
	return nil
}

func applyAppliedPolicies(ctx context.Context, server policyPathServer, applied bgpdaemon.AppliedConfig) error {
	policies, assignment := appliedPolicies(applied)
	if len(assignment.GetPolicies()) == 0 {
		if err := server.SetPolicyAssignment(ctx, &gobgpapi.SetPolicyAssignmentRequest{Assignment: assignment}); err != nil {
			return fmt.Errorf("apply BGP import policy assignment: %w", err)
		}
	}
	if err := server.SetPolicies(ctx, policies); err != nil {
		return fmt.Errorf("apply BGP policies: %w", err)
	}
	if len(assignment.GetPolicies()) > 0 {
		if err := server.SetPolicyAssignment(ctx, &gobgpapi.SetPolicyAssignmentRequest{Assignment: assignment}); err != nil {
			return fmt.Errorf("apply BGP import policy assignment: %w", err)
		}
	}
	return nil
}

func restoreAppliedPaths(ctx context.Context, server pathServer, applied *bgpdaemon.AppliedConfig) error {
	if applied == nil {
		return nil
	}
	normalized := bgpdaemon.Normalize(*applied)
	for i, appliedPath := range normalized.Paths {
		path, err := pathFromAppliedPath(appliedPath)
		if err != nil {
			return fmt.Errorf("restore BGP path %s/%s: %w", appliedPath.Source, appliedPath.Prefix, err)
		}
		resp, err := server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_GLOBAL, Path: path})
		if err != nil {
			return fmt.Errorf("restore BGP path %s/%s: %w", appliedPath.Source, appliedPath.Prefix, err)
		}
		normalized.Paths[i].UUID = bgpdaemon.EncodeUUID(resp.GetUuid())
	}
	*applied = bgpdaemon.Normalize(normalized)
	return nil
}

func appliedGlobal(global bgpdaemon.AppliedGlobal) *gobgpapi.Global {
	out := &gobgpapi.Global{
		Asn:              global.ASN,
		RouterId:         global.RouterID,
		ListenPort:       int32(global.ListenPort),
		ListenAddresses:  global.ListenAddresses,
		UseMultiplePaths: global.UseMultiplePaths,
	}
	for _, family := range global.Families {
		switch family {
		case "ipv6-unicast":
			out.Families = append(out.Families, 1)
		default:
			out.Families = append(out.Families, 0)
		}
	}
	if len(out.Families) == 0 {
		out.Families = []uint32{0}
	}
	if gr := global.GracefulRestart; gr != nil && gr.Enabled {
		out.GracefulRestart = &gobgpapi.GracefulRestart{Enabled: true, RestartTime: gr.RestartTime, StaleRoutesTime: gr.StaleRoutesTime}
	}
	return out
}

func appliedPeer(peer bgpdaemon.AppliedPeer, global bgpdaemon.AppliedGlobal) *gobgpapi.Peer {
	peerType := gobgpapi.PeerType_EXTERNAL
	if global.ASN != 0 && peer.ASN == global.ASN {
		peerType = gobgpapi.PeerType_INTERNAL
	}
	out := &gobgpapi.Peer{
		Conf: &gobgpapi.PeerConf{
			NeighborAddress: peer.Address,
			PeerAsn:         peer.ASN,
			AuthPassword:    peer.Password,
			Type:            peerType,
			SendCommunity:   3,
		},
		Timers: &gobgpapi.Timers{Config: timers(peer.TimersProfile)},
		AfiSafis: []*gobgpapi.AfiSafi{
			afiSafi(ipv4Family()),
			afiSafi(ipv6Family()),
		},
	}
	if gr := peer.GracefulRestart; gr != nil && gr.Enabled {
		out.GracefulRestart = &gobgpapi.GracefulRestart{Enabled: true, RestartTime: gr.RestartTime, StaleRoutesTime: gr.StaleRoutesTime}
	}
	if peer.EbgpMultihop > 1 {
		out.EbgpMultihop = &gobgpapi.EbgpMultihop{Enabled: true, MultihopTtl: uint32(peer.EbgpMultihop)}
	}
	if peer.RouteReflectorClient {
		out.RouteReflector = &gobgpapi.RouteReflector{
			RouteReflectorClient:    true,
			RouteReflectorClusterId: strings.TrimSpace(peer.RouteReflectorClusterID),
		}
	}
	applyPolicy := &gobgpapi.ApplyPolicy{}
	if len(appliedPolicyPrefixes(peer.ImportPolicy)) > 0 && strings.TrimSpace(peer.ImportPolicyName) != "" {
		applyPolicy.ImportPolicy = &gobgpapi.PolicyAssignment{
			Name:          strings.TrimSpace(peer.Address),
			Direction:     gobgpapi.PolicyDirection_IMPORT,
			DefaultAction: gobgpapi.RouteAction_REJECT,
			Policies: []*gobgpapi.Policy{{
				Name: strings.TrimSpace(peer.ImportPolicyName),
			}},
		}
	}
	if len(appliedExportPolicyPrefixes(peer.ExportPolicy)) > 0 && strings.TrimSpace(peer.ExportPolicyName) != "" {
		applyPolicy.ExportPolicy = &gobgpapi.PolicyAssignment{
			Name:          strings.TrimSpace(peer.Address),
			Direction:     gobgpapi.PolicyDirection_EXPORT,
			DefaultAction: gobgpapi.RouteAction_REJECT,
			Policies: []*gobgpapi.Policy{{
				Name: strings.TrimSpace(peer.ExportPolicyName),
			}},
		}
	}
	if applyPolicy.ImportPolicy != nil || applyPolicy.ExportPolicy != nil {
		out.ApplyPolicy = applyPolicy
	}
	return out
}

func timers(profile string) *gobgpapi.TimersConfig {
	switch profile {
	case "fast":
		return &gobgpapi.TimersConfig{ConnectRetry: 1, HoldTime: 9, KeepaliveInterval: 3, IdleHoldTimeAfterReset: 1}
	case "slow":
		return &gobgpapi.TimersConfig{ConnectRetry: 30, HoldTime: 180, KeepaliveInterval: 60, IdleHoldTimeAfterReset: 5}
	default:
		return &gobgpapi.TimersConfig{ConnectRetry: 10, HoldTime: 90, KeepaliveInterval: 30, IdleHoldTimeAfterReset: 1}
	}
}

func afiSafi(family *gobgpapi.Family) *gobgpapi.AfiSafi {
	return &gobgpapi.AfiSafi{
		Config: &gobgpapi.AfiSafiConfig{Family: family, Enabled: true},
		UseMultiplePaths: &gobgpapi.UseMultiplePaths{
			Config: &gobgpapi.UseMultiplePathsConfig{Enabled: true},
			Ebgp:   &gobgpapi.Ebgp{Config: &gobgpapi.EbgpConfig{MaximumPaths: 16}},
		},
	}
}

func appliedPolicies(config bgpdaemon.AppliedConfig) (*gobgpapi.SetPoliciesRequest, *gobgpapi.PolicyAssignment) {
	req := &gobgpapi.SetPoliciesRequest{}
	assignment := &gobgpapi.PolicyAssignment{
		Name:          "global",
		Direction:     gobgpapi.PolicyDirection_IMPORT,
		DefaultAction: gobgpapi.RouteAction_ACCEPT,
	}
	globalImportName := "routerd-restore-import"
	seenImportPolicies := map[string]bool{}
	peerImportPolicies := appliedImportPolicies(config)
	globalImportPolicy := config.Global.ImportPolicy
	if len(mergeStringSets(globalImportPolicy.AllowedPrefixes)) > 0 {
		globalImportPolicy.AllowedPrefixes = mergeStringSets(globalImportPolicy.AllowedPrefixes, appliedDynamicPathPrefixes(config.Paths))
	}
	if len(appliedPolicyPrefixes(globalImportPolicy)) > 0 {
		appendAppliedImportPolicy(req, globalImportName, globalImportName+"-prefixes", globalImportPolicy)
		if len(peerImportPolicies) == 0 {
			assignment.DefaultAction = gobgpapi.RouteAction_REJECT
			assignment.Policies = append(assignment.Policies, &gobgpapi.Policy{Name: globalImportName})
		}
		seenImportPolicies[globalImportName] = true
	}
	for _, policy := range peerImportPolicies {
		if seenImportPolicies[policy.Name] {
			continue
		}
		appendAppliedImportPolicy(req, policy.Name, policy.Name+"-prefixes", policy.Spec)
		seenImportPolicies[policy.Name] = true
	}
	for _, policy := range appliedExportPolicies(config) {
		prefixes := appliedExportPolicyPrefixes(policy.Spec)
		if len(prefixes) == 0 {
			continue
		}
		prefixSetName := policy.Name + "-prefixes"
		req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
			DefinedType: gobgpapi.DefinedType_PREFIX,
			Name:        prefixSetName,
			Prefixes:    prefixes,
		})
		req.Policies = append(req.Policies, &gobgpapi.Policy{
			Name: policy.Name,
			Statements: []*gobgpapi.Statement{{
				Name: appliedPolicyStatementName(policy.Name, "allow-export"),
				Conditions: &gobgpapi.Conditions{PrefixSet: &gobgpapi.MatchSet{
					Type: gobgpapi.MatchSet_ANY,
					Name: prefixSetName,
				}},
				Actions: &gobgpapi.Actions{RouteAction: gobgpapi.RouteAction_ACCEPT},
			}},
		})
	}
	return req, assignment
}

func appendAppliedImportPolicy(req *gobgpapi.SetPoliciesRequest, policyName, prefixSetName string, spec bgpdaemon.AppliedImportPolicy) {
	prefixes := appliedPolicyPrefixes(spec)
	if len(prefixes) == 0 || strings.TrimSpace(policyName) == "" || strings.TrimSpace(prefixSetName) == "" {
		return
	}
	req.DefinedSets = append(req.DefinedSets, &gobgpapi.DefinedSet{
		DefinedType: gobgpapi.DefinedType_PREFIX,
		Name:        strings.TrimSpace(prefixSetName),
		Prefixes:    prefixes,
	})
	req.Policies = append(req.Policies, &gobgpapi.Policy{
		Name: strings.TrimSpace(policyName),
		Statements: []*gobgpapi.Statement{{
			Name: appliedPolicyStatementName(policyName, "allow-import"),
			Conditions: &gobgpapi.Conditions{PrefixSet: &gobgpapi.MatchSet{
				Type: gobgpapi.MatchSet_ANY,
				Name: strings.TrimSpace(prefixSetName),
			}},
			Actions: &gobgpapi.Actions{
				RouteAction: gobgpapi.RouteAction_ACCEPT,
				Nexthop:     appliedNextHopAction(spec),
			},
		}},
	})
}

type appliedImportPolicy struct {
	Name string
	Spec bgpdaemon.AppliedImportPolicy
}

func appliedImportPolicies(config bgpdaemon.AppliedConfig) []appliedImportPolicy {
	byName := map[string]bgpdaemon.AppliedImportPolicy{}
	dynamicPrefixes := appliedDynamicPathPrefixes(config.Paths)
	for _, peer := range config.Peers {
		name := strings.TrimSpace(peer.ImportPolicyName)
		if name == "" {
			name = "routerd-restore-import"
		}
		spec := peer.ImportPolicy
		if len(spec.AllowedPrefixes) == 0 {
			spec = config.Global.ImportPolicy
		}
		spec.AllowedPrefixes = mergeStringSets(spec.AllowedPrefixes, dynamicPrefixes)
		if len(appliedPolicyPrefixes(spec)) > 0 {
			byName[name] = spec
		}
	}
	if len(byName) == 0 && len(appliedPolicyPrefixes(config.Global.ImportPolicy)) > 0 {
		spec := config.Global.ImportPolicy
		spec.AllowedPrefixes = mergeStringSets(spec.AllowedPrefixes, dynamicPrefixes)
		byName["routerd-restore-import"] = spec
	}
	var out []string
	for name := range byName {
		out = append(out, name)
	}
	sort.Strings(out)
	policies := make([]appliedImportPolicy, 0, len(out))
	for _, name := range out {
		policies = append(policies, appliedImportPolicy{Name: name, Spec: byName[name]})
	}
	return policies
}

func appliedDynamicPathPrefixes(paths []bgpdaemon.AppliedPath) []string {
	var out []string
	for _, path := range bgpdaemon.Normalize(bgpdaemon.AppliedConfig{Paths: paths}).Paths {
		if path.Source == bgpdaemon.AppliedPathSourceStatic {
			continue
		}
		if strings.TrimSpace(path.Prefix) == "" {
			continue
		}
		out = append(out, path.Prefix)
	}
	return mergeStringSets(out)
}

func mergeStringSets(groups ...[]string) []string {
	seen := map[string]bool{}
	var out []string
	for _, group := range groups {
		for _, value := range group {
			value = strings.TrimSpace(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			out = append(out, value)
		}
	}
	sort.Strings(out)
	return out
}

type appliedExportPolicy struct {
	Name string
	Spec bgpdaemon.AppliedExportPolicy
}

func appliedExportPolicies(config bgpdaemon.AppliedConfig) []appliedExportPolicy {
	byName := map[string]bgpdaemon.AppliedExportPolicy{}
	dynamicPrefixes := appliedDynamicPathPrefixes(config.Paths)
	for _, peer := range config.Peers {
		name := strings.TrimSpace(peer.ExportPolicyName)
		if name == "" {
			continue
		}
		spec := peer.ExportPolicy
		spec.AllowedPrefixes = mergeStringSets(spec.AllowedPrefixes, dynamicPrefixes)
		if len(appliedExportPolicyPrefixes(spec)) == 0 {
			continue
		}
		byName[name] = spec
	}
	var names []string
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	policies := make([]appliedExportPolicy, 0, len(names))
	for _, name := range names {
		policies = append(policies, appliedExportPolicy{Name: name, Spec: byName[name]})
	}
	return policies
}

func appliedPolicyStatementName(policyName, suffix string) string {
	return strings.TrimSpace(policyName) + "-" + suffix
}

func appliedPolicyPrefixes(spec bgpdaemon.AppliedImportPolicy) []*gobgpapi.Prefix {
	var out []*gobgpapi.Prefix
	for _, value := range spec.AllowedPrefixes {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(value))
		if err != nil {
			continue
		}
		prefix = prefix.Masked()
		bits := uint32(prefix.Bits())
		out = append(out, &gobgpapi.Prefix{IpPrefix: prefix.String(), MaskLengthMin: bits, MaskLengthMax: appliedPrefixMaxLength(prefix)})
	}
	return out
}

func appliedExportPolicyPrefixes(spec bgpdaemon.AppliedExportPolicy) []*gobgpapi.Prefix {
	return appliedPolicyPrefixes(bgpdaemon.AppliedImportPolicy{AllowedPrefixes: spec.AllowedPrefixes})
}

func appliedPrefixMaxLength(prefix netip.Prefix) uint32 {
	if prefix.Addr().Is6() {
		return 128
	}
	return 32
}

func appliedNextHopAction(spec bgpdaemon.AppliedImportPolicy) *gobgpapi.NexthopAction {
	if strings.TrimSpace(spec.NextHopRewrite) == "unchanged" {
		return &gobgpapi.NexthopAction{Unchanged: true}
	}
	return &gobgpapi.NexthopAction{PeerAddress: true}
}

func decodePathRequest(r *http.Request) (bgpdaemon.AppliedPath, error) {
	defer r.Body.Close()
	var path bgpdaemon.AppliedPath
	if err := json.NewDecoder(r.Body).Decode(&path); err != nil {
		return bgpdaemon.AppliedPath{}, err
	}
	return validateDynamicMobilityPath(path)
}

func decodeDeletePathRequest(r *http.Request) (bgpdaemon.AppliedPath, error) {
	path := bgpdaemon.AppliedPath{
		Source: strings.TrimSpace(r.URL.Query().Get("source")),
		Prefix: strings.TrimSpace(r.URL.Query().Get("prefix")),
	}
	if path.Source == "" && path.Prefix == "" && r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&path)
	}
	return validateDynamicMobilityPath(path)
}

func validateDynamicMobilityPath(path bgpdaemon.AppliedPath) (bgpdaemon.AppliedPath, error) {
	path = bgpdaemon.NormalizeAppliedPath(path)
	if !bgpdaemon.IsMobilityPathSource(path.Source) {
		return bgpdaemon.AppliedPath{}, fmt.Errorf("dynamic BGP path source %q is not a MobilityPool source", path.Source)
	}
	prefix, err := netip.ParsePrefix(path.Prefix)
	if err != nil {
		return bgpdaemon.AppliedPath{}, fmt.Errorf("dynamic BGP path prefix: %w", err)
	}
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() || prefix.Bits() != 32 {
		return bgpdaemon.AppliedPath{}, fmt.Errorf("dynamic mobility BGP paths must be IPv4 /32, got %s", prefix.String())
	}
	path.Prefix = prefix.String()
	path.Family = bgpdaemon.AppliedPathFamilyIPv4Unicast
	if err := bgpdaemon.ValidateAppliedPath(path); err != nil {
		return bgpdaemon.AppliedPath{}, err
	}
	return path, nil
}

func upsertDynamicPath(ctx context.Context, server pathServer, statePath string, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, *bgpdaemon.AppliedPath, error) {
	applied, ok, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	if !ok {
		return bgpdaemon.AppliedConfig{}, nil, fmt.Errorf("applied BGP config is not initialized")
	}
	if err := bgpdaemon.Validate(applied); err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	path, err = validateDynamicMobilityPath(path)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	key := bgpdaemon.AppliedPathKey(path)
	for i, existing := range applied.Paths {
		if bgpdaemon.AppliedPathKey(existing) != key {
			continue
		}
		if reflect.DeepEqual(existing.Attrs, path.Attrs) && existing.UUID != "" {
			return applied, &applied.Paths[i], nil
		}
		if uuid, err := bgpdaemon.DecodeUUID(existing.UUID); err == nil && len(uuid) > 0 {
			if err := server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_GLOBAL, Uuid: uuid}); err != nil {
				if !isMissingGoBGPPath(err) {
					return bgpdaemon.AppliedConfig{}, nil, err
				}
			}
		}
		applied.Paths = append(applied.Paths[:i], applied.Paths[i+1:]...)
		break
	}
	reqPath, err := pathFromAppliedPath(path)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	resp, err := server.AddPath(ctx, &gobgpapi.AddPathRequest{TableType: gobgpapi.TableType_GLOBAL, Path: reqPath})
	if err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	path.UUID = bgpdaemon.EncodeUUID(resp.GetUuid())
	applied.Paths = append(applied.Paths, path)
	applied = bgpdaemon.Normalize(applied)
	if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	if err := refreshDynamicPathPolicies(ctx, server, applied); err != nil {
		return bgpdaemon.AppliedConfig{}, nil, err
	}
	for i := range applied.Paths {
		if bgpdaemon.AppliedPathKey(applied.Paths[i]) == key {
			return applied, &applied.Paths[i], nil
		}
	}
	return applied, nil, nil
}

func deleteDynamicPath(ctx context.Context, server pathServer, statePath string, path bgpdaemon.AppliedPath) (bgpdaemon.AppliedConfig, error) {
	applied, ok, err := bgpdaemon.ReadApplied(statePath)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	if !ok {
		return bgpdaemon.AppliedConfig{}, fmt.Errorf("applied BGP config is not initialized")
	}
	if err := bgpdaemon.Validate(applied); err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	path, err = validateDynamicMobilityPath(path)
	if err != nil {
		return bgpdaemon.AppliedConfig{}, err
	}
	key := bgpdaemon.AppliedPathKey(path)
	for i, existing := range applied.Paths {
		if bgpdaemon.AppliedPathKey(existing) != key {
			continue
		}
		if uuid, err := bgpdaemon.DecodeUUID(existing.UUID); err == nil && len(uuid) > 0 {
			if err := server.DeletePath(ctx, &gobgpapi.DeletePathRequest{TableType: gobgpapi.TableType_GLOBAL, Uuid: uuid}); err != nil {
				if !isMissingGoBGPPath(err) {
					return bgpdaemon.AppliedConfig{}, err
				}
			}
		}
		applied.Paths = append(applied.Paths[:i], applied.Paths[i+1:]...)
		applied = bgpdaemon.Normalize(applied)
		if err := bgpdaemon.WriteApplied(statePath, applied); err != nil {
			return bgpdaemon.AppliedConfig{}, err
		}
		if err := refreshDynamicPathPolicies(ctx, server, applied); err != nil {
			return bgpdaemon.AppliedConfig{}, err
		}
		return applied, nil
	}
	return applied, nil
}

func refreshDynamicPathPolicies(ctx context.Context, server pathServer, applied bgpdaemon.AppliedConfig) error {
	policyServer, ok := server.(policyPathServer)
	if !ok {
		return nil
	}
	if err := applyAppliedPolicies(ctx, policyServer, applied); err != nil {
		return err
	}
	for _, address := range dynamicExportPolicyPeerAddresses(applied) {
		if err := policyServer.ResetPeer(ctx, &gobgpapi.ResetPeerRequest{
			Address:   address,
			Soft:      true,
			Direction: gobgpapi.ResetPeerRequest_OUT,
		}); err != nil {
			return fmt.Errorf("soft reset export policy for peer %s: %w", address, err)
		}
	}
	return nil
}

func dynamicExportPolicyPeerAddresses(applied bgpdaemon.AppliedConfig) []string {
	applied = bgpdaemon.Normalize(applied)
	if len(appliedDynamicPathPrefixes(applied.Paths)) == 0 {
		return nil
	}
	var addresses []string
	for address, peer := range applied.Peers {
		if strings.TrimSpace(peer.ExportPolicyName) == "" {
			continue
		}
		peerAddress := strings.TrimSpace(peer.Address)
		if peerAddress == "" {
			peerAddress = strings.TrimSpace(address)
		}
		if peerAddress == "" {
			continue
		}
		addresses = append(addresses, peerAddress)
	}
	sort.Strings(addresses)
	return addresses
}

func isMissingGoBGPPath(err error) bool {
	return err != nil && strings.Contains(err.Error(), "can't find a specified path")
}

func httpStatusForPathError(err error) int {
	if err == nil {
		return http.StatusOK
	}
	msg := err.Error()
	if strings.Contains(msg, "not initialized") {
		return http.StatusConflict
	}
	return http.StatusBadRequest
}

func localPath(prefix string) (*gobgpapi.Path, error) {
	return pathFromAppliedPath(bgpdaemon.StaticAppliedPath(prefix, nil))
}

func pathFromAppliedPath(appliedPath bgpdaemon.AppliedPath) (*gobgpapi.Path, error) {
	appliedPath = bgpdaemon.NormalizeAppliedPath(appliedPath)
	parsed, err := netip.ParsePrefix(appliedPath.Prefix)
	if err != nil {
		return nil, err
	}
	parsed = parsed.Masked()
	nlri, err := anypb.New(&gobgpapi.IPAddressPrefix{Prefix: parsed.Addr().String(), PrefixLen: uint32(parsed.Bits())})
	if err != nil {
		return nil, err
	}
	origin, err := anypb.New(&gobgpapi.OriginAttribute{Origin: 0})
	if err != nil {
		return nil, err
	}
	nextHop := "0.0.0.0"
	if parsed.Addr().Is6() {
		nextHop = "::"
	}
	if appliedPath.Attrs.NextHop != "" {
		nextHop = appliedPath.Attrs.NextHop
	}
	nh, err := anypb.New(&gobgpapi.NextHopAttribute{NextHop: nextHop})
	if err != nil {
		return nil, err
	}
	attrs := []*anypb.Any{origin, nh}
	if appliedPath.Attrs.LocalPref > 0 {
		localPref, err := anypb.New(&gobgpapi.LocalPrefAttribute{LocalPref: appliedPath.Attrs.LocalPref})
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, localPref)
	}
	if appliedPath.Attrs.MED > 0 {
		med, err := anypb.New(&gobgpapi.MultiExitDiscAttribute{Med: appliedPath.Attrs.MED})
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, med)
	}
	communities, err := standardCommunities(appliedPath.Attrs.Communities)
	if err != nil {
		return nil, err
	}
	if len(communities) > 0 {
		attr, err := anypb.New(&gobgpapi.CommunitiesAttribute{Communities: communities})
		if err != nil {
			return nil, err
		}
		attrs = append(attrs, attr)
	}
	return &gobgpapi.Path{Family: familyForPrefix(parsed), Nlri: nlri, Pattrs: attrs}, nil
}

func standardCommunities(values []string) ([]uint32, error) {
	var out []uint32
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if strings.Contains(value, ":") {
			left, right, ok := strings.Cut(value, ":")
			if !ok {
				return nil, fmt.Errorf("invalid standard community %q", value)
			}
			hi, err := strconv.ParseUint(strings.TrimSpace(left), 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid standard community %q: %w", value, err)
			}
			lo, err := strconv.ParseUint(strings.TrimSpace(right), 10, 16)
			if err != nil {
				return nil, fmt.Errorf("invalid standard community %q: %w", value, err)
			}
			out = append(out, uint32(hi)<<16|uint32(lo))
			continue
		}
		community, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return nil, fmt.Errorf("invalid standard community %q: %w", value, err)
		}
		out = append(out, uint32(community))
	}
	return out, nil
}

func familyForPrefix(prefix netip.Prefix) *gobgpapi.Family {
	if prefix.Addr().Is6() {
		return ipv6Family()
	}
	return ipv4Family()
}

func ipv4Family() *gobgpapi.Family {
	return &gobgpapi.Family{Afi: gobgpapi.Family_AFI_IP, Safi: gobgpapi.Family_SAFI_UNICAST}
}

func ipv6Family() *gobgpapi.Family {
	return &gobgpapi.Family{Afi: gobgpapi.Family_AFI_IP6, Safi: gobgpapi.Family_SAFI_UNICAST}
}

func sortedPeers(peers map[string]bgpdaemon.AppliedPeer) []bgpdaemon.AppliedPeer {
	var keys []string
	for key := range peers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]bgpdaemon.AppliedPeer, 0, len(keys))
	for _, key := range keys {
		out = append(out, peers[key])
	}
	return out
}
