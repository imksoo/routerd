# DerivedEvent guard decisions

`emitWhen` and `retractWhen` are independent guards evaluated against the same
input snapshot. `all_true`/`any_true` select the assertion guard, while
`any_false`/`all_false` select retraction. When neither guard matches, the existing
asserted or retracted latch is retained. Assertion takes precedence when both
match. For example, with `all_true` and `all_false`, one true and one false input
retains the preceding decision in either direction.

Hysteresis rechecks these guards before publishing. A pending transition is
canceled when the current inputs retain the existing latch. The committed latch,
pending target and wall-clock deadline live in the existing DerivedEvent status.
An unchanged selected rule resumes them after restart. Removing a selected rule
retires that checkpoint, so readding it starts a fresh rule. Preparing an
unselected configuration does not change the checkpoint.

Required status reads return errors. A storage failure or malformed observation
preserves the preceding latch and pending deadline instead of becoming a false
input. Before committing, the same SQLite transaction checks the input object
versions and current Health/PD projections again. An intervening change or expiry
rejects the stale decision. Hysteresis remains a deadline plus a current guard
recheck; it does not prove continuous reachability during a process outage.

The checkpoint, public status notification and asserted/retracted output commit
in one transaction. A failed write advances none of them. After a lost reply,
rereading the checkpoint prevents repeating the committed transition. Local bus
delivery follows the commit and journal readers recover a missed wakeup. The
status owner and bus must use the same SQLite journal.

One controller loop owns reconciliation and the next timer. Its stop
acknowledgement means no detached callback remains able to publish. Negative
`hysteresis` durations are rejected. This branch uses the new checkpoint format;
old status-only DerivedEvent records are not a supported restart format.
