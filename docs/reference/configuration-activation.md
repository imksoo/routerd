# Configuration commit and runtime activation

A candidate configuration becomes desired authority only after its canonical
file replacement is acknowledged durable. CLI apply and rollback, and the live
control API's apply/delete path use the same ordering:

1. Read, merge and validate the candidate, plan without running candidate
   controllers, record the attempted generation, and obtain HA admission.
2. Replace the canonical file and acknowledge both replacement and durability.
3. Refresh committed candidate PD inputs, activate and reconcile the selected
   runtime and HTTP listeners, then observe native reconciliation.
4. Record the actual terminal result and publish a response.

The existing live mutation gate owns the transaction from reading its canonical
base through activation and result publication. The selected Runner admits a
live request using its actual HA lease, consumes the canonical commit callback,
and runs its selected controllers once. A generation replacement transfers the
same locked lease descriptor without an unlock/reacquire gap. Adding HA obtains
one lease before commit and hands that lease to the replacement generation.
Standalone apply and rollback retain their existing native operation owner.
Pending generation history and
ordinary current-host inventory observations are bookkeeping; they do not
activate a candidate. Candidate PD resets and warning events follow the commit.

| Observed outcome | Canonical desired configuration | Candidate activation |
| --- | --- | --- |
| Validation or HA admission fails | Unchanged | None |
| Standby admission | Unchanged | None |
| Rename fails | Unchanged | None |
| Replacement is visible but durability is unconfirmed | Reported as replaced, durability unconfirmed | None |
| Durable replacement succeeds | New desired configuration | May begin |
| Activation or reconciliation later fails | New desired configuration remains | Report the acknowledged active runtime and partial effects |

A nil writer error alone is insufficient: both the replacement and durability
acknowledgements are required. A directory-sync error can leave new bytes visible
without authorizing any candidate worker. An explicit subsequent apply may obtain
durability acknowledgement and activate them. `NoReconcile` persists intent
without runtime activation; dry-run is read-only. Serve's explicit last-good
boot fallback uses its already-persisted generation without rewriting an invalid
canonical file.

Native effects cannot be made atomic with a later file rename or fsync. For that
reason, postcommit failures do not silently rewrite old canonical intent. A
failed runtime reload can acknowledge restoration of its previous generation;
that describes the actual active runtime, while the new durable canonical
configuration remains desired. An accepted reload whose actual state is unknown
retains that uncertainty. A failed terminal history write or response write also
does not reverse an acknowledged commit or manufacture success.

The commit callback belongs to the existing apply attempt and is consumed at
most once by the selected Runner after admission. It acknowledges durability
before candidate activation. The existing control-http controller owns listener
preparation and publication as a required effect; the API handler has no second
HTTP publisher after terminal history. A failed effect returns an error before
a successful terminal result. A selected controller graph is not proof that
every native resource or HTTP listener has converged. A commit-only request does
not enter the Runner.

Local regressions use separate real SQLite DNS logs and the actual LogRetention
controller/Runner to verify rejected writes cannot prune candidate logs, durable
activation can, and an explicit durable retry works after unconfirmed fsync.
Other regressions cover actual runtime cancellation after commit, held-lease
standby, live leader admission, introduction of HA, HTTP token and listener
changes, bind failure and retry, terminal failure and truthful desired/active
snapshots. These checks do
not qualify native forwarding, native OS service installation or crash durability
on target storage.
