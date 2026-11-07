// SPDX-License-Identifier: BSD-3-Clause

package mobility

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	routerstate "github.com/imksoo/routerd/pkg/state"
)

// mobilityFederationFactID binds the immutable fields of one durable fact.
// Reobserving exactly that fact can refresh its lease; changed observations
// retain their own history under a new ID and the same grouping DedupeKey.
func mobilityFederationFactID(event routerstate.EventRecord) string {
	identity := struct {
		Group, Source, Type, Subject, Dedupe string
		Payload                              map[string]string
	}{event.Group, event.SourceNode, event.Type, event.Subject, event.DedupeKey, event.Payload}
	data, _ := json.Marshal(identity) // This fixed string/map envelope cannot fail.
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
