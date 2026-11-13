# launchpad-status-grid

Drives a Novation Launchpad as a desk status display for a
[mario](https://github.com/Soilytix/mario) worker. The pads show agent
sessions, repositories, subscription quota and burn, all read from the
worker's HTTP API. Beside them sit a visualiser of what the desk is playing
and the media keys.

This is mario's Launchpad board, moved out so it no longer weighs on mario:
the same screens drawing the same frames. Its data comes from the worker's
`board` and `session-list` API rows, the ones mario's desk client (`cappy`)
reads. It depends on none of mario's code.

## Layout

| Row (Launchpad) | Pads |
|---|---|
| 1, round buttons | play/pause at column 6 (with shift: next track); shift at column 8 |
| 2 | eight agent sessions, newest first; press runs `MARIO_LAUNCHPAD_FOCUS` |
| 3 | eight repositories: dirty, ahead/behind, unreadable |
| 4 | the five-hour burn against its median (columns 1–4) |
| 5 | a pad per stored subscription |
| 6–7 | each subscription's five-hour quota left; visualiser at columns 5–8 |
| 8–9 | its seven-day quota left; visualiser continues |

A window nobody measured is grey, never a colour that claims a quantity. The
pads mario's desk client still owns (CPU and RAM, the worker switch, and the
explain, note and task pads in column 9) stay dark here.

## Build

```sh
mise install          # Go and golangci-lint at the pinned versions
mise run build        # dist/launchpad-status-grid
mise run check        # lint, tidy, race tests, darwin cross-build, build
```

The Launchpad (CoreMIDI), live audio (Core Audio) and media keys need macOS
with cgo, which is the default on a Mac. Elsewhere the binary still builds;
`--preview` draws the board in the terminal, and the visualiser falls back to
its synthetic source.

## Run

```sh
export MARIO_WORKER_URL=https://<worker>      # or pass --worker
export MARIO_WORKER_TOKEN=<bearer>            # where the worker needs one
dist/launchpad-status-grid                    # on the attached Launchpad
dist/launchpad-status-grid --preview          # in the terminal, mirrored to a Launchpad if attached
```

| Variable | Meaning |
|---|---|
| `MARIO_WORKER_URL` | the worker's base URL; `/v1/board` and `/v1/session-list` are read under it |
| `MARIO_WORKER_TOKEN` | sent as `Authorization: Bearer …`. `cappy login`'s Keychain token is not read; a worker that refuses gives `401` |
| `MARIO_LAUNCHPAD_CONTEXT_LIMIT` | the context window session pads are measured against (default 200000) |
| `MARIO_LAUNCHPAD_FOCUS` | the command a session press runs through `sh -c`; `%d` directory, `%t` tool, `%i` session id. Unset, a press runs nothing and says so |
| `MARIO_LAUNCHPAD_AUDIO` | visualiser input: `system` (default), `microphone` or `synthetic` |
| `MARIO_LAUNCHPAD_AUDIO_FILE` | a 16-bit PCM WAV that drives the visualiser instead, for repeatable sound |

These are the same names cappy uses, so settings carry over.

Exit codes: 0 ok, 1 the board failed, 2 invalid invocation, 4 a dependency
unavailable (no worker URL, no Launchpad attached), 130 interrupted.
`--version` prints the version.

## Layout of the code

- `cmd/launchpad-status-grid/`: the composition root (flags, environment, the desk layout)
- `internal/launchpad/domain/`: pads, cells, frames and widgets
- `internal/launchpad/app/`: the board, the run loop and the controls (quota, burn, sessions, repositories, visualiser, media, shift)
- `internal/launchpad/adapter/`: `worker` (the HTTP API client), `coremidi`,
  `liveaudio`, `spectrum`, `audiofile`, `mediakeys`, `tui` (the terminal preview)

The worker's JSON is the contract. `internal/launchpad/adapter/worker/testdata`
holds mario's own board fixtures (`testdata/board` in soilytix/mario). When
mario changes that shape, copy them again and run `mise run check`.
