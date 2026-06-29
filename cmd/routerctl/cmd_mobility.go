// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/imksoo/routerd/pkg/api"
	"github.com/imksoo/routerd/pkg/config"
	"github.com/imksoo/routerd/pkg/controlapi"
	"github.com/imksoo/routerd/pkg/dynamicconfig"
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
	AdvertiseOwnerNode    string `json:"advertiseOwnerNode,omitempty" yaml:"advertiseOwnerNode,omitempty"`
	SuppressionReason     string `json:"suppressionReason,omitempty" yaml:"suppressionReason,omitempty"`
	ConflictReason        string `json:"conflictReason,omitempty" yaml:"conflictReason,omitempty"`
}

type mobilityExplainReport struct {
	Pool                 string            `json:"pool" yaml:"pool"`
	Address              string            `json:"address" yaml:"address"`
	Phase                string            `json:"phase" yaml:"phase"`
	Severity             string            `json:"severity,omitempty" yaml:"severity,omitempty"`
	Diagnostic           bool              `json:"diagnostic,omitempty" yaml:"diagnostic,omitempty"`
	DiagnosticReason     string            `json:"diagnosticReason,omitempty" yaml:"diagnosticReason,omitempty"`
	Health               string            `json:"health,omitempty" yaml:"health,omitempty"`
	Class                string            `json:"class,omitempty" yaml:"class,omitempty"`
	OwnerNode            string            `json:"ownerNode,omitempty" yaml:"ownerNode,omitempty"`
	CaptureHolderNode    string            `json:"captureHolderNode,omitempty" yaml:"captureHolderNode,omitempty"`
	OwnerProviderRef     string            `json:"ownerProviderRef,omitempty" yaml:"ownerProviderRef,omitempty"`
	AssignmentGeneration string            `json:"assignmentGeneration,omitempty" yaml:"assignmentGeneration,omitempty"`
	ProviderAction       string            `json:"providerAction,omitempty" yaml:"providerAction,omitempty"`
	ProviderActionKey    string            `json:"providerActionKey,omitempty" yaml:"providerActionKey,omitempty"`
	BlockingCondition    string            `json:"blockingCondition,omitempty" yaml:"blockingCondition,omitempty"`
	Conditions           map[string]string `json:"conditions" yaml:"conditions"`
	ConditionReasons     map[string]string `json:"conditionReasons,omitempty" yaml:"conditionReasons,omitempty"`
}

type mobilityEnrollmentJoinResult struct {
	Accepted      bool      `json:"accepted" yaml:"accepted"`
	ClaimRef      string    `json:"claimRef" yaml:"claimRef"`
	RRSetRef      string    `json:"rrSetRef" yaml:"rrSetRef"`
	DynamicSource string    `json:"dynamicSource" yaml:"dynamicSource"`
	Generation    int64     `json:"generation" yaml:"generation"`
	ObservedAt    time.Time `json:"observedAt" yaml:"observedAt"`
	ExpiresAt     time.Time `json:"expiresAt" yaml:"expiresAt"`
	StateFile     string    `json:"stateFile" yaml:"stateFile"`
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
	case "enrollment-join":
		return mobilityEnrollmentJoinCommand(args[1:], stdout)
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

func mobilityEnrollmentJoinCommand(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("mobility enrollment-join", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() {
		printSubcommandHelp(fs,
			"Submit a leaf SAMEnrollmentClaim, fetch its SAMRRSet, and persist the RRSet into local dynamic state.",
			"routerctl mobility enrollment-join --config leaf.yaml --claim pve-leaf-b --rr-url http://pve-rr:8080 --state-file /var/lib/routerd/routerd.db\n"+
				"routerctl mobility enrollment-join --config leaf.yaml --claim pve-leaf-a --rr-socket /run/routerd/routerd.sock")
	}
	configPath := fs.String("config", defaultConfigPath(), "leaf config containing the SAMEnrollmentClaim")
	claimName := fs.String("claim", "", "SAMEnrollmentClaim name")
	rrSocketPath := fs.String("rr-socket", "", "RR routerd Unix domain socket path")
	rrURL := fs.String("rr-url", "", "RR routerd control API base URL")
	statePath := fs.String("state-file", defaultStatePath(), "local leaf routerd state database file")
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
		return fmt.Errorf("unexpected mobility enrollment-join argument %q", fs.Arg(0))
	}
	if strings.TrimSpace(*claimName) == "" {
		return errors.New("mobility enrollment-join requires --claim")
	}
	if strings.TrimSpace(*rrSocketPath) != "" && strings.TrimSpace(*rrURL) != "" {
		return errors.New("mobility enrollment-join accepts only one of --rr-socket or --rr-url")
	}
	router, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	claimResource, err := mobilityEnrollmentClaimResource(router, *claimName)
	if err != nil {
		return err
	}
	claim, err := claimResource.SAMEnrollmentClaimSpec()
	if err != nil {
		return err
	}
	rrSetName, err := mobilityRRSetNameFromRef(claim.RRSetRef)
	if err != nil {
		return err
	}
	client := mobilityEnrollmentClient(*rrSocketPath, *rrURL)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	submitResult, err := client.SubmitSAMEnrollmentClaim(ctx, controlapi.SAMEnrollmentClaimSubmitRequest{Claim: claimResource})
	if err != nil {
		return fmt.Errorf("submit SAMEnrollmentClaim to routerd failed: %w", err)
	}
	rrSetResult, err := client.GetSAMRRSet(ctx, controlapi.SAMRRSetGetRequest{Name: rrSetName, ClaimRef: "SAMEnrollmentClaim/" + claimResource.Metadata.Name})
	if err != nil {
		return fmt.Errorf("fetch SAMRRSet from routerd failed: %w", err)
	}
	record, err := mobilityFetchedSAMRRSetRecord(rrSetResult.RRSet, submitResult.ObservedAt, submitResult.ExpiresAt)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(strings.TrimSpace(*statePath)), 0755); err != nil {
		return err
	}
	store, err := routerstate.OpenSQLite(*statePath)
	if err != nil {
		return fmt.Errorf("open local leaf state database %s: %w", *statePath, err)
	}
	defer store.Close()
	if err := store.UpsertDynamicConfigPart(record); err != nil {
		return err
	}
	result := mobilityEnrollmentJoinResult{
		Accepted:      submitResult.Accepted,
		ClaimRef:      submitResult.ClaimRef,
		RRSetRef:      "SAMRRSet/" + rrSetName,
		DynamicSource: record.Source,
		Generation:    record.Generation,
		ObservedAt:    record.ObservedAt,
		ExpiresAt:     record.ExpiresAt,
		StateFile:     *statePath,
	}
	switch output {
	case "", "table", "text":
		fmt.Fprintf(stdout, "accepted\t%s\nrrSet\t%s\ndynamicSource\t%s\ngeneration\t%d\nexpiresAt\t%s\nstateFile\t%s\n", result.ClaimRef, result.RRSetRef, result.DynamicSource, result.Generation, result.ExpiresAt.Format(time.RFC3339), result.StateFile)
		return nil
	case "json":
		return writeJSON(stdout, result)
	case "yaml":
		return writeYAML(stdout, result)
	default:
		return fmt.Errorf("unsupported output %q", output)
	}
}

func mobilityEnrollmentClient(socketPath, baseURL string) *controlapi.Client {
	if strings.TrimSpace(baseURL) != "" {
		return controlapi.NewHTTPClient(baseURL)
	}
	if strings.TrimSpace(socketPath) == "" {
		socketPath = defaultSocketPath()
	}
	return controlapi.NewUnixClient(socketPath)
}

func mobilityRRSetNameFromRef(ref string) (string, error) {
	kind, name, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok || kind != "SAMRRSet" || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("rrSetRef must reference SAMRRSet/<name>")
	}
	return strings.TrimSpace(name), nil
}

func mobilityFetchedSAMRRSetRecord(resource api.Resource, observedAt, expiresAt time.Time) (routerstate.DynamicConfigPartRecord, error) {
	if resource.APIVersion != api.MobilityAPIVersion || resource.Kind != "SAMRRSet" || strings.TrimSpace(resource.Metadata.Name) == "" {
		return routerstate.DynamicConfigPartRecord{}, fmt.Errorf("fetched resource must be %s/SAMRRSet", api.MobilityAPIVersion)
	}
	if _, err := resource.SAMRRSetSpec(); err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	if expiresAt.IsZero() {
		expiresAt = observedAt.Add(24 * time.Hour)
	}
	part := dynamicconfig.DynamicConfigPart{
		TypeMeta: api.TypeMeta{APIVersion: dynamicconfig.ConfigAPIVersion, Kind: "DynamicConfigPart"},
		Metadata: api.ObjectMeta{
			Name: "fetched-sam-rrset-" + resource.Metadata.Name,
			OwnerRefs: []api.OwnerRef{{
				APIVersion: api.MobilityAPIVersion,
				Kind:       "SAMRRSet",
				Name:       resource.Metadata.Name,
			}},
		},
		Spec: dynamicconfig.DynamicConfigPartSpec{
			Source:     "SAMRRSet/" + resource.Metadata.Name,
			Generation: 1,
			ObservedAt: observedAt.UTC(),
			ExpiresAt:  expiresAt.UTC(),
			Resources:  []api.Resource{resource},
		},
	}
	part.Spec.Digest = digestMobilityDynamicPart(part)
	resources, err := json.Marshal(part.Spec.Resources)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	directives, err := json.Marshal(part.Spec.Directives)
	if err != nil {
		return routerstate.DynamicConfigPartRecord{}, err
	}
	return routerstate.DynamicConfigPartRecord{
		Source:         part.Spec.Source,
		Generation:     part.Spec.Generation,
		ObservedAt:     part.Spec.ObservedAt,
		ExpiresAt:      part.Spec.ExpiresAt,
		Digest:         part.Spec.Digest,
		ResourcesJSON:  string(resources),
		DirectivesJSON: string(directives),
		Status:         "active",
	}, nil
}

func digestMobilityDynamicPart(part dynamicconfig.DynamicConfigPart) string {
	data, err := json.Marshal(part.Spec)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
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
			"Explain one SAM address from MobilityPool address-level conditions.",
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
	fmt.Fprintln(w, "  enrollment-join --config <path> --claim <name> [--rr-socket <path>|--rr-url <url>] [--state-file <path>] [-o table|json|yaml]")
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
		if len(table) == 0 {
			table = statusMaps(status.Status["ownershipResolverOwnerTable"])
		}
		for _, item := range table {
			address := stringStatus(item, "address")
			if addressFilter != "" && address != addressFilter {
				continue
			}
			rows = append(rows, mobilityOwnerRow{
				Pool:                  status.Name,
				Address:               address,
				State:                 firstNonEmpty(stringStatus(item, "state"), "Unknown"),
				Class:                 stringStatus(item, "class"),
				OwnerNode:             firstNonEmpty(stringStatus(item, "ownerNode"), stringStatus(item, "homeOwnerNode")),
				OwnerProviderRef:      stringStatus(item, "ownerProviderRef"),
				OwnerNICRef:           stringStatus(item, "ownerNICRef"),
				OwnerResourceRef:      stringStatus(item, "ownerResourceRef"),
				LocalEvidenceNode:     firstNonEmpty(stringStatus(item, "localEvidenceNode"), stringStatus(item, "localNode")),
				LocalEvidenceSource:   firstNonEmpty(stringStatus(item, "localEvidenceSource"), stringStatus(item, "localSource")),
				LocalEvidenceNICRef:   firstNonEmpty(stringStatus(item, "localEvidenceNICRef"), stringStatus(item, "localNICRef")),
				LocalEvidenceResource: firstNonEmpty(stringStatus(item, "localEvidenceResourceRef"), stringStatus(item, "localResourceRef")),
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
	address = strings.TrimSpace(address)
	for _, status := range statuses {
		if status.APIVersion != api.MobilityAPIVersion || status.Kind != "MobilityPool" || status.Name != pool {
			continue
		}
		addresses := statusMap(status.Status["addresses"])
		if len(addresses) == 0 {
			return mobilityExplainReport{}, fmt.Errorf("MobilityPool/%s has no address-level status; reconcile with a routerd version that writes status.addresses", pool)
		}
		item := statusMap(addresses[address])
		if len(item) == 0 {
			return mobilityExplainReport{}, fmt.Errorf("MobilityPool/%s has no address status for %s", pool, address)
		}
		report := mobilityExplainReport{
			Pool:                 pool,
			Address:              address,
			Phase:                firstNonEmpty(stringStatus(item, "phase"), stringStatus(status.Status, "phase")),
			Health:               stringStatus(status.Status, "health"),
			Class:                stringStatus(item, "class"),
			OwnerNode:            stringStatus(item, "ownerNode"),
			CaptureHolderNode:    stringStatus(item, "captureHolderNode"),
			OwnerProviderRef:     stringStatus(item, "ownerProviderRef"),
			AssignmentGeneration: stringStatus(item, "assignmentGeneration"),
			ProviderAction:       stringStatus(item, "providerAction"),
			ProviderActionKey:    stringStatus(item, "providerActionKey"),
			BlockingCondition:    stringStatus(item, "blockingCondition"),
			Conditions:           stringMapStatus(item["conditions"]),
			ConditionReasons:     stringMapStatus(item["conditionReasons"]),
		}
		classifyMobilityExplainDiagnostic(&report)
		return report, nil
	}
	return mobilityExplainReport{}, fmt.Errorf("MobilityPool/%s not found", pool)
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
		for prefix, nextHops := range mobilityInstalledNextHops(status.Status["installedNextHops"]) {
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

func mobilityInstalledNextHops(value any) map[string][]string {
	out := map[string][]string{}
	switch typed := value.(type) {
	case map[string][]string:
		for prefix, hops := range typed {
			out[strings.TrimSpace(prefix)] = cleanMobilityStrings(hops)
		}
	case map[string]any:
		for prefix, raw := range typed {
			out[strings.TrimSpace(prefix)] = cleanMobilityStrings(mobilityStringSlice(raw))
		}
	}
	return out
}

func mobilityTrapRows(parts []routerstate.DynamicConfigPartRecord, addressFilter string) []mobilityTrapRow {
	addressFilter = strings.TrimSpace(addressFilter)
	var rows []mobilityTrapRow
	for _, part := range parts {
		if strings.TrimSpace(part.ActionPlansJSON) == "" {
			continue
		}
		var plans []dynamicconfig.ActionPlan
		if err := json.Unmarshal([]byte(part.ActionPlansJSON), &plans); err != nil {
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
				ProviderRef:    firstNonEmpty(plan.ProviderRef, plan.Target["providerRef"]),
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
		if report.AssignmentGeneration != "" {
			fmt.Fprintf(stdout, "Assignment generation: %s\n", report.AssignmentGeneration)
		}
		if report.ProviderAction != "" {
			fmt.Fprintf(stdout, "Provider action: %s", report.ProviderAction)
			if report.ProviderActionKey != "" {
				fmt.Fprintf(stdout, " (%s)", report.ProviderActionKey)
			}
			fmt.Fprintln(stdout)
		}
		if report.BlockingCondition != "" {
			fmt.Fprintf(stdout, "Blocking condition: %s\n", report.BlockingCondition)
		} else {
			fmt.Fprintln(stdout, "Blocking condition: -")
		}
		fmt.Fprintln(stdout, "")
		w := tabwriter.NewWriter(stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "CONDITION\tSTATUS\tREASON")
		for _, name := range sortedStringKeys(report.Conditions) {
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
		fmt.Fprintln(w, "POOL\tADDRESS\tSTATE\tCLASS\tOWNER\tOWNER_PROVIDER\tOWNER_NIC\tLOCAL_EVIDENCE\tLOCAL_SOURCE\tADVERTISE\tSUPPRESSION\tCONFLICT")
		for _, row := range rows {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				row.Pool,
				row.Address,
				displayCell(row.State),
				displayCell(row.Class),
				displayCell(row.OwnerNode),
				displayCell(row.OwnerProviderRef),
				displayCell(row.OwnerNICRef),
				displayCell(row.LocalEvidenceNode),
				displayCell(row.LocalEvidenceSource),
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

func stringMapStatus(value any) map[string]string {
	switch typed := value.(type) {
	case map[string]string:
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			out[k] = v
		}
		return out
	case map[string]any:
		out := make(map[string]string, len(typed))
		for k, v := range typed {
			if s := strings.TrimSpace(fmt.Sprint(v)); s != "" && s != "<nil>" {
				out[k] = s
			}
		}
		return out
	default:
		return nil
	}
}

func sortedStringKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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

func cleanMobilityStrings(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			seen[value] = true
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}
