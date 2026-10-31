// SPDX-License-Identifier: BSD-3-Clause

package chain

import (
	"os"
	"strings"
	"testing"

	"github.com/imksoo/routerd/pkg/api"
)

func TestNTPConfigReceiptTokenMustMatchTheNativeFileProof(t *testing.T) {
	for _, kind := range []string{"NTPClient", "NTPServer"} {
		t.Run(kind, func(t *testing.T) {
			f := newNTPLifecycleFixture(t, kind)
			name := f.router.Spec.Resources[0].Metadata.Name
			if err := f.reconcile(t.Context()); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(f.path)
			if err != nil {
				t.Fatal(err)
			}
			owner := ntpConfigOwner{store: f.store, kind: kind}
			record, err := owner.get(name)
			if err != nil || record == nil {
				t.Fatalf("record=%+v err=%v", record, err)
			}
			replacement := *record
			replacement.Token = strings.Repeat("f", 32)
			if replacement.Token == record.Token {
				replacement.Token = strings.Repeat("e", 32)
			}
			// The database now names a different owner. The native file still has
			// the old owner's proof, even though its exact content hash is known.
			if err := owner.save(name, record, &replacement); err != nil {
				t.Fatal(err)
			}
			f.reopen(t)
			f.router = &api.Router{}
			f.commands = nil
			reconcileErr := f.reconcile(t.Context())
			after, readErr := os.ReadFile(f.path)
			if reconcileErr == nil || readErr != nil || string(after) != string(before) || len(f.commands) != 0 {
				t.Fatalf("receipt/file proof mismatch granted mutation: err=%v nativeRead=%v preserved=%t service=%v", reconcileErr, readErr, string(after) == string(before), f.commands)
			}
		})
	}
}
