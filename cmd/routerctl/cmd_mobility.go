// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/netip"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/internal/mapsort"
	"github.com/imksoo/routerd/internal/stringutil"
	"github.com/imksoo/routerd/pkg/api"
	bgpstate "github.com/imksoo/routerd/pkg/bgp"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig/codec"
	"github.com/imksoo/routerd/pkg/mobilityconfig"
	"github.com/imksoo/routerd/pkg/samenrollment"
	routerstate "github.com/imksoo/routerd/pkg/state"
)

type mobilityPathRow struct {
	Router   string   `json:"router" yaml:"router"`
	Prefix   string   `json:"prefix" yaml:"prefix"`
	NextHops []string `json:"nextHops" yaml:"nextHops"`
}

type mobilityTrapRow struct {
	Source         string `json:"source" yaml:"source"`
	Action         string `json:"action" yaml:"action"`
	Provider       string `json:"provider,omitempty" yaml:"provider,omitempty"`
	ProviderRef    string `json:"providerRef,omitempty" yaml:"providerRef,omitempty"`
	NICRef         string `json:"nicRef,omitempty" yaml:"nicRef,omitempty"`
	Address        string `json:"address,omitempty" yaml:"address,omitempty"`
	IdempotencyKey string `json:"idempotencyKey" yaml:"idempotencyKey"`
}

type mobilityOwnerRow struct {
	Pool                  string `json:"pool" yaml:"pool"`
	Address               string `json:"address" yaml:"address"`
	State                 string `json:"state" yaml:"state"`
	Class                 string `json:"class" yaml:"class"`
	OwnerNode             string `json:"ownerNode,omitempty" yaml:"ownerNode,omitempty"`
	OwnerProviderRef      string `json:"ownerProviderRef,omitempty" yaml:"ownerProviderRef,omitempty"`
	OwnerNICRef           string `json:"ownerNICRef,omitempty" yaml:"ownerNICRef,omitempty"`
	OwnerResourceRef      string `json:"ownerResourceRef,omitempty" yaml:"ownerResourceRef,omitempty"`
	LocalEvidenceNode     string `json:"localEvidenceNode,omitempty" yaml:"localEvidenceNode,omitempty"`
	LocalEvidenceSource   string `json:"localEvidenceSource,omitempty" yaml:"localEvidenceSource,omitempty"`
	LocalEvidenceNICRef   string `json:"localEvidenceNICRef,omitempty" yaml:"localEvidenceNICRef,omitempty"`
	LocalEvidenceResource string `json:"localEvidenceResourceRef,omitempty" yaml:"localEvidenceResourceRef,omitempty"`
	CaptureHolderNode     string `json:"captureHolderNode,omitempty" yaml:"captureHolderNode,omitempty"`
	CaptureDisposition    string `json:"captureDisposition,omitempty" yaml:"captureDisposition,omitempty"`
	CaptureReason         string `json:"captureReason,omitempty" yaml:"captureReason,omitempty"`
	AdvertiseOwnerNode    string `json:"advertiseOwnerNode,omitempty" yaml:"advertiseOwnerNode,omitempty"`
	SuppressionReason     string `json:"suppressionReason,omitempty" yaml:"suppressionReason,omitempty"`
	ConflictReason        string `json:"conflictReason,omitempty" yaml:"conflictReason,omitempty"`
}

type mobilityExplainReport struct {
	Pool               string            `json:"pool" yaml:"pool"`
	Address            string            `json:"address" yaml:"address"`
	Phase              string            `json:"phase" yaml:"phase"`
	Severity           string            `json:"severity,omitempty" yaml:"severity,omitempty"`
	Diagnostic         bool              `json:"diagnostic,omitempty" yaml:"diagnostic,omitempty"`
	DiagnosticReason   string            `json:"diagnosticReason,omitempty" yaml:"diagnosticReason,omitempty"`
	Health             string            `json:"health,omitempty" yaml:"health,omitempty"`
	Class              string            `json:"class,omitempty" yaml:"class,omitempty"`
	OwnerNode          string            `json:"ownerNode,omitempty" yaml:"ownerNode,omitempty"`
	CaptureHolderNode  string            `json:"captureHolderNode,omitempty" yaml:"captureHolderNode,omitempty"`
	OwnerProviderRef   string            `json:"ownerProviderRef,omitempty" yaml:"ownerProviderRef,omitempty"`
	CaptureDisposition string            `json:"captureDisposition,omitempty" yaml:"captureDisposition,omitempty"`
	CaptureReason      string            `json:"captureReason,omitempty" yaml:"captureReason,omitempty"`
	BlockingCondition  string            `json:"blockingCondition,omitempty" yaml:"blockingCondition,omitempty"`
	Conditions         map[string]string `json:"conditions" yaml:"conditions"`
	ConditionReasons   map[string]string `json:"conditionReasons,omitempty" yaml:"conditionReasons,omitempty"`
}

type mobilityRepeatedStringFlag []string

func (f *mobilityRepeatedStringFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(*f, ",")
}

func (f *mobilityRepeatedStringFlag) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			*f = append(*f, part)
		}
	}
	return nil
}

func mobilityCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		mobilityUsage(stderr)
		return errors.New("mobility requires subcommand")
	}
	switch args[0] {
	case "paths":
		return mobilityPathsCommand(args[1:], stdout)
	case "traps":
		return mobilityTrapsCommand(args[1:], stdout)
	case "owners":
		return mobilityOwnersCommand(args[1:], stdout)
	case "explain":
		return mobilityExplainCommand(args[1:], stdout)
	case "enrollment-hmac":
		return mobilityEnrollmentHMACCommand(args[1:], stdout)
	case "enrollment-submit":
		return mobilityEnrollmentSubmitCommand(args[1:], stdout)
	case "enrollment-revoke":
		return mobilityEnrollmentRevokeCommand(args[1:], stdout)
	case "leaf-config":
		return mobilityLeafConfigCommand(args[1:], stdout)
	case "leases", "list", "ownership", "show":
		return fmt.Errorf("mobility %s was removed with BGP mobility; use `routerctl mobility owners`, `routerctl mobility paths`, `routerctl mobility traps`, or `routerctl mobility explain`", args[0])
	case "help", "-h", "--help":
		mobilityUsage(stdout)
		return nil
	default:
		return fmt.Errorf("unknown mobility subcommand %q", args[0])
	}
}

func mobilityEnrollmentHMACCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility enrollment-hmac", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Compute a SAMEnrollmentClaim joinHMAC from a router config and join secret.",
			"routerctl mobility enrollment-hmac --config leaf.yaml --claim leaf-b --secret-file /usr/local/etc/routerd/secrets/cloudedge-join-token\n"+
				"routerctl mobility enrollment-hmac --config leaf.yaml --claim leaf-a --secret-env ROUTERD_JOIN_TOKEN --show-payload")
	}
	configPath := fs.String("config", defaultConfigPath(), "router config containing the SAMEnrollmentClaim")
	claimName := fs.String("claim", "", "SAMEnrollmentClaim name")
	secretFile := fs.String("secret-file", "", "join secret file")
	secretEnv := fs.String("secret-env", "", "environment variable containing the join secret")
	secretLiteral := fs.String("secret", "", "literal join secret")
	secretBase64 := fs.Bool("secret-base64", false, "decode the selected secret as base64 before HMAC")
	showPayload := fs.Bool("show-payload", false, "print the canonical payload before the HMAC")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected mobility enrollment-hmac argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*claimName) == "" {
		return errors.New("mobility enrollment-hmac requires --claim")
	}
	secret, err := mobilityEnrollmentHMACSecret(*secretFile, *secretEnv, *secretLiteral, *secretBase64)
	if err != nil {
		return err
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	claim, err := mobilityEnrollmentClaim(router, *claimName)
	if err != nil {
		return err
	}
	if *showPayload {
		fmt.Fprintln(stdout, samenrollment.JoinCanonicalPayload(claim))
		fmt.Fprintln(stdout, "---")
	}
	fmt.Fprintln(stdout, samenrollment.JoinHMAC(secret, claim))
	return nil
}

func mobilityEnrollmentSubmitCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility enrollment-submit", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Submit a leaf SAMEnrollmentClaim to an RR routerd control API.",
			"routerctl mobility enrollment-submit --config leaf.yaml --claim pve-leaf-b --socket /run/routerd/routerd.sock")
	}
	configPath := fs.String("config", defaultConfigPath(), "leaf config containing the SAMEnrollmentClaim")
	claimName := fs.String("claim", "", "SAMEnrollmentClaim name")
	socketPath := fs.String("socket", defaultSocketPath(), "RR routerd Unix domain socket path")
	timeout := fs.Duration("timeout", 10*time.Second, "request timeout")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected mobility enrollment-submit argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*claimName) == "" {
		return errors.New("mobility enrollment-submit requires --claim")
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	claim, err := mobilityEnrollmentClaimResource(router, *claimName)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := controlapi.NewUnixClient(*socketPath).SubmitSAMEnrollmentClaim(ctx, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claim})
	if err != nil {
		return fmt.Errorf("submit SAMEnrollmentClaim to routerd failed: %w", err)
	}
	switch output {
	case "", "table", "text":
		fmt.Fprintf(stdout, "accepted\t%s\nsource\t%s\ngeneration\t%d\nexpiresAt\t%s\n", result.ClaimRef, result.DynamicSource, result.Generation, result.ExpiresAt.Format(time.RFC3339))
		return nil
	case "json":
		return writeJSON(stdout, result)
	case "yaml":
		return writeYAML(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func mobilityEnrollmentRevokeCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility enrollment-revoke", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Revoke an accepted SAMEnrollmentClaim on an RR routerd control API.",
			"routerctl mobility enrollment-revoke --claim pve-leaf-b --rr-url https://pve-rr:65432 --rr-token-file /usr/local/etc/routerd/secrets/control-api-token --rr-ca-file /usr/local/etc/routerd/secrets/rr-ca.pem\n"+
				"routerctl mobility enrollment-revoke --claim pve-leaf-a --rr-socket /run/routerd/routerd.sock --reason rotated")
	}
	claimName := fs.String("claim", "", "SAMEnrollmentClaim name")
	reason := fs.String("reason", "", "operator reason recorded in the revoke result")
	rrSocketPath := fs.String("rr-socket", "", "RR routerd Unix domain socket path")
	rrURL := fs.String("rr-url", "", "RR routerd control API base URL")
	rrTokenFile := fs.String("rr-token-file", "", "file containing bearer token for --rr-url")
	rrTokenEnv := fs.String("rr-token-env", "", "environment variable containing bearer token for --rr-url")
	rrTokenBase64 := fs.Bool("rr-token-base64", false, "decode the selected RR bearer token as base64")
	rrCAFile := fs.String("rr-ca-file", "", "CA bundle for verifying --rr-url TLS")
	rrClientCertFile := fs.String("rr-client-cert-file", "", "client certificate file for RR mTLS")
	rrClientKeyFile := fs.String("rr-client-key-file", "", "client private key file for RR mTLS")
	rrServerName := fs.String("rr-server-name", "", "TLS server name override for --rr-url")
	rrInsecureSkipVerify := fs.Bool("rr-insecure-skip-verify", false, "skip RR TLS certificate verification")
	timeout := fs.Duration("timeout", 10*time.Second, "request timeout")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected mobility enrollment-revoke argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*claimName) == "" {
		return errors.New("mobility enrollment-revoke requires --claim")
	}
	if strings.TrimSpace(*rrSocketPath) != "" && strings.TrimSpace(*rrURL) != "" {
		return errors.New("mobility enrollment-revoke accepts only one of --rr-socket or --rr-url")
	}
	if strings.TrimSpace(*rrSocketPath) != "" && (strings.TrimSpace(*rrTokenFile) != "" || strings.TrimSpace(*rrTokenEnv) != "") {
		return errors.New("mobility enrollment-revoke RR bearer token flags require --rr-url")
	}
	if strings.TrimSpace(*rrSocketPath) != "" && (strings.TrimSpace(*rrCAFile) != "" || strings.TrimSpace(*rrClientCertFile) != "" || strings.TrimSpace(*rrClientKeyFile) != "" || strings.TrimSpace(*rrServerName) != "" || *rrInsecureSkipVerify) {
		return errors.New("mobility enrollment-revoke RR TLS flags require --rr-url")
	}
	token, err := mobilityEnrollmentBearerToken(*rrTokenFile, *rrTokenEnv, *rrTokenBase64)
	if err != nil {
		return err
	}
	client, err := mobilityEnrollmentClient(*rrSocketPath, *rrURL, token, controlapi.TLSOptions{
		CAFile:             *rrCAFile,
		CertFile:           *rrClientCertFile,
		KeyFile:            *rrClientKeyFile,
		ServerName:         *rrServerName,
		InsecureSkipVerify: *rrInsecureSkipVerify,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := client.RevokeSAMEnrollmentClaim(ctx, controlapi.SAMEnrollmentClaimRevokeRequest{Name: *claimName, Reason: *reason})
	if err != nil {
		return fmt.Errorf("revoke SAMEnrollmentClaim on routerd failed: %w", err)
	}
	switch output {
	case "", "table", "text":
		fmt.Fprintf(stdout, "revoked\t%s\nsource\t%s\ngeneration\t%d\nexpiresAt\t%s\n", result.ClaimRef, result.DynamicSource, result.Generation, result.ExpiresAt.Format(time.RFC3339))
		return nil
	case "json":
		return writeJSON(stdout, result)
	case "yaml":
		return writeYAML(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func mobilityEnrollmentClient(socketPath, baseURL, bearerToken string, tlsOptions controlapi.TLSOptions) (*controlapi.Client, error) {
	if strings.TrimSpace(baseURL) != "" {
		client, err := controlapi.NewHTTPClientWithTLS(baseURL, tlsOptions)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(bearerToken) != "" {
			client = client.WithBearerToken(bearerToken)
		}
		return client, nil
	}
	if strings.TrimSpace(socketPath) == "" {
		socketPath = defaultSocketPath()
	}
	return controlapi.NewUnixClient(socketPath), nil
}

func mobilityLeafConfigCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility leaf-config", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Generate a minimal SAM dynamic leaf config with SAMEnrollmentClient bootstrap.",
			"routerctl mobility leaf-config --leaf-id pve-leaf-b --underlay-ifname vmbr0 --underlay-address 10.30.0.22/24 --local-endpoint 10.30.0.22 --endpoint-prefix 10.30.0.0/24 --inner-prefix 10.255.10.0/24 --tunnel-address 10.255.10.22/32 --mobility-prefix 10.77.70.0/24 --owned-address 10.77.70.22/32 --rr-set pve-rrs --policy pve-fou-leaves --join-audience pve-private-underlay --bootstrap-endpoint https://10.30.0.10:65432 --control-api-token-file /usr/local/etc/routerd/secrets/control-api-token --control-api-ca-file /usr/local/etc/routerd/secrets/rr-ca.pem --control-api-client-cert-file /usr/local/etc/routerd/secrets/leaf.crt --control-api-client-key-file /usr/local/etc/routerd/secrets/leaf.key --secret-file /usr/local/etc/routerd/secrets/pve-join-token")
	}
	leafID := fs.String("leaf-id", "", "leaf node ID, Router.metadata.name, and claim/client/profile name")
	underlayName := fs.String("underlay-name", "private-wan", "routerd Interface resource name for the underlay")
	underlayIfName := fs.String("underlay-ifname", "", "host interface name for the underlay")
	underlayAddress := fs.String("underlay-address", "", "underlay IPv4 address with prefix length")
	localEndpoint := fs.String("local-endpoint", "", "leaf underlay endpoint address advertised in the claim")
	endpointPrefix := fs.String("endpoint-prefix", "", "allowed underlay endpoint prefix for the local policy")
	innerPrefix := fs.String("inner-prefix", "", "SAM tunnel inner prefix")
	tunnelAddress := fs.String("tunnel-address", "", "leaf SAM tunnel /32")
	mobilityPrefix := fs.String("mobility-prefix", "", "authorized SAM IPv4 prefix accepted from the RR")
	ownedAddress := fs.String("owned-address", "", "leaf-owned SAM IPv4 /32")
	rrSet := fs.String("rr-set", "", "SAMRRSet name fetched from the RR")
	directPeerGroup := fs.String("direct-peer-group", "", "policy-scoped SAMPeerGroup name for opportunistic direct leaf peers; RR remains the fallback")
	policy := fs.String("policy", "", "SAMEnrollmentPolicy name")
	joinAudience := fs.String("join-audience", "", "join audience string")
	joinNonce := fs.String("join-nonce", "", "claim join nonce; defaults to <leaf-id>-0001")
	joinTimestamp := fs.String("join-timestamp", "", "claim join timestamp; defaults to current UTC time")
	joinHMAC := fs.String("join-hmac", "", "precomputed join HMAC; defaults to computed HMAC when a secret source is supplied, otherwise EXAMPLE_HMAC_SHA256_HEX")
	secretFile := fs.String("secret-file", "", "join secret file used to compute claim joinHMAC")
	secretEnv := fs.String("secret-env", "", "join secret env var used to compute claim joinHMAC")
	secretLiteral := fs.String("secret", "", "literal join secret used to compute claim joinHMAC")
	secretBase64 := fs.Bool("secret-base64", false, "decode selected join secret as base64 before HMAC")
	bgpASN := fs.Uint("bgp-asn", 64577, "leaf and RR BGP ASN")
	bgpRouterID := fs.String("bgp-router-id", "", "leaf BGP router ID; defaults to tunnel address host")
	rrLocalPreference := fs.Uint("rr-local-preference", uint(mobilityconfig.DefaultBGPImportLocalPreference), "BGP local preference for RR-imported paths when --direct-peer-group is used")
	directLocalPreference := fs.Uint("direct-local-preference", uint(mobilityconfig.DefaultSAMTransportDirectLocalPreference), "BGP local preference for direct leaf paths; must exceed --rr-local-preference")
	mode := fs.String("mode", "fou", "SAM transport mode: fou, ipip, gre, or gue")
	encryption := fs.String("encryption", "none", "SAM transport encryption")
	encapSport := fs.Int("encap-sport", 5555, "FOU/GUE source port")
	encapDport := fs.Int("encap-dport", 5555, "FOU/GUE destination port")
	stateTTLRefreshBefore := fs.String("state-ttl-refresh-before", "10m", "SAMEnrollmentClient refresh-before duration")
	retryMin := fs.String("retry-min", "10s", "SAMEnrollmentClient minimum retry backoff")
	retryMax := fs.String("retry-max", "15m", "SAMEnrollmentClient maximum retry backoff")
	controlAPITokenFile := fs.String("control-api-token-file", "", "bearer token file for SAMEnrollmentClient HTTP bootstrap")
	controlAPITokenEnv := fs.String("control-api-token-env", "", "bearer token env var for SAMEnrollmentClient HTTP bootstrap")
	controlAPICAFile := fs.String("control-api-ca-file", "", "CA bundle for SAMEnrollmentClient HTTPS bootstrap")
	controlAPIClientCertFile := fs.String("control-api-client-cert-file", "", "client certificate for SAMEnrollmentClient mTLS")
	controlAPIClientKeyFile := fs.String("control-api-client-key-file", "", "client key for SAMEnrollmentClient mTLS")
	controlAPIServerName := fs.String("control-api-server-name", "", "TLS server name override for SAMEnrollmentClient")
	controlAPIInsecureSkipVerify := fs.Bool("control-api-insecure-skip-verify", false, "skip SAMEnrollmentClient TLS certificate verification")
	var bootstrapEndpoints mobilityRepeatedStringFlag
	fs.Var(&bootstrapEndpoints, "bootstrap-endpoint", "RR ControlAPI bootstrap endpoint; repeat or comma-separate")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected mobility leaf-config argument %q", fs.Arg(0))
	}
	if *bgpASN > uint(^uint32(0)) {
		return errors.New("mobility leaf-config requires --bgp-asn within uint32 range")
	}
	if *rrLocalPreference > uint(^uint32(0)) || *directLocalPreference > uint(^uint32(0)) {
		return errors.New("mobility leaf-config local preference values must be within uint32 range")
	}
	router, err := mobilityGeneratedLeafConfig(mobilityLeafConfigOptions{
		LeafID:                       *leafID,
		UnderlayName:                 *underlayName,
		UnderlayIfName:               *underlayIfName,
		UnderlayAddress:              *underlayAddress,
		LocalEndpoint:                *localEndpoint,
		EndpointPrefix:               *endpointPrefix,
		InnerPrefix:                  *innerPrefix,
		TunnelAddress:                *tunnelAddress,
		MobilityPrefix:               *mobilityPrefix,
		OwnedAddress:                 *ownedAddress,
		RRSet:                        *rrSet,
		DirectPeerGroup:              *directPeerGroup,
		Policy:                       *policy,
		JoinAudience:                 *joinAudience,
		JoinNonce:                    *joinNonce,
		JoinTimestamp:                *joinTimestamp,
		JoinHMAC:                     *joinHMAC,
		SecretFile:                   *secretFile,
		SecretEnv:                    *secretEnv,
		SecretLiteral:                *secretLiteral,
		SecretBase64:                 *secretBase64,
		BGPASN:                       uint32(*bgpASN),
		BGPRouterID:                  *bgpRouterID,
		RRLocalPreference:            uint32(*rrLocalPreference),
		DirectLocalPreference:        uint32(*directLocalPreference),
		Mode:                         *mode,
		Encryption:                   *encryption,
		EncapSport:                   *encapSport,
		EncapDport:                   *encapDport,
		BootstrapEndpoints:           []string(bootstrapEndpoints),
		StateTTLRefreshBefore:        *stateTTLRefreshBefore,
		RetryMin:                     *retryMin,
		RetryMax:                     *retryMax,
		ControlAPITokenFile:          *controlAPITokenFile,
		ControlAPITokenEnv:           *controlAPITokenEnv,
		ControlAPICAFile:             *controlAPICAFile,
		ControlAPIClientCertFile:     *controlAPIClientCertFile,
		ControlAPIClientKeyFile:      *controlAPIClientKeyFile,
		ControlAPIServerName:         *controlAPIServerName,
		ControlAPIInsecureSkipVerify: *controlAPIInsecureSkipVerify,
	})
	if err != nil {
		return err
	}
	if err := config.Validate(router); err != nil {
		return fmt.Errorf("generated leaf config is invalid: %w", err)
	}
	return writeYAML(stdout, router)
}

type mobilityLeafConfigOptions struct {
	LeafID                       string
	UnderlayName                 string
	UnderlayIfName               string
	UnderlayAddress              string
	LocalEndpoint                string
	EndpointPrefix               string
	InnerPrefix                  string
	TunnelAddress                string
	MobilityPrefix               string
	OwnedAddress                 string
	RRSet                        string
	DirectPeerGroup              string
	Policy                       string
	JoinAudience                 string
	JoinNonce                    string
	JoinTimestamp                string
	JoinHMAC                     string
	SecretFile                   string
	SecretEnv                    string
	SecretLiteral                string
	SecretBase64                 bool
	BGPASN                       uint32
	BGPRouterID                  string
	RRLocalPreference            uint32
	DirectLocalPreference        uint32
	Mode                         string
	Encryption                   string
	EncapSport                   int
	EncapDport                   int
	BootstrapEndpoints           []string
	StateTTLRefreshBefore        string
	RetryMin                     string
	RetryMax                     string
	ControlAPITokenFile          string
	ControlAPITokenEnv           string
	ControlAPICAFile             string
	ControlAPIClientCertFile     string
	ControlAPIClientKeyFile      string
	ControlAPIServerName         string
	ControlAPIInsecureSkipVerify bool
}

func mobilityGeneratedLeafConfig(opts mobilityLeafConfigOptions) (*api.Router, error) {
	opts.LeafID = strings.TrimSpace(opts.LeafID)
	required := map[string]string{
		"--leaf-id":          opts.LeafID,
		"--underlay-ifname":  opts.UnderlayIfName,
		"--underlay-address": opts.UnderlayAddress,
		"--local-endpoint":   opts.LocalEndpoint,
		"--endpoint-prefix":  opts.EndpointPrefix,
		"--inner-prefix":     opts.InnerPrefix,
		"--tunnel-address":   opts.TunnelAddress,
		"--mobility-prefix":  opts.MobilityPrefix,
		"--owned-address":    opts.OwnedAddress,
		"--rr-set":           opts.RRSet,
		"--policy":           opts.Policy,
		"--join-audience":    opts.JoinAudience,
	}
	for flagName, value := range required {
		if strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("mobility leaf-config requires %s", flagName)
		}
	}
	if len(opts.BootstrapEndpoints) == 0 {
		return nil, errors.New("mobility leaf-config requires at least one --bootstrap-endpoint")
	}
	if opts.BGPASN == 0 {
		return nil, errors.New("mobility leaf-config requires --bgp-asn greater than zero")
	}
	if strings.TrimSpace(opts.DirectPeerGroup) != "" && mobilityconfig.EffectiveSAMTransportDirectLocalPreference(opts.DirectLocalPreference) <= mobilityconfig.EffectiveBGPImportLocalPreference(opts.RRLocalPreference) {
		return nil, errors.New("mobility leaf-config requires --direct-local-preference greater than --rr-local-preference when --direct-peer-group is set")
	}
	mode := strings.TrimSpace(opts.Mode)
	if mode == "" {
		mode = "fou"
	}
	encryption := strings.TrimSpace(opts.Encryption)
	if encryption == "" {
		encryption = "none"
	}
	if mode == "fou" || mode == "gue" {
		if opts.EncapSport <= 0 || opts.EncapDport <= 0 {
			return nil, errors.New("mobility leaf-config requires positive --encap-sport and --encap-dport for FOU/GUE")
		}
	} else {
		opts.EncapSport = 0
		opts.EncapDport = 0
	}
	if _, err := netip.ParsePrefix(strings.TrimSpace(opts.UnderlayAddress)); err != nil {
		return nil, fmt.Errorf("--underlay-address must be an IP prefix: %w", err)
	}
	if _, err := netip.ParsePrefix(strings.TrimSpace(opts.EndpointPrefix)); err != nil {
		return nil, fmt.Errorf("--endpoint-prefix must be an IP prefix: %w", err)
	}
	if _, err := netip.ParsePrefix(strings.TrimSpace(opts.InnerPrefix)); err != nil {
		return nil, fmt.Errorf("--inner-prefix must be an IP prefix: %w", err)
	}
	tunnelPrefix, err := netip.ParsePrefix(strings.TrimSpace(opts.TunnelAddress))
	if err != nil || !tunnelPrefix.Addr().Is4() || tunnelPrefix.Bits() != 32 {
		return nil, errors.New("--tunnel-address must be an IPv4 /32")
	}
	if _, err := netip.ParsePrefix(strings.TrimSpace(opts.MobilityPrefix)); err != nil {
		return nil, fmt.Errorf("--mobility-prefix must be an IP prefix: %w", err)
	}
	ownedPrefix, err := netip.ParsePrefix(strings.TrimSpace(opts.OwnedAddress))
	if err != nil || !ownedPrefix.Addr().Is4() || ownedPrefix.Bits() != 32 {
		return nil, errors.New("--owned-address must be an IPv4 /32")
	}
	if strings.TrimSpace(opts.BGPRouterID) == "" {
		opts.BGPRouterID = tunnelPrefix.Addr().String()
	}
	if _, err := netip.ParseAddr(strings.TrimSpace(opts.BGPRouterID)); err != nil {
		return nil, fmt.Errorf("--bgp-router-id must be an IP address: %w", err)
	}
	if strings.TrimSpace(opts.JoinNonce) == "" {
		opts.JoinNonce = opts.LeafID + "-0001"
	}
	if strings.TrimSpace(opts.JoinTimestamp) == "" {
		opts.JoinTimestamp = time.Now().UTC().Format(time.RFC3339)
	}
	if _, err := time.Parse(time.RFC3339, strings.TrimSpace(opts.JoinTimestamp)); err != nil {
		return nil, fmt.Errorf("--join-timestamp must be RFC3339: %w", err)
	}
	claim := api.SAMEnrollmentClaimSpec{
		PolicyRef:     "SAMEnrollmentPolicy/" + strings.TrimSpace(opts.Policy),
		RRSetRef:      "SAMRRSet/" + strings.TrimSpace(opts.RRSet),
		LeafID:        opts.LeafID,
		JoinAudience:  strings.TrimSpace(opts.JoinAudience),
		JoinNonce:     strings.TrimSpace(opts.JoinNonce),
		JoinTimestamp: strings.TrimSpace(opts.JoinTimestamp),
		TunnelAddress: strings.TrimSpace(opts.TunnelAddress),
		Endpoint:      strings.TrimSpace(opts.LocalEndpoint),
		Mobility:      api.SAMEnrollmentClaimMobilitySpec{OwnedAddresses: []string{strings.TrimSpace(opts.OwnedAddress)}},
		BGP:           api.SAMEnrollmentClaimBGPSpec{ASN: opts.BGPASN, RouterID: strings.TrimSpace(opts.BGPRouterID)},
	}
	if strings.TrimSpace(opts.DirectPeerGroup) != "" {
		claim.DirectMesh = true
	}
	switch {
	case strings.TrimSpace(opts.JoinHMAC) != "":
		claim.JoinHMAC = strings.TrimSpace(opts.JoinHMAC)
	case strings.TrimSpace(opts.SecretFile) != "" || strings.TrimSpace(opts.SecretEnv) != "" || opts.SecretLiteral != "":
		secret, err := mobilityEnrollmentHMACSecret(opts.SecretFile, opts.SecretEnv, opts.SecretLiteral, opts.SecretBase64)
		if err != nil {
			return nil, err
		}
		claim.JoinHMAC = samenrollment.JoinHMAC(secret, claim)
	default:
		claim.JoinHMAC = "EXAMPLE_HMAC_SHA256_HEX"
	}
	peersFrom := []api.SAMTransportPeersSourceSpec{{Resource: "SAMRRSet/" + strings.TrimSpace(opts.RRSet)}}
	if directPeerGroup := strings.TrimSpace(opts.DirectPeerGroup); directPeerGroup != "" {
		peersFrom = append(peersFrom, api.SAMTransportPeersSourceSpec{
			Resource: "SAMPeerGroup/" + directPeerGroup,
			Direct:   true,
		})
	}
	rrLocalPreference := uint32(0)
	directLocalPreference := uint32(0)
	if strings.TrimSpace(opts.DirectPeerGroup) != "" {
		rrLocalPreference = opts.RRLocalPreference
		directLocalPreference = opts.DirectLocalPreference
	}
	// Direct peers require a signed, deterministic identity community in
	// addition to their per-leaf /32 allowlist. Tag the generated leaf's local
	// advertisement at its source so another direct leaf can prove both who
	// sent the route and which signed address that sender owns.
	communities := api.BGPCommunitiesSpec{}
	if strings.TrimSpace(opts.DirectPeerGroup) != "" {
		communities = api.BGPCommunitiesSpec{
			Send: "standard",
			Set:  api.BGPCommunitySetSpec{Out: []string{bgpstate.MobilityNodeIdentityCommunity(opts.LeafID)}},
		}
	}
	controlTokenFrom := api.SecretValueSourceSpec{File: strings.TrimSpace(opts.ControlAPITokenFile), Env: strings.TrimSpace(opts.ControlAPITokenEnv)}
	router := &api.Router{
		TypeMeta: api.TypeMeta{APIVersion: api.RouterAPIVersion, Kind: "Router"},
		Metadata: api.ObjectMeta{Name: opts.LeafID},
		Spec: api.RouterSpec{Resources: []api.Resource{
			leafResource(api.NetAPIVersion, "Interface", strings.TrimSpace(opts.UnderlayName), api.InterfaceSpec{IfName: strings.TrimSpace(opts.UnderlayIfName), Managed: false, MTU: 1500}),
			leafResource(api.NetAPIVersion, "IPv4StaticAddress", strings.TrimSpace(opts.UnderlayName)+"-ipv4", api.IPv4StaticAddressSpec{Interface: strings.TrimSpace(opts.UnderlayName), Address: strings.TrimSpace(opts.UnderlayAddress)}),
			leafResource(api.NetAPIVersion, "Interface", "lo-mobility", api.InterfaceSpec{IfName: "lo", Managed: false}),
			leafResource(api.NetAPIVersion, "IPv4StaticAddress", "owned-service-ip", api.IPv4StaticAddressSpec{Interface: "lo-mobility", Address: strings.TrimSpace(opts.OwnedAddress), AllowOverlap: true, AllowOverlapReason: opts.LeafID + " owned SAM /32 authorized by the RR policy"}),
			leafResource(api.NetAPIVersion, "BGPRouter", "mobility-bgp", api.BGPRouterSpec{
				ASN:      opts.BGPASN,
				RouterID: strings.TrimSpace(opts.BGPRouterID),
				ExportPolicy: api.BGPExportPolicySpec{
					AllowedPrefixes: []string{strings.TrimSpace(opts.OwnedAddress)},
				},
				Communities: communities,
				Redistribute: api.BGPRedistributeSpec{
					Connected: api.BGPRedistributeRouteSpec{AllowedPrefixes: []string{strings.TrimSpace(opts.OwnedAddress)}},
				},
				Timers:             api.BGPTimersSpec{Profile: "fast"},
				ConvergenceProfile: "fast",
			}),
			leafResource(api.MobilityAPIVersion, "SAMTransportProfile", opts.LeafID, api.SAMTransportProfileSpec{
				SelfNodeRef:       opts.LeafID,
				Mode:              mode,
				Encryption:        encryption,
				AddressingMode:    "pair-stable",
				InnerPrefix:       strings.TrimSpace(opts.InnerPrefix),
				UnderlayInterface: strings.TrimSpace(opts.UnderlayName),
				LocalEndpoint:     strings.TrimSpace(opts.LocalEndpoint),
				EncapSport:        opts.EncapSport,
				EncapDport:        opts.EncapDport,
				PeersFrom:         peersFrom,
				BGP: api.SAMTransportBGPProfileSpec{
					RouterRef:             "BGPRouter/mobility-bgp",
					PeerASN:               opts.BGPASN,
					TimersPreset:          "fast",
					DirectLocalPreference: directLocalPreference,
					ImportPolicy: api.BGPImportPolicySpec{
						AllowedPrefixes:        []string{strings.TrimSpace(opts.MobilityPrefix)},
						AllowedPrefixLengthMin: 32,
						AllowedPrefixLengthMax: 32,
						NextHopRewrite:         "unchanged",
						LocalPreference:        rrLocalPreference,
					},
					ExportPolicy: api.BGPExportPolicySpec{AllowedPrefixes: []string{strings.TrimSpace(opts.OwnedAddress)}},
				},
			}),
			leafResource(api.MobilityAPIVersion, "SAMEnrollmentClaim", opts.LeafID, claim),
			leafResource(api.MobilityAPIVersion, "SAMEnrollmentClient", opts.LeafID, api.SAMEnrollmentClientSpec{
				ClaimRef:              "SAMEnrollmentClaim/" + opts.LeafID,
				BootstrapEndpoints:    trimmedStrings(opts.BootstrapEndpoints),
				ControlAPITokenFrom:   controlTokenFrom,
				ControlAPITLS:         api.ControlAPIClientTLSSpec{CAFile: strings.TrimSpace(opts.ControlAPICAFile), CertFile: strings.TrimSpace(opts.ControlAPIClientCertFile), KeyFile: strings.TrimSpace(opts.ControlAPIClientKeyFile), ServerName: strings.TrimSpace(opts.ControlAPIServerName), InsecureSkipVerify: opts.ControlAPIInsecureSkipVerify},
				StateTTLRefreshBefore: strings.TrimSpace(opts.StateTTLRefreshBefore),
				RetryBackoff:          api.SAMEnrollmentRetryBackoffSpec{Min: strings.TrimSpace(opts.RetryMin), Max: strings.TrimSpace(opts.RetryMax)},
			}),
		}},
	}
	return router, nil
}

func leafResource(apiVersion, kind, name string, spec any) api.Resource {
	return api.Resource{TypeMeta: api.TypeMeta{APIVersion: apiVersion, Kind: kind}, Metadata: api.ObjectMeta{Name: name}, Spec: spec}
}

func trimmedStrings(values []string) []string {
	var out []string
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func mobilityEnrollmentBearerToken(file, env string, base64Encoded bool) (string, error) {
	hasFile := strings.TrimSpace(file) != ""
	hasEnv := strings.TrimSpace(env) != ""
	if !hasFile && !hasEnv {
		return "", nil
	}
	if hasFile == hasEnv {
		return "", errors.New("use only one of --rr-token-file or --rr-token-env")
	}
	var value string
	switch {
	case hasFile:
		path := strings.TrimSpace(file)
		data, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read RR bearer token file %q: %w", path, err)
		}
		value = string(data)
	case hasEnv:
		name := strings.TrimSpace(env)
		found, ok := os.LookupEnv(name)
		if !ok {
			return "", fmt.Errorf("read RR bearer token env %q: not set", name)
		}
		value = found
	}
	value = strings.TrimSpace(value)
	if base64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return "", fmt.Errorf("decode RR bearer token: %w", err)
		}
		value = strings.TrimSpace(string(decoded))
	}
	if value == "" {
		return "", errors.New("RR bearer token must not be empty")
	}
	return value, nil
}

func mobilityEnrollmentHMACSecret(file, env, literal string, decodeBase64 bool) ([]byte, error) {
	selected := 0
	var value string
	if strings.TrimSpace(file) != "" {
		selected++
		data, err := os.ReadFile(strings.TrimSpace(file))
		if err != nil {
			return nil, err
		}
		value = string(data)
	}
	if strings.TrimSpace(env) != "" {
		selected++
		envValue, ok := os.LookupEnv(strings.TrimSpace(env))
		if !ok {
			return nil, fmt.Errorf("secret environment variable %q is not set", strings.TrimSpace(env))
		}
		value = envValue
	}
	if literal != "" {
		selected++
		value = literal
	}
	if selected != 1 {
		return nil, errors.New("mobility enrollment-hmac requires exactly one of --secret-file, --secret-env, or --secret")
	}
	value = strings.TrimSpace(value)
	if decodeBase64 {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, err
		}
		return decoded, nil
	}
	return []byte(value), nil
}

func mobilityEnrollmentClaim(router *api.Router, name string) (api.SAMEnrollmentClaimSpec, error) {
	resource, err := mobilityEnrollmentClaimResource(router, name)
	if err != nil {
		return api.SAMEnrollmentClaimSpec{}, err
	}
	return resource.SAMEnrollmentClaimSpec()
}

func mobilityEnrollmentClaimResource(router *api.Router, name string) (api.Resource, error) {
	name = strings.TrimSpace(name)
	for _, resource := range router.Spec.Resources {
		if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMEnrollmentClaim" || resource.Metadata.Name != name {
			continue
		}
		if _, err := resource.SAMEnrollmentClaimSpec(); err != nil {
			return api.Resource{}, err
		}
		return resource, nil
	}
	return api.Resource{}, fmt.Errorf("SAMEnrollmentClaim/%s not found", name)
}

func mobilityOwnersCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility owners", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show SAM control-plane owner table state from MobilityPool status.",
			"routerctl mobility owners\n"+
				"routerctl mobility owners --pool cloudedge --address 10.77.60.10/32 -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	poolFilter := fs.String("pool", "", "filter by MobilityPool name")
	addressFilter := fs.String("address", "", "filter by mobility address")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected mobility owners argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		return err
	}
	rows := mobilityOwnerRows(statuses, *poolFilter, *addressFilter)
	return writeMobilityOwners(stdout, rows, output)
}

func mobilityExplainCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility explain", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Explain one SAM address from the owner table and pool-level operational status.",
			"routerctl mobility explain --pool cloudedge --address 10.77.60.10/32\n"+
				"routerctl mobility explain --pool cloudedge --address 10.77.60.10/32 -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	pool := fs.String("pool", "", "MobilityPool name")
	address := fs.String("address", "", "mobility address")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected mobility explain argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*pool) == "" {
		return errors.New("mobility explain requires --pool")
	}
	if strings.TrimSpace(*address) == "" {
		return errors.New("mobility explain requires --address")
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		return err
	}
	report, err := mobilityExplainReportFor(statuses, *pool, *address)
	if err != nil {
		return err
	}
	return writeMobilityExplain(stdout, report, output)
}

func mobilityPathsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility paths", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show BGP-installed mobility /32 next-hop state.",
			"routerctl mobility paths\n"+
				"routerctl mobility paths --prefix 10.77.60.10/32 -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	prefixFilter := fs.String("prefix", "", "filter by prefix")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected mobility paths argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	statuses, err := store.ListObjectStatuses()
	if err != nil {
		return err
	}
	rows := mobilityPathRows(statuses, *prefixFilter)
	return writeMobilityPaths(stdout, rows, output)
}

func mobilityTrapsCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility traps", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Show provider trap ActionPlans generated by MobilityPool.",
			"routerctl mobility traps\n"+
				"routerctl mobility traps --address 10.77.60.10/32 -o json")
	}
	statePath := fs.String("state-file", defaultStatePath(), "routerd state database file")
	addressFilter := fs.String("address", "", "filter by captured address")
	output := "table"
	fs.StringVar(&output, "o", "table", "output format: table, json, yaml")
	fs.StringVar(&output, "output", "table", "output format: table, json, yaml")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 0 {
		return fmt.Errorf("unexpected mobility traps argument %q", fs.Arg(0))
	}
	store, err := openLedgerStateReadOnly(*statePath)
	if err != nil {
		return err
	}
	defer store.Close()
	parts, err := store.ListDynamicConfigParts()
	if err != nil {
		return err
	}
	rows := mobilityTrapRows(parts, *addressFilter)
	return writeMobilityTraps(stdout, rows, output)
}

func mobilityUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: routerctl mobility <command> [options]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "commands:")
	fmt.Fprintln(w, "  owners [--pool <name>] [--address <ipv4/32>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  explain --pool <name> --address <ipv4/32> [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  paths [--prefix <prefix>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  traps [--address <ipv4/32>] [--state-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  enrollment-hmac --config <path> --claim <name> (--secret-file <path>|--secret-env <name>|--secret <value>) [--secret-base64] [--show-payload]")
	fmt.Fprintln(w, "  enrollment-submit --config <path> --claim <name> [--socket <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  enrollment-revoke --claim <name> [--rr-socket <path>|--rr-url <url>] [--rr-token-file <path>|--rr-token-env <name>] [--rr-ca-file <path>] [--rr-client-cert-file <path> --rr-client-key-file <path>] [-o table|json|yaml]")
	fmt.Fprintln(w, "  leaf-config --leaf-id <name> --underlay-ifname <ifname> --underlay-address <cidr> --local-endpoint <ip> --endpoint-prefix <cidr> --inner-prefix <cidr> --tunnel-address <ipv4/32> --mobility-prefix <cidr> --owned-address <ipv4/32> --rr-set <name> [--direct-peer-group <name> --rr-local-preference 100 --direct-local-preference 200] --policy <name> --join-audience <name> --bootstrap-endpoint <url>")
}

func mobilityOwnerRows(statuses []routerstate.ObjectStatus, poolFilter, addressFilter string) []mobilityOwnerRow {
	poolFilter = strings.TrimSpace(poolFilter)
	addressFilter = strings.TrimSpace(addressFilter)
	var rows []mobilityOwnerRow
	for _, status := range statuses {
		if status.APIVersion != api.MobilityAPIVersion || status.Kind != "MobilityPool" {
			continue
		}
		if poolFilter != "" && status.Name != poolFilter {
			continue
		}
		table := statusMaps(status.Status["ownershipResolverControlPlaneOwnerTable"])
		for _, item := range table {
			address := stringStatus(item, "address")
			if addressFilter != "" && address != addressFilter {
				continue
			}
			rows = append(rows, mobilityOwnerRow{
				Pool:                  status.Name,
				Address:               address,
				State:                 stringutil.FirstNonEmpty(stringStatus(item, "state"), "Unknown"),
				Class:                 stringStatus(item, "class"),
				OwnerNode:             stringutil.FirstNonEmpty(stringStatus(item, "ownerNode"), stringStatus(item, "homeOwnerNode")),
				OwnerProviderRef:      stringStatus(item, "ownerProviderRef"),
				OwnerNICRef:           stringStatus(item, "ownerNICRef"),
				OwnerResourceRef:      stringStatus(item, "ownerResourceRef"),
				LocalEvidenceNode:     stringutil.FirstNonEmpty(stringStatus(item, "localEvidenceNode"), stringStatus(item, "localNode")),
				LocalEvidenceSource:   stringutil.FirstNonEmpty(stringStatus(item, "localEvidenceSource"), stringStatus(item, "localSource")),
				LocalEvidenceNICRef:   stringutil.FirstNonEmpty(stringStatus(item, "localEvidenceNICRef"), stringStatus(item, "localNICRef")),
				LocalEvidenceResource: stringutil.FirstNonEmpty(stringStatus(item, "localEvidenceResourceRef"), stringStatus(item, "localResourceRef")),
				CaptureHolderNode:     stringStatus(item, "captureHolderNode"),
				CaptureDisposition:    stringStatus(item, "captureDisposition"),
				CaptureReason:         stringStatus(item, "captureReason"),
				AdvertiseOwnerNode:    stringStatus(item, "advertiseOwnerNode"),
				SuppressionReason:     stringStatus(item, "suppressionReason"),
				ConflictReason:        stringStatus(item, "conflictReason"),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Pool == rows[j].Pool {
			return rows[i].Address < rows[j].Address
		}
		return rows[i].Pool < rows[j].Pool
	})
	return rows
}

func mobilityExplainReportFor(statuses []routerstate.ObjectStatus, pool, address string) (mobilityExplainReport, error) {
	pool = strings.TrimSpace(pool)
	address = mobilityCanonicalAddress(address)
	for _, status := range statuses {
		if status.APIVersion != api.MobilityAPIVersion || status.Kind != "MobilityPool" || status.Name != pool {
			continue
		}
		item, ok := mobilityOwnerTableRow(status.Status["ownershipResolverControlPlaneOwnerTable"], address)
		if !ok {
			return mobilityExplainReport{}, fmt.Errorf("MobilityPool/%s has no ownership row for %s", pool, address)
		}
		conditions, reasons := mobilityExplainConditions(status.Status, item, address)
		blocking := mobilityExplainBlockingCondition(conditions)
		report := mobilityExplainReport{
			Pool:               pool,
			Address:            stringutil.FirstNonEmpty(stringStatus(item, "address"), address),
			Phase:              mobilityExplainPhase(status.Status, blocking),
			Health:             stringStatus(status.Status, "health"),
			Class:              stringStatus(item, "class"),
			OwnerNode:          stringStatus(item, "ownerNode"),
			CaptureHolderNode:  stringStatus(item, "captureHolderNode"),
			OwnerProviderRef:   stringStatus(item, "ownerProviderRef"),
			CaptureDisposition: stringStatus(item, "captureDisposition"),
			CaptureReason:      stringStatus(item, "captureReason"),
			BlockingCondition:  blocking,
			Conditions:         conditions,
			ConditionReasons:   reasons,
		}
		classifyMobilityExplainDiagnostic(&report)
		return report, nil
	}
	return mobilityExplainReport{}, fmt.Errorf("MobilityPool/%s not found", pool)
}

// mobilityExplainConditions deliberately treats provider status as pool-level
// unless that status names the requested address. The owner table is the only
// per-address status projection; a failed or pending pool phase must not make
// unrelated addresses look like failed provider actions.
func mobilityExplainConditions(poolStatus, ownerRow map[string]any, address string) (map[string]string, map[string]string) {
	conditions := map[string]string{}
	reasons := map[string]string{}
	set := func(name, state, reason string) {
		conditions[name] = state
		if reason = strings.TrimSpace(reason); reason != "" {
			reasons[name] = reason
		}
	}

	switch stringutil.FirstNonEmpty(stringStatus(ownerRow, "state"), "Unknown") {
	case "OK":
		set("OwnershipResolved", "True", stringutil.FirstNonEmpty(stringStatus(ownerRow, "suppressionReason"), stringStatus(ownerRow, "class"), "ownership resolved"))
	case "Conflict":
		set("OwnershipResolved", "False", stringutil.FirstNonEmpty(stringStatus(ownerRow, "conflictReason"), "ownership conflict"))
	case "Stale":
		set("OwnershipResolved", "False", "stale ownership evidence")
	default:
		set("OwnershipResolved", "False", "ownership unknown")
	}

	if mobilityStatusIncludesAddress(poolStatus["providerActionFailedAddresses"], address) {
		set("ProviderActionApplied", "False", stringutil.FirstNonEmpty(stringStatus(poolStatus, "providerActionError"), "provider action failed"))
	} else {
		set("ProviderActionApplied", "Unknown", "address-specific provider action status unavailable")
	}
	if mobilityStatusIncludesAddress(poolStatus["providerObservationPendingAddresses"], address) {
		set("ProviderObserved", "False", "provider observation pending")
	} else {
		set("ProviderObserved", "Unknown", "address-specific provider observation status unavailable")
	}
	set("FIBInstalled", "Unknown", "verified by routerctl doctor sam")
	set("ReachabilityProbed", "Unknown", "verified by external dataplane probes")
	return conditions, reasons
}

func mobilityOwnerTableRow(value any, address string) (map[string]any, bool) {
	for _, row := range statusMaps(value) {
		if mobilityCanonicalAddress(stringStatus(row, "address")) == address {
			return row, true
		}
	}
	return nil, false
}

func mobilityStatusIncludesAddress(value any, address string) bool {
	for _, candidate := range mobilityStringSlice(value) {
		if mobilityCanonicalAddress(candidate) == address {
			return true
		}
	}
	return false
}

func mobilityCanonicalAddress(value string) string {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked().String()
	}
	if ip, err := netip.ParseAddr(value); err == nil && ip.Is4() {
		return netip.PrefixFrom(ip, 32).String()
	}
	return value
}

func mobilityExplainBlockingCondition(conditions map[string]string) string {
	for _, name := range []string{"OwnershipResolved", "ProviderActionApplied", "ProviderObserved", "FIBInstalled", "ReachabilityProbed"} {
		if conditions[name] == "False" {
			return name
		}
	}
	return ""
}

func mobilityExplainPhase(poolStatus map[string]any, blockingCondition string) string {
	phase := stringutil.FirstNonEmpty(stringStatus(poolStatus, "phase"), "Unknown")
	if blockingCondition != "" && (phase == "Unknown" || phase == "Ready") {
		return "Pending"
	}
	return phase
}

func classifyMobilityExplainDiagnostic(report *mobilityExplainReport) {
	if report == nil {
		return
	}
	if report.Class == "StaleCapture" && report.BlockingCondition == "OwnershipResolved" {
		report.Severity = "warning"
		report.Diagnostic = true
		report.DiagnosticReason = "stale capture evidence remains in status; use doctor-sam, provider action lifecycle, and dataplane matrix to decide whether it is an active blocker"
	}
}

func mobilityPathRows(statuses []routerstate.ObjectStatus, prefixFilter string) []mobilityPathRow {
	prefixFilter = strings.TrimSpace(prefixFilter)
	var rows []mobilityPathRow
	for _, status := range statuses {
		if status.Kind != "BGPRouter" {
			continue
		}
		for prefix, nextHops := range bgpstate.InstalledNextHopsFromStatus(status.Status["installedNextHops"]) {
			if prefixFilter != "" && prefix != prefixFilter {
				continue
			}
			rows = append(rows, mobilityPathRow{Router: status.Name, Prefix: prefix, NextHops: nextHops})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Router == rows[j].Router {
			return rows[i].Prefix < rows[j].Prefix
		}
		return rows[i].Router < rows[j].Router
	})
	return rows
}

func mobilityTrapRows(parts []routerstate.DynamicConfigPartRecord, addressFilter string) []mobilityTrapRow {
	addressFilter = strings.TrimSpace(addressFilter)
	var rows []mobilityTrapRow
	for _, part := range parts {
		if strings.TrimSpace(part.ActionPlansJSON) == "" {
			continue
		}
		plans, err := codec.DecodeActionPlans(part.ActionPlansJSON)
		if err != nil {
			continue
		}
		for _, plan := range plans {
			if !mobilityTrapAction(plan.Action) {
				continue
			}
			address := strings.TrimSpace(plan.Target["address"])
			if addressFilter != "" && address != addressFilter {
				continue
			}
			rows = append(rows, mobilityTrapRow{
				Source:         part.Source,
				Action:         plan.Action,
				Provider:       plan.Provider,
				ProviderRef:    stringutil.FirstNonEmpty(plan.ProviderRef, plan.Target["providerRef"]),
				NICRef:         plan.Target["nicRef"],
				Address:        address,
				IdempotencyKey: strings.TrimSpace(plan.IdempotencyKey),
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].Source == rows[j].Source {
			if rows[i].Address == rows[j].Address {
				return rows[i].Action < rows[j].Action
			}
			return rows[i].Address < rows[j].Address
		}
		return rows[i].Source < rows[j].Source
	})
	return rows
}

func mobilityTrapAction(action string) bool {
	switch strings.TrimSpace(action) {
	case "assign-secondary-ip", "unassign-secondary-ip", "ensure-forwarding-enabled", "ensure-forwarding-disabled":
		return true
	default:
		return false
	}
}

func writeMobilityPaths(stdout io.Writer, rows []mobilityPathRow, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "ROUTER\tPREFIX\tNEXT_HOPS")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\n", row.Router, row.Prefix, displayCell(strings.Join(row.NextHops, ",")))
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeMobilityTraps(stdout io.Writer, rows []mobilityTrapRow, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "SOURCE\tACTION\tPROVIDER\tPROVIDER_REF\tNIC\tADDRESS\tIDEMPOTENCY_KEY")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Source,
				row.Action,
				displayCell(row.Provider),
				displayCell(row.ProviderRef),
				displayCell(row.NICRef),
				displayCell(row.Address),
				displayCell(row.IdempotencyKey),
			)
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeMobilityExplain(stdout io.Writer, report mobilityExplainReport, output string) error {
	switch output {
	case "", "table":
		fmt.Fprintf(stdout, "Pool: %s\n", report.Pool)
		fmt.Fprintf(stdout, "Address: %s\n", report.Address)
		fmt.Fprintf(stdout, "Phase: %s\n", displayCell(report.Phase))
		if report.Severity != "" {
			fmt.Fprintf(stdout, "Severity: %s\n", report.Severity)
		}
		if report.DiagnosticReason != "" {
			fmt.Fprintf(stdout, "Diagnostic: %s\n", report.DiagnosticReason)
		}
		if report.OwnerNode != "" {
			fmt.Fprintf(stdout, "Owner: %s\n", report.OwnerNode)
		}
		if report.Class != "" {
			fmt.Fprintf(stdout, "Class: %s\n", report.Class)
		}
		if report.CaptureDisposition != "" {
			fmt.Fprintf(stdout, "Capture disposition: %s\n", report.CaptureDisposition)
		}
		if report.CaptureReason != "" {
			fmt.Fprintf(stdout, "Capture reason: %s\n", report.CaptureReason)
		}
		if report.BlockingCondition != "" {
			fmt.Fprintf(stdout, "Blocking condition: %s\n", report.BlockingCondition)
		} else {
			fmt.Fprintln(stdout, "Blocking condition: -")
		}
		fmt.Fprintln(stdout, "")
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "CONDITION\tSTATUS\tREASON")
		for _, name := range mapsort.Keys(report.Conditions) {
			fmt.Fprintf(w, "%s\t%s\t%s\n", name, report.Conditions[name], displayCell(report.ConditionReasons[name]))
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, report)
	case "yaml":
		return writeYAML(stdout, report)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func writeMobilityOwners(stdout io.Writer, rows []mobilityOwnerRow, output string) error {
	switch output {
	case "", "table":
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "POOL\tADDRESS\tSTATE\tCLASS\tOWNER\tOWNER_PROVIDER\tOWNER_NIC\tLOCAL_EVIDENCE\tLOCAL_SOURCE\tCAPTURE_HOLDER\tCAPTURE_DISPOSITION\tCAPTURE_REASON\tADVERTISE\tSUPPRESSION\tCONFLICT")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Pool,
				row.Address,
				displayCell(row.State),
				displayCell(row.Class),
				displayCell(row.OwnerNode),
				displayCell(row.OwnerProviderRef),
				displayCell(row.OwnerNICRef),
				displayCell(row.LocalEvidenceNode),
				displayCell(row.LocalEvidenceSource),
				displayCell(row.CaptureHolderNode),
				displayCell(row.CaptureDisposition),
				displayCell(row.CaptureReason),
				displayCell(row.AdvertiseOwnerNode),
				displayCell(row.SuppressionReason),
				displayCell(row.ConflictReason),
			)
		}
		return w.Flush()
	case "json":
		return writeJSON(stdout, rows)
	case "yaml":
		return writeYAML(stdout, rows)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func mobilityStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if value := strings.TrimSpace(fmt.Sprint(item)); value != "" {
				out = append(out, value)
			}
		}
		return out
	default:
		if value := strings.TrimSpace(fmt.Sprint(value)); value != "" && value != "<nil>" {
			return []string{value}
		}
	}
	return nil
}
