# Sandbox Backends

## Purpose

Sandbox backends isolate agent processes per project — each project directory runs inside its own container with scoped filesystem, network, and capability restrictions.

The state layer knows only `LaunchPlan.Project` (the project path); it has no awareness of which backend is active. Backend selection and command wrapping live in the runtime layer; container lifecycle lives in the `sandbox/` package.

Backends form a closed set. Docker is the production implementation; Firecracker has a PoC measurement but is not yet wired.

## Layer Responsibilities

| Layer | Sandbox role |
|---|---|
| `state/` | Holds `LaunchPlan.Project`. Backend-agnostic |
| `runtime/` | `AgentLauncher` wraps `LaunchPlan` into `WrappedLaunch{Command, Cleanup}`. `SandboxDispatcher` resolves which launcher (direct / docker) to use per project |
| `sandbox/` | `Manager[I any]` interface + backend implementations. Owns container lifecycle only; does not import driver / lib / runtime / tui |

`sandbox/` is tool-agnostic. It does not contain knowledge of any specific tool (e.g. Claude). Tool-specific host paths are declared by the user in `~/.roost/settings.toml`; they are never hardcoded in Go source.

## Design Decisions

| Decision | Choice | Rationale |
|---|---|---|
| Backend abstraction | `sandbox.Manager[I any]` + typed `Instance[I]` | Eliminates `any` and forcetypeasserts. Backend-specific state (e.g. `*docker.ContainerState`) is carried as the type parameter |
| Instance scope | Per-project per-image, frames share via ref-count | Multiple frames in the same (project, image) share one container. `AcquireFrame` / `ReleaseFrame` manage the ref-count; `DestroyInstance` is called when the count reaches zero |
| Config resolution | User scope + project scope merged by `config.SandboxResolver` with mtime-based caching | Default direct mode; individual repos opt into docker without daemon restart |
| Lifecycle and effect | detach → `EffDetachClient` only (container survives); shutdown → `EffReleaseFrameSandboxes` then `EffKillSession` (container destroyed); SIGINT (ctx cancel) → same as detach | Container lifetime decisions are expressed as state-layer effects, ordered in the event loop rather than a defer stack |
| Crash recovery | `PruneOrphans` at daemon startup enumerates roost-managed containers and kills any whose project is not in sessions.json, or whose image label no longer matches the resolved config | Covers SIGKILL / panic paths where defer and effects never run. sessions.json is the ground truth |
| Image resolution | User config > `.devcontainer/devcontainer.json` > built-in default | Projects with devcontainer.json need zero extra config. An explicit user setting always wins (no-fallback principle) |

## Frame Lifecycle Interaction

**New frame**
`AgentLauncher.WrapLaunch` → `EnsureInstance` (singleflight-serialized per project+image) → `AcquireFrame` → the resulting `WrappedLaunch` is embedded in `EffSpawnTmuxWindow`

**Warm start**
`AdoptFrame` reclaims the still-running container and increments the ref-count for each restored frame

**Frame exit / shutdown**
`Cleanup` callback → `ReleaseFrame` → if count reaches zero → `DestroyInstance`

**Daemon startup**
`PruneOrphans` kills containers outside the known project set or with a stale image label

## Docker Backend

### Container Shape

One long-lived container per (project, image) pair idles between frame activations. Frames join via `docker exec -it` rather than spawning a new container per frame. This amortizes container start latency across all frames of the same project — tmux panes open and close frequently; containers do not.

### Identity and Reclamation

The container name is a deterministic hash of (project, image). Because the name is fully derived from inputs, the daemon can locate and reclaim an existing container after a restart without consulting any persistent state. Ownership metadata (roost management marker, project path, image) is stored as container labels so `PruneOrphans` can enumerate them with a label filter alone. `sessions.json` is the authoritative project set; container labels are the container-side ground truth.

### Host Path Parity

The project directory and HOME are bind-mounted at the **same paths inside the container as on the host**. Agent-reported paths (transcript files, the roost socket) therefore resolve on both sides without translation. UID/GID are set to the host user's values for the same reason — no permission mapping is needed and no path fallback is required in the driver (no-fallback principle).

### Security Defaults

Each roost daemon uses a dedicated docker network created on demand, isolating project containers from each other and from the host network. All Linux capabilities are dropped and `no-new-privileges` is set at container creation time.

### Image Resolution

Priority: user config image > `.devcontainer/devcontainer.json` `image` field > built-in default. Only the `image` key of devcontainer.json is parsed; full devcontainer spec support is out of scope.

### Host Mounts

`[sandbox.docker.host_mounts]` bind-mounts host paths into every container managed by roost. Keys are host paths (tilde-expanded); values are `"rw"` or `"ro"`. Paths that do not exist on the host are skipped silently.

These mounts apply once at container creation (per-project, not per-frame). The docker backend is tool-agnostic — it does not know which paths are needed for any particular agent. It is the user's responsibility to declare the paths their agents require.

```toml
[sandbox.docker.host_mounts]
"~/.claude"      = "rw"
"~/.claude.json" = "rw"
```

To use Claude Code inside containers with the credential proxy enabled, omit `~/.claude` (so the host credential store is not exposed) and list only the non-credential subdirs:

```toml
[sandbox.proxy]
enabled = true

[sandbox.docker.host_mounts]
"~/.claude.json"          = "rw"
"~/.claude/settings.json" = "ro"
"~/.claude/projects"      = "rw"
"~/.claude/todos"         = "rw"
"~/.claude/statsig"       = "rw"
"~/.claude/ide"           = "rw"
```

### Concurrency

Concurrent `EnsureInstance` calls for the same (project, image) are serialized via `singleflight` to prevent duplicate containers from being started. The in-memory registry (`Manager.containers`) is the source of truth once the container is registered; subsequent calls return the cached entry without hitting Docker.

## Credential Proxy

When `[sandbox.proxy] enabled = true`, roost starts an in-process HTTP forward proxy backed by the [`credproxy`](https://github.com/takezoh/credproxy) library. The proxy listens on an ephemeral loopback port (`127.0.0.1:0`) and is reached from containers via `host.docker.internal`. Its lifetime is tied to the roost process — no external daemon is needed.

### AWS SSO Credentials (multi-profile)

The proxy generates a synthetic `~/.aws/config` inside each container. Every profile entry uses `credential_process` to call back to the roost proxy via a small helper script (`/opt/roost/aws-creds`). Both the config and the script are bind-mounted read-only; no credentials are stored inside the container.

**Proxy route:** `/aws-credentials/<profile>` — returns `credential_process`-format JSON (`Version:1`, `AccessKeyId`, `SecretAccessKey`, `SessionToken`, `Expiration`).

Two container env vars carry the proxy coordinates:

| Container env var | Value |
|---|---|
| `ROOST_AWS_TOKEN` | Ephemeral bearer token (never written to disk) |
| `ROOST_PROXY_PORT` | TCP port of the in-process proxy |

**Per-project profile configuration** — in the project's `.roost/settings.toml`:

```toml
# <project-root>/.roost/settings.toml
[sandbox.proxy]
aws_profiles = ["default", "master", "general"]
```

Each name in the array appears as a `[profile <name>]` section in the synthetic `~/.aws/config`. Including `"default"` adds a `[default]` section so `aws` commands without `--profile` also work. Profiles not listed are not reachable from the container. If `aws_profiles` is absent or empty, no synthetic config is mounted and AWS proxy is a no-op for that project.

Enable the proxy in the global `~/.roost/settings.toml`:

```toml
[sandbox.proxy]
enabled = true
```

The provider calls `aws configure export-credentials --format process --profile <name>` on the host, then falls back to reading `~/.aws/sso/cache/*.json`. Run `aws sso login` on the host before starting containers. `~/.aws/sso/cache` is never bind-mounted — containers obtain short-lived credentials through the proxy only.

**Container image requirement:** `curl` must be available (present in standard base images; document explicitly for minimal images).

### Claude Code (Subscription)

Claude Code uses OAuth subscription credentials stored in `~/.claude/.credentials.json`. Container-side auth state is determined by the presence of this file; environment variables alone are not sufficient for the interactive UI to show a logged-in state.

Bind-mount `~/.claude` via `host_mounts` for subscription use:

```toml
[sandbox.docker.host_mounts]
"~/.claude"      = "rw"
"~/.claude.json" = "rw"
```

This exposes the OAuth refresh token to the container. Accept this trade-off or restrict write access to specific subdirs if the threat model requires tighter isolation.

### gcloud CLI

Bind-mounting `~/.config/gcloud` exposes the OAuth refresh token. Instead, roost can generate a synthetic `CLOUDSDK_CONFIG` directory and refresh a short-lived access token on the host, so containers receive only the access token (≤1h TTL).

**Per-project configuration** — in the project's `.roost/settings.toml`:

```toml
[sandbox.proxy.gcp]
account  = "user@example.com"            # from `gcloud auth list`
projects = ["proj-prod", "proj-staging"] # GCP project IDs; first entry is the active default
```

When `account` and `projects` are set, roost:

- Calls `gcloud auth print-access-token --account=<account>` on the host every 50 minutes and writes the result to `<dataDir>/gcp/<hash>/access-token`.
- Generates a synthetic `CLOUDSDK_CONFIG` directory with one `configurations/config_<projectId>` per listed project. Each configuration sets `auth/access_token_file` to `/opt/roost/gcp-token`.
- Bind-mounts both files read-only into the container.
- Injects `CLOUDSDK_CONFIG=/opt/roost/gcloud-config` into the container environment.

Inside the container:

```sh
gcloud config list                               # shows active project (first listed)
gcloud --configuration=proj-staging projects list  # switch to another project
gcloud --project=proj-staging storage ls          # also works
```

`~/.config/gcloud` is never bind-mounted. `gcloud auth login` inside the container will fail (read-only mount) — authenticate on the host before starting containers.

The first token refresh is synchronous at container start. If `gcloud auth print-access-token` fails (not logged in), a warning is logged and the container's `gcloud` calls will receive 401 until the host re-authenticates.

**Container image requirement:** `gcloud` must be installed in the image.
