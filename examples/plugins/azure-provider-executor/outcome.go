// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"
)

// A local launch failure proves the requested provider command did not run.
// Every other write failure is uncertain unless an authoritative read confirms
// its result. Human-readable diagnostic text cannot authorize a second write.
type commandNotStartedError struct{ err error }

func (e *commandNotStartedError) Error() string { return e.err.Error() }
func (e *commandNotStartedError) Unwrap() error { return e.err }

func mutationFailure(prior executeActionResult, message string, err error) executeActionResult {
	result := failed(message, err)
	result.Status.UndoAvailable = prior.Status.UndoAvailable
	if result.Status.Observed == nil {
		result.Status.Observed = map[string]string{}
	}
	for key, value := range prior.Status.Observed {
		result.Status.Observed[key] = value
	}
	var notStarted *commandNotStartedError
	if !errors.As(err, &notStarted) {
		result.Status.Status = statusOutcomeUnknown
	}
	return result
}

// A previous successful write or failed post-write observation makes the whole
// operation uncertain even if its next command never started.
func uncertainResult(prior executeActionResult, message string, err error) executeActionResult {
	result := mutationFailure(prior, message, err)
	result.Status.Status = statusOutcomeUnknown
	return result
}

// Provider capture operates on an IPv4 host, never a broader route or IPv6.
func capturedIPv4Address(value string) (string, error) {
	value = strings.TrimSpace(value)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		if !prefix.Addr().Is4() || prefix.Bits() != 32 {
			return "", fmt.Errorf("capture address %q must be an IPv4 host or /32", value)
		}
		return prefix.Addr().String(), nil
	}
	address, err := netip.ParseAddr(value)
	if err != nil || !address.Is4() {
		return "", fmt.Errorf("invalid IPv4 capture address %q", value)
	}
	return address.String(), nil
}

func nicResourceIdentity(id string) (subscription, group, name string, err error) {
	parts := strings.Split(strings.Trim(id, "/"), "/")
	if len(parts) != 8 || !strings.EqualFold(parts[0], "subscriptions") || !strings.EqualFold(parts[2], "resourceGroups") || !strings.EqualFold(parts[4], "providers") || !strings.EqualFold(parts[5], "Microsoft.Network") || !strings.EqualFold(parts[6], "networkInterfaces") || parts[1] == "" || parts[3] == "" || parts[7] == "" {
		return "", "", "", fmt.Errorf("invalid Azure NIC resource ID %q", id)
	}
	return parts[1], parts[3], parts[7], nil
}
