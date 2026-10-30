# DerivedEvent guard decisions

`emitWhen` and `retractWhen` are independent guards evaluated against the same
input snapshot. `all_true`/`any_true` select the assertion guard, while
`any_false`/`all_false` select retraction. When neither guard matches, the existing
asserted or retracted latch is retained. Assertion takes precedence when both
match. For example, with `all_true` and `all_false`, one true and one false input
retains the preceding decision in either direction.

Hysteresis rechecks these guards before publishing. A pending transition is
canceled when the current inputs retain the existing latch. This checkpoint
corrects guard behavior only; durable latch recovery, required-read uncertainty
and atomic status/event publication remain separate implementation work.
