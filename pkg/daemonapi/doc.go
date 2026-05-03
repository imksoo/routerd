// Package daemonapi defines the shared local contract used by routerd-managed
// helper daemons.
//
// The contract is intentionally transport-neutral. The first transport is an
// HTTP+JSON API over a Unix domain socket, but these types are plain JSON
// envelopes so tests and future state DB ingestion do not depend on HTTP.
package daemonapi
