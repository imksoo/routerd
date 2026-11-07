# Pinned GoBGP community-send correction

Source: github.com/osrg/gobgp/v4 v4.7.0 (Apache-2.0; see LICENSE).
This local module retains the upstream Go source required by routerd; upstream
commands, documentation, generators and test assets are omitted.

The latest upstream release checked on 2026-10-30 was
[v4.10.0](https://github.com/osrg/gobgp/releases/tag/v4.10.0). Its
[`grpc_server.go`](https://github.com/osrg/gobgp/blob/v4.10.0/pkg/server/grpc_server.go),
[`server.go`](https://github.com/osrg/gobgp/blob/v4.10.0/pkg/server/server.go), and
[`oc/util.go`](https://github.com/osrg/gobgp/blob/v4.10.0/pkg/config/oc/util.go)
still omit this send-community conversion/filter. Merely upgrading the dependency
does not supply the missing path. This patch retains the already integrated
v4.7.0 API rather than coupling this repair to unrelated upstream changes.

Four files differ from v4.7.0:

- `pkg/server/grpc_server.go`: decode the public peer/peer-group send-community
  mask instead of silently discarding it; reject unsupported mask values.
- `pkg/config/oc/util.go`: return the selected mask in peer/group observations.
- `pkg/server/server.go`: apply the selector after outbound policy, on a cloned
  path so one neighbor cannot alter another neighbor's route or the local RIB.
- `internal/pkg/table/path.go`: remove standard or both extended attribute forms
  according to the selector.

The routerd contract remains `standard`, `extended`, or `both` (default). These
select standard and extended communities. Large communities are independent and
remain advertised in all three modes. Filtering does not alter withdrawals.

`cmd/routerd-bgp/community_wire_test.go` reads actual UPDATE messages with a raw
TCP BGP peer for each mode and both static/dynamic admission, and checks that the
local RIB retains every original attribute. The test also checks peer/group
observation and restoration through routerd's applied-peer constructors.
