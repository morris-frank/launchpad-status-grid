# launchpad-status-grid working agreement

For people and coding agents. The enforcement is the decision (`.golangci.yml`,
the tests); this file only points at it.

- One Go module, one binary (`cmd/launchpad-status-grid`). `mise run check` is
  the definition of done.
- `internal/launchpad/domain` and `app` hold the board's rules and import no
  adapter, terminal library or `net/http` (depguard). Adapters implement app's
  ports; `cmd/` wires them.
- No import of `github.com/Soilytix/mario` (depguard). The worker's HTTP API is
  the only coupling. Its JSON fixtures live in
  `internal/launchpad/adapter/worker/testdata`, copied from mario's `testdata/board`.
- Unknown is not zero: an unread or unmeasured reading is drawn grey or dark,
  never as a quantity.
- Exit codes follow mario's contract: 0, 1, 2, 4, 130. stdout is the result;
  everything else goes to stderr.
- Shelling out is evidence: the one process start (`MARIO_LAUNCHPAD_FOCUS`)
  names its reason at the line.
- Add dependencies with `go get`, then `go mod tidy`; prefer the standard library.
