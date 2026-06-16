# OpenShell Sandbox Backend

> **Status: Design — not yet implemented.**
> This document specifies the integration of NVIDIA OpenShell as the
> security and governance layer for Astonish's Kubernetes sandbox tier.
> The OpenShell backend is a new implementation of the existing
> `SandboxBackend` interface (§3 of `sandbox-backends.md`) that routes
> all sandbox operations through the OpenShell Gateway and runs
> sandboxed processes under OpenShell's 4-layer security model
> (Landlock, seccomp, network namespace, OCSF audit).
>
> The existing K8s backend (`pkg/sandbox/k8s/`) remains available for
> deployments that do not run OpenShell. Backend selection is via
> configuration: `sandbox.backend: openshell` (new default) or
> `sandbox.backend: k8s` (existing).

## 1. Context & Motivation

The K8s backend (`sandbox-backends.md` §5) creates and manages sandbox
pods directly via the Kubernetes API. This works well for single-tenant
clusters but has limitations in regulated or multi-tenant environments:

- **No in-pod process isolation.** Once code executes inside the pod, it
  has full access to the overlay filesystem, can make arbitrary syscalls,
  and has unrestricted network egress within the pod's NetworkPolicy.
- **No per-process audit trail.** Kubernetes audit logs record pod/exec
  events but not what commands run or what files they access.
- **No inference routing control.** Agent tool calls that reach external
  LLM APIs are not mediated by any privacy or compliance layer.
- **No credential isolation.** Environment variables visible to PID 1 are
  visible to all exec'd processes.

NVIDIA OpenShell (https://github.com/NVIDIA/OpenShell) is an open-source
sandbox security framework that provides:

| Layer | Mechanism | What It Protects |
|-------|-----------|-----------------|
| **Filesystem** | Landlock LSM | Per-process read/write/exec access to paths |
| **Syscalls** | seccomp BPF | Blocks mount, mknod, pivot_root, ptrace for agent code |
| **Network** | Network namespace + policy proxy | Per-destination, per-binary, L7-aware egress control |
| **Audit** | OCSF structured logging | Every process spawn, file access denial, network connection |

OpenShell's model places a **supervisor binary as PID 1** inside each
sandbox container. The supervisor spawns agent processes as restricted
children — each child inherits Landlock rules, seccomp filters, and runs
in a constrained network namespace that forces traffic through a policy
proxy. Exec requests arrive via the OpenShell Gateway relay (not
`kubectl exec`), so the supervisor mediates every process entry point.

### Why OpenShell (not just hardened K8s pods)

| Requirement | K8s-only | K8s + OpenShell |
|-------------|----------|-----------------|
| Process-level filesystem isolation | No (pod-wide) | Yes (Landlock per-child) |
| Syscall filtering for agent code | No (pod-wide seccomp only) | Yes (per-process BPF) |
| Per-destination network policy | Pod-level NetworkPolicy | Per-process, L7, OPA-based |
| Credential isolation | Shared env | Supervisor strips secrets before child spawn |
| OCSF audit trail | No | Every spawn, denial, connection logged |
| Privacy/inference routing | No | Policy proxy intercepts LLM egress |

### Constraint: Overlay Filesystem Must Continue to Work

Astonish's template/layer system (§5.3, §5.11 of `sandbox-backends.md`)
is the foundation of the sandbox model. Every session composes a
multi-layer OverlayFS that provides a full Linux rootfs:

```
Session Upper   (writable, per-session)
Team Template   (lowerdir, shared by team)
Admin Layer     (lowerdir, org-wide "Build Base Layer")
@base           (lowerdir, full Debian rootfs from container image)
```

This overlay requires `mount(2)` (or `fuse-overlayfs`), `pivot_root(2)`,
and bind-mounts of `/dev`, `/proc`, `/sys`. These are privileged
operations that conflict with OpenShell's supervisor model (which
restricts child processes from performing mounts).

The architecture below resolves this conflict cleanly.

---

## 2. Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│ Astonish Control Plane                                                  │
│                                                                         │
│  pkg/sandbox/openshell.OpenShellBackend                                 │
│    implements SandboxBackend interface                                   │
│    communicates via gRPC to OpenShell Gateway                            │
└─────────────────────────┬───────────────────────────────────────────────┘
                          │ gRPC (TLS)
                          ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ OpenShell Gateway (Deployment in openshell-system namespace)             │
│                                                                         │
│  - Sandbox lifecycle: create, delete, watch state                        │
│  - Exec relay: gateway ↔ supervisor (SSH over Unix socket)              │
│  - File sync: upload/download via relay                                  │
│  - Policy management: per-sandbox policy YAML                           │
│  - Credential injection: resolved before passing to supervisor          │
│  - OCSF audit aggregation                                               │
│                                                                         │
│  K8s Driver (in-process):                                               │
│    Creates Sandbox CRD (agents.x-k8s.io/v1alpha1)                       │
│    Watches pod status                                                    │
│    Injects supervisor env, TLS material, relay config                    │
└─────────────────────────┬───────────────────────────────────────────────┘
                          │ Sandbox CRD → Pod
                          ▼
┌─────────────────────────────────────────────────────────────────────────┐
│ Sandbox Pod (namespace: astonish-sandboxes)                             │
│                                                                         │
│  Image: astonish-openshell-sandbox                                      │
│                                                                         │
│  ENTRYPOINT: /opt/astonish/bin/astonish-boot                            │
│    Phase A — Overlay Composition (privileged, before supervisor):        │
│      1. Read ASTONISH_LAYER_CHAIN, ASTONISH_SESSION_ID from env         │
│      2. Resume evicted upper from /mnt/astonish-uppers/ if exists       │
│      3. Mount fuse-overlayfs at /overlay/merged                         │
│           lowerdir = reverse(layer-chain) from /mnt/astonish-layers/    │
│           upperdir = /overlay/upper                                      │
│           workdir  = /overlay/work                                       │
│      4. Copy infrastructure binaries into overlay upper:                 │
│           /opt/astonish/bin/astonish → upper/usr/local/bin/astonish     │
│           /opt/openshell/bin/openshell-sandbox → upper/usr/local/bin/   │
│      5. Bind-mount /dev, /proc, /sys into /overlay/merged/              │
│      6. Bind-mount /etc/resolv.conf for DNS                             │
│      7. Bind-mount /overlay/upper → /overlay/merged/var/astonish/upper  │
│      8. Bind-mount /mnt/astonish-uppers → /overlay/merged/mnt/uppers   │
│      9. Bind-mount /mnt/astonish-layers → /overlay/merged/mnt/layers   │
│     10. pivot_root(/overlay/merged) — overlay becomes new /             │
│     11. Unmount old root at /.pivot_old                                  │
│                                                                         │
│    Phase B — Supervisor Startup (post-pivot, / = overlay):              │
│     12. exec /usr/local/bin/openshell-sandbox \                          │
│           --sandbox-id=$OPENSHELL_SANDBOX_ID \                           │
│           --openshell-endpoint=$OPENSHELL_ENDPOINT \                     │
│           --ssh-socket-path=/var/run/openshell/ssh.sock \                │
│           -- /bin/sleep infinity                                         │
│                                                                         │
│  After exec, PID 1 = openshell-sandbox (supervisor):                    │
│    - / = composed overlay (full Linux FS)                               │
│    - Connects to gateway relay (outbound gRPC)                          │
│    - Starts SSH server on Unix socket                                   │
│    - Spawns entrypoint (/bin/sleep infinity) as restricted child:       │
│        Landlock: / = read_write (overlay is the workspace)              │
│        seccomp: blocks mount, pivot_root, mknod, chroot                 │
│        privilege drop: sandbox:sandbox user                             │
│        network namespace: traffic forced through policy proxy           │
│    - Policy proxy enforces per-destination network allowlist            │
│    - OCSF audit logs every process spawn and denial                    │
│                                                                         │
│  On exec request (from Astonish via gateway relay):                     │
│    Gateway → supervisor SSH socket → fork child process:                │
│      - Apply Landlock ruleset (same as entrypoint)                      │
│      - Apply seccomp BPF filter                                         │
│      - Drop privileges (setuid/setgid to sandbox user)                  │
│      - Enter network namespace                                          │
│      - Set workdir (e.g., /root or /sandbox)                            │
│      - exec command (e.g., /usr/local/bin/astonish node)                │
│    Stdout/stderr relayed back: supervisor → gateway → Astonish          │
│                                                                         │
│  Volumes:                                                               │
│    /mnt/astonish-layers  (PVC, RO) — shared layer store                 │
│    /mnt/astonish-uppers  (PVC, RW) — eviction persistence               │
│    /overlay              (emptyDir) — upper + work dirs                  │
│    /var/run/openshell    (emptyDir) — supervisor runtime (SSH socket)    │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 3. Layer System Interaction

The overlay layer system is **unchanged** from the K8s backend
(§5.3, §5.11 of `sandbox-backends.md`). The same layer chain, the same
PVCs, the same content-addressed storage model, the same capture/eviction
tar pipelines. What changes is:

| Aspect | K8s Backend | OpenShell Backend |
|--------|-------------|-------------------|
| Who composes the overlay | Pod entrypoint (PID 1) | `astonish-boot` binary (ENTRYPOINT, pre-supervisor) |
| What happens after overlay | `chroot /sandbox/rootfs && exec sleep infinity` | `pivot_root` — overlay becomes `/` → `exec openshell-sandbox` |
| How exec enters the overlay | `kubectl exec` → SPDY → wrapper does chroot | Gateway relay → supervisor SSH → child spawned inside overlay (already root) |
| Where `/` points for agent code | Inside chroot at `/sandbox/rootfs` | Directly at `/` (pivot_root made overlay the root) |
| Paths agent sees | `/tmp/foo`, `/home/user/...` (chroot-relative) | `/tmp/foo`, `/home/user/...` (same — overlay IS root) |

### Why `pivot_root` Instead of `chroot`

The K8s backend uses `chroot /sandbox/rootfs` to make the overlay visible
as root. This requires every exec'd process to be wrapped in a chroot
helper (`astonish-shell`, `astonish` wrapper). With OpenShell:

- The supervisor is PID 1 and needs to be inside the overlay (it spawns
  children with `current_dir(workdir)` relative to `/`).
- OpenShell's Landlock policy references paths from `/` (e.g.,
  `read_write: ["/"]`). If the overlay were at `/sandbox/rootfs`, every
  Landlock path would need that prefix.
- `pivot_root` makes the overlay the actual root filesystem. No wrapper
  needed. No path translation. The supervisor and all children see a
  normal Linux root.

### `pivot_root` Mechanics

```
Before pivot_root:
  / = container image filesystem
  /overlay/merged = fuse-overlayfs mount (the composed overlay)
  /overlay/upper = session writable layer
  /overlay/work = overlayfs workdir
  /mnt/astonish-layers/ = PVC with template layers
  /mnt/astonish-uppers/ = PVC with eviction state

After pivot_root(/overlay/merged):
  / = the overlay (full Linux rootfs from layers)
  /.pivot_old = old container image root (unmounted immediately)
  /dev, /proc, /sys = bind-mounted from host (set up pre-pivot)
  /var/astonish/upper = bind-mount of raw overlay upper (for eviction)
  /mnt/uppers = bind-mount of uppers PVC (for eviction writes)
  /mnt/layers = bind-mount of layers PVC (for template captures)
  /usr/local/bin/astonish = copied into upper pre-pivot
  /usr/local/bin/openshell-sandbox = copied into upper pre-pivot
```

After unmounting `/.pivot_old`, the container image's original filesystem
is gone. The supervisor runs in the overlay. Agent processes run in the
overlay. There is no path indirection.

### Binary Injection

Infrastructure binaries (`astonish`, `openshell-sandbox`) are **not** part
of the layer system. They live in the container image at `/opt/astonish/bin/`
and `/opt/openshell/bin/`. The `astonish-boot` binary copies them into the
overlay's upper directory before `pivot_root`:

```
cp /opt/astonish/bin/astonish   /overlay/upper/usr/local/bin/astonish
cp /opt/openshell/bin/openshell-sandbox /overlay/upper/usr/local/bin/openshell-sandbox
```

This ensures:
- Binary version is tied to the **container image** (not to layer content)
- Updating binaries = building a new image (standard CI/CD)
- Template layers remain pure user workspace state
- No layer rebuild needed for binary updates
- Template captures can exclude `/usr/local/bin/astonish*` (or not —
  they're overwritten on every boot)

### The `sandbox` User

OpenShell's supervisor drops privileges to a `sandbox` user before
spawning child processes. After `pivot_root`, `/etc/passwd` comes from the
overlay (ultimately from `@base`). The `sandbox-base` Dockerfile must
include:

```dockerfile
RUN useradd -m -d /sandbox -s /bin/bash sandbox
```

This user is seeded into `@base` via the standard seed Job mechanism.

---

## 4. Container Image

### `astonish-openshell-sandbox` Dockerfile

```dockerfile
FROM astonish-sandbox-base:latest

# OpenShell supervisor binary (from NVIDIA OpenShell release)
COPY --from=ghcr.io/nvidia/openshell-community/openshell-sandbox:latest \
     /usr/local/bin/openshell-sandbox /opt/openshell/bin/openshell-sandbox

# Astonish binary (built from this repo's release)
COPY astonish /opt/astonish/bin/astonish

# Boot binary (overlay composition + pivot_root + exec supervisor)
COPY astonish-boot /opt/astonish/bin/astonish-boot

# Sandbox user for OpenShell privilege drop
RUN useradd -m -d /sandbox -s /bin/bash sandbox && \
    mkdir -p /overlay /var/run/openshell && \
    chmod 755 /opt/astonish/bin/astonish-boot

ENTRYPOINT ["/opt/astonish/bin/astonish-boot"]
```

The image extends the existing `sandbox-base` (Debian Bookworm + tar +
zstd + fuse-overlayfs + mount utilities). It adds:
- The OpenShell supervisor binary
- The Astonish agent binary
- The `astonish-boot` binary (new)
- The `sandbox` user

The seed Job (§5.6 of `sandbox-backends.md`) tars this image's filesystem
into `@base/rootfs` on the layers PVC — excluding `/opt/astonish`,
`/opt/openshell`, `/overlay`, `/var/run/openshell`, and virtual
filesystems. These paths are infrastructure, not user workspace.

---

## 5. The `astonish-boot` Binary

A Go binary at `cmd/astonish-boot/main.go`. It is the container
ENTRYPOINT and performs two phases:

### Phase A: Overlay Composition (Privileged)

This phase requires `CAP_SYS_ADMIN` (for `pivot_root`) and FUSE device
access (for `fuse-overlayfs`). It runs as root.

```go
func main() {
    // 1. Parse environment
    layerChain := os.Getenv("ASTONISH_LAYER_CHAIN")   // "team-sha:admin-sha:@base"
    sessionID  := os.Getenv("ASTONISH_SESSION_ID")
    layersDir  := envOr("ASTONISH_LAYERS_DIR", "/mnt/astonish-layers")
    uppersDir  := envOr("ASTONISH_UPPERS_DIR", "/mnt/astonish-uppers")
    overlayDir := envOr("ASTONISH_OVERLAY_DIR", "/overlay")

    // 2. Resume evicted upper (if exists)
    resumeTar := filepath.Join(uppersDir, sessionID, "upper.tar.zst")
    if fileExists(resumeTar) {
        execCmd("tar", "--numeric-owner", "--xattrs", "--acls",
            "-I", "zstd", "-xf", resumeTar,
            "-C", filepath.Join(overlayDir, "upper"))
    }

    // 3. Compose overlay
    lower := buildLowerDir(layerChain, layersDir) // reverse order, colon-separated
    mountOverlay(lower, overlayDir)                // fuse-overlayfs or native

    merged := filepath.Join(overlayDir, "merged")

    // 4. Copy infrastructure binaries into overlay upper
    copyFile("/opt/astonish/bin/astonish",
             filepath.Join(overlayDir, "upper/usr/local/bin/astonish"))
    copyFile("/opt/openshell/bin/openshell-sandbox",
             filepath.Join(overlayDir, "upper/usr/local/bin/openshell-sandbox"))

    // 5. Bind-mount kernel filesystems into the overlay
    for _, fs := range []string{"/dev", "/proc", "/sys"} {
        bindMount(fs, filepath.Join(merged, fs))
    }
    bindMount("/etc/resolv.conf", filepath.Join(merged, "etc/resolv.conf"))

    // 6. Bind-mount the raw overlay upper into the overlay (for eviction)
    bindMount(filepath.Join(overlayDir, "upper"),
              filepath.Join(merged, "var/astonish/upper"))

    // 7. Bind-mount PVCs into the overlay (for eviction + template capture)
    bindMount(uppersDir, filepath.Join(merged, "mnt/uppers"))
    bindMount(layersDir, filepath.Join(merged, "mnt/layers"))

    // 8. pivot_root
    pivotRoot(merged)

    // 9. Unmount old root
    syscall.Unmount("/.pivot_old", syscall.MNT_DETACH)
    os.RemoveAll("/.pivot_old")

    // 10. exec openshell-sandbox
    supervisorArgs := buildSupervisorArgs()
    syscall.Exec("/usr/local/bin/openshell-sandbox", supervisorArgs, os.Environ())
}
```

### Phase B: Supervisor Startup

After `exec`, the process image is replaced by `openshell-sandbox`.
The supervisor:

1. Connects outbound to the OpenShell Gateway (relay session)
2. Starts an SSH server on `/var/run/openshell/ssh.sock`
3. Loads policy (from gateway via gRPC, or from env/file)
4. Spawns the entrypoint command (`/bin/sleep infinity`) as a restricted
   child with full Landlock + seccomp + privilege drop
5. Applies its own startup seccomp hardening (blocks mount/pivot_root
   for any subsequent operations)
6. Enters steady state: accepts exec requests via the relay

### Error Handling

- If overlay composition fails (bad layer, missing PVC, FUSE unavailable),
  `astonish-boot` exits non-zero. Pod enters `CrashLoopBackOff`. Logs
  clearly indicate which step failed.
- If the supervisor fails to connect to the gateway, it retries with
  exponential backoff. The pod stays running but reports `NotReady`.

---

## 6. OpenShell Supervisor Security Model

After `pivot_root`, the supervisor applies OpenShell's standard security
to all child processes:

### Landlock Filesystem Policy

```yaml
filesystem:
  read_write:
    - /          # The overlay IS the root — full workspace access
    - /tmp
  read_only: [] # Everything under / is already covered by read_write
```

After `pivot_root`, `/` is the overlay. Granting `/` as `read_write` gives
the agent full access to the workspace filesystem — which is correct,
because that's the user's session. The security value is that:

- The agent cannot access paths OUTSIDE the overlay (they don't exist
  after pivot — old root is unmounted)
- The PVC mount paths (`/mnt/astonish-layers`, `/mnt/astonish-uppers`)
  are NOT accessible at their original container-image paths — they were
  only needed during Phase A. Post-pivot, they appear at `/mnt/layers`
  and `/mnt/uppers` and can be restricted via Landlock policy for
  non-builder sessions.
- The supervisor's own identity material (tokens, TLS certs) is hidden
  via mount-namespace isolation (standard OpenShell mechanism)

### seccomp Filter

Blocks (for child processes only — supervisor is exempt):
- `mount`, `umount2` — no filesystem manipulation
- `pivot_root`, `chroot` — no namespace escape
- `mknod` — no device creation
- `ptrace` — no debugging/injection of other processes
- `kexec_load` — no kernel replacement
- Standard OpenShell restrictive baseline

### Network Namespace

- Child processes run in a dedicated network namespace
- All egress is forced through the supervisor's policy proxy (veth pair,
  10.200.0.1:3128)
- The proxy evaluates OPA rules per destination:
  - Package managers (npm, pypi, golang) — allowed
  - Git (github.com) — allowed
  - LLM inference endpoints — routed per privacy configuration
  - Everything else — denied by default
- Per-org/team allowlists extend the policy

### Privilege Drop

- Children run as `sandbox:sandbox` (UID/GID from `/etc/passwd` in overlay)
- `initgroups` + `setgid` + `setuid` with post-drop verification
- Root cannot be re-acquired (CERT POS37-C check)
- Core dumps disabled, `PR_SET_DUMPABLE=0`

### OCSF Audit

Every child spawn, exit, file-access denial, and network connection is
logged in OCSF format (structured JSON). Logs are:
- Written to `/var/log/openshell*.log` (rolling, 3 files)
- Pushed to the gateway via gRPC (when connected)
- Available for aggregation into SIEM/compliance systems

---

## 7. Exec Model

### Current (K8s Backend)

```
Astonish Backend.Exec(sessionID, ExecSpec)
  → Build SPDY URL: /api/v1/namespaces/.../pods/.../exec
  → client-go remotecommand.NewSPDYExecutor
  → Stream stdin/stdout/stderr
  → Wrapper inside pod does chroot before exec
  → Return ExecResult{Stdout, Stderr, ExitCode}
```

### New (OpenShell Backend)

```
Astonish Backend.Exec(sessionID, ExecSpec)
  → OpenShell Gateway gRPC: ExecSandbox(sandbox_id, command, env, workdir)
    → Gateway routes to supervisor via relay
      → Supervisor: SSH exec_request handler
        → fork()
        → pre_exec:
            setns(netns)          — enter network namespace
            drop_privileges()     — setuid sandbox
            Landlock enforce()    — restrict filesystem
            seccomp enforce()     — restrict syscalls
        → exec(command)
        → relay stdout/stderr back through SSH → relay → gateway
  → Return ExecResult{Stdout, Stderr, ExitCode}
```

### Per-Call Node Protocol (Unchanged)

The node binary protocol is identical to the K8s backend's per-call model.
Each tool invocation:

1. Backend calls `Exec` with command `["/usr/local/bin/astonish", "node"]`
   and stdin containing the JSON-RPC request
2. The supervisor spawns `astonish node` as a restricted child
3. The process reads one request from stdin, executes the tool, writes
   one response to stdout, exits
4. Backend reads the response from stdout

No persistent connection. No long-running agent process. Each tool call
is a fresh, fully-isolated process spawn.

### Interactive Terminal

```
Astonish Backend.ExecInteractive(sessionID, PTYSpec)
  → OpenShell Gateway gRPC: ConnectSandbox(sandbox_id, pty=true)
    → Gateway relay → supervisor SSH
      → shell_request + pty_request
        → Supervisor allocates PTY
        → fork() with same security stack
        → exec /bin/bash -i
      → Bidirectional stream: user keystrokes ↔ shell output
        → Window resize via SSH window_change_request
```

### MCP Transport

MCP servers inside sandboxes use `ExecStreaming` (non-interactive,
long-running, bidirectional stdin/stdout). The supervisor's SSH
`exec_request` handler supports this natively — it spawns the MCP server
command as a restricted child and relays stdin/stdout for the duration.

---

## 8. Session Lifecycle

### Create

```
1. Resolve template layer chain (same DB query as K8s backend)
2. Generate OpenShell policy YAML (from org/team config — see §11)
3. Call OpenShell Gateway: CreateSandbox
     name:       "astn-sess-<sessionID[:8]>"
     image:      config.sandbox_image
     env:        ASTONISH_LAYER_CHAIN, ASTONISH_SESSION_ID,
                 ASTONISH_LAYERS_DIR, ASTONISH_UPPERS_DIR, ...
     policy:     generated YAML
     labels:     astonish.io/{org,team,session-id,type=session,template}
     driver_config: {kubernetes: {pod: {node_selector, tolerations, ...}}}
4. MutatingAdmissionWebhook injects PVC volumes (see §10)
5. Gateway creates Sandbox CRD → K8s driver creates pod
6. astonish-boot composes overlay, pivot_root, starts supervisor
7. Supervisor connects to gateway relay → sandbox state = RUNNING
8. Backend detects RUNNING state → session is ready
9. Register session in PG (same schema as K8s backend)
```

### WaitForSessionReady

Polls/watches sandbox state via the gateway until:
- State transitions to `RUNNING` (supervisor connected to relay)
- Or timeout expires (configurable, default 120s)

No need to poll for a file (`/etc/astonish-overlay-ok`) — the supervisor
connecting to the relay is the authoritative ready signal.

### Stop (Eviction)

The orchestrator owns eviction explicitly (not a pre-stop hook):

```
1. Exec tar pipeline inside the sandbox (via gateway relay):
     tar --numeric-owner --xattrs --acls -I "zstd --adapt -T0" \
         -C /var/astonish/upper -cf /mnt/uppers/<session-id>/upper.tar.zst .

   /var/astonish/upper is the bind-mount of the raw overlay upper
   directory (set up in Phase A step 7, before pivot_root).

   /mnt/uppers is the bind-mount of the uppers PVC (set up in
   Phase A step 8, before pivot_root).

2. Verify tar exit code = 0 (via exec result)

3. Call gateway: DeleteSandbox(sandbox_id)

4. Update session state in PG to "stopped", set upper_persisted_at
```

### Resume (Start)

```
1. Call gateway: CreateSandbox (same flow as fresh create)
   - Env includes ASTONISH_SESSION_ID (same as before)
   - astonish-boot detects upper.tar.zst on uppers PVC, extracts it
   - Overlay is composed with the resumed upper
   - pivot_root, supervisor starts
2. Session is back to RUNNING state
3. User continues where they left off
```

### Destroy

```
1. Call gateway: DeleteSandbox(sandbox_id) — removes the pod
2. Delete persisted upper: create a short-lived helper sandbox to
   exec "rm -rf /mnt/uppers/<session-id>/" on the uppers PVC
   (same pattern as K8s backend GC pods)
3. Delete PG row from sandbox_sessions
4. Delete any K8s Services for exposed ports
5. Emit audit event
```

---

## 9. Template Operations

Template operations use the same exec relay as tool calls. No special
path needed — the operations run commands inside sandboxes.

### BuildTemplate (Admin "Build Base Layer" / CreateTemplate)

```
1. Create an ephemeral sandbox via OpenShell (same as CreateSession)
   - Overlay composed from parent template's layer chain
   - Labels: astonish.io/type=template-builder
   - Layers PVC bind-mounted RW at /mnt/layers inside overlay
2. Exec build commands inside (apt install, npm install, etc.)
3. Run capture: tar the overlay upper, compute SHA-256
     exec: tar --numeric-owner --xattrs --acls --sort=name \
         -I "zstd --adapt -T0" \
         -C /var/astonish/upper -cf - . \
       | tee >(sha256sum > /tmp/sha) \
       | tar -I zstd -xf - -C /mnt/layers/__staging-<id>/rootfs
     exec: cat /tmp/sha (read the content hash)
     exec: mv /mnt/layers/__staging-<id> /mnt/layers/<sha256>
4. Register layer in PG (same transaction pattern as K8s backend)
5. Destroy ephemeral sandbox
```

### SaveSessionAsTemplate

```
1. Session pod is already running
2. Exec capture script in the running sandbox (same tar pipeline)
3. Stage layer on layers PVC (via /mnt/layers bind-mount)
4. Register in PG (same transaction pattern)
5. Session continues running (not destroyed)
```

### Layer Chain Resolution

Unchanged. Walk `parent_template_id` in PG from the requested template
up to `@base`. Collect each `top_layer_id`. Reverse to get bottom-up
order. Pass as `ASTONISH_LAYER_CHAIN=<comma-separated>`.

---

## 10. MutatingAdmissionWebhook

OpenShell's K8s driver creates pods with a standard spec. Astonish needs
additional volumes (layers PVC, uppers PVC, overlay emptyDir) and
potentially FUSE device resources. Since OpenShell's `driver-config-json`
does not support custom volume mounts, a MutatingAdmissionWebhook injects
them.

### Webhook Deployment

```
Namespace: astonish-system
Deployment: astonish-sandbox-webhook (2 replicas for HA)
MutatingWebhookConfiguration:
  name: astonish-sandbox-volumes.astonish.io
  rules:
    - apiGroups: [""]
      resources: ["pods"]
      operations: ["CREATE"]
  namespaceSelector:
    matchLabels:
      astonish.io/sandbox-namespace: "true"
  objectSelector:
    matchLabels:
      astonish.io/type: exists
  failurePolicy: Fail
```

### Mutation Logic

```go
func mutatePod(pod *corev1.Pod) {
    // Only mutate Astonish sandbox pods
    if _, ok := pod.Labels["astonish.io/type"]; !ok {
        return
    }

    // Inject volumes
    pod.Spec.Volumes = append(pod.Spec.Volumes,
        pvcVolume("astonish-layers", config.LayersPVCName),
        pvcVolume("astonish-uppers", config.UppersPVCName),
        emptyDirVolume("astonish-overlay"),
        emptyDirVolume("openshell-runtime"),
    )

    // Inject mounts into the agent container (first container)
    c := &pod.Spec.Containers[0]
    c.VolumeMounts = append(c.VolumeMounts,
        mount("astonish-layers", "/mnt/astonish-layers"),
        mount("astonish-uppers", "/mnt/astonish-uppers"),
        mount("astonish-overlay", "/overlay"),
        mount("openshell-runtime", "/var/run/openshell"),
    )

    // FUSE device resource (if configured)
    if config.FuseDeviceResource != "" {
        c.Resources.Limits[config.FuseDeviceResource] = "1"
        c.Resources.Requests[config.FuseDeviceResource] = "1"
    }

    // CAP_SYS_ADMIN for astonish-boot's pivot_root
    // (Supervisor's seccomp blocks it after startup hardening)
    ensureCapability(c, "SYS_ADMIN")
}
```

### Layers PVC Mount Mode

The layers PVC is always mounted read-write at the container level (the
webhook does not differentiate). Access control is enforced at the
OpenShell policy level:

- **Session sandboxes:** Landlock policy restricts `/mnt/layers` to
  `read_only` for child processes. The supervisor (root) retains write
  access but does not expose it to children.
- **Template editor/builder sandboxes:** Landlock policy grants
  `/mnt/layers` as `read_write` for child processes (needed to write
  captured layers).

This simplifies the webhook (no label inspection for mount mode) while
maintaining the security boundary via OpenShell's per-sandbox policy.

---

## 11. Policy Generation

The OpenShell backend generates policy YAML for each sandbox based on
org/team configuration:

```go
func (b *Backend) generatePolicy(orgID, teamID, sessionType string) string {
    policy := Policy{
        Version: 1,
        Filesystem: FilesystemPolicy{
            IncludeWorkdir: true,
            ReadWrite:      b.readWritePaths(sessionType),
            ReadOnly:       b.readOnlyPaths(sessionType),
        },
        Process: ProcessPolicy{
            RunAsUser:  "sandbox",
            RunAsGroup: "sandbox",
        },
        Network: b.buildNetworkPolicy(orgID, teamID, sessionType),
    }
    return marshalYAML(policy)
}

func (b *Backend) readWritePaths(sessionType string) []string {
    paths := []string{"/", "/tmp"}
    if sessionType == "template-builder" || sessionType == "team-template-editor" {
        paths = append(paths, "/mnt/layers")  // RW for template capture
    }
    return paths
}

func (b *Backend) readOnlyPaths(sessionType string) []string {
    if sessionType != "template-builder" && sessionType != "team-template-editor" {
        return []string{"/mnt/layers"}  // RO for regular sessions
    }
    return nil
}
```

### Network Policy Rules

Default allowlist (all sandboxes):
- Package managers: `registry.npmjs.org`, `proxy.golang.org`, `pypi.org`,
  `files.pythonhosted.org`
- Git: `github.com`, `api.github.com`
- DNS: cluster DNS

Per-org/team extensions (from Astonish platform configuration):
- Custom allowed endpoints (e.g., internal APIs, artifact registries)
- LLM provider endpoints (when privacy router is disabled)

When privacy router is enabled:
- All LLM egress routed through local inference endpoint
- Direct access to external LLM APIs blocked

---

## 12. Network Enforcement

The OpenShell backend uses **two layers** of network enforcement:

### Layer 1: Kubernetes NetworkPolicy (Pod-Level)

Same as current K8s backend (§5.7 of `sandbox-backends.md`):
- Per-org NetworkPolicy in the sandbox namespace
- Ingress: from control-plane namespace + same-org pods
- Egress: DNS + org-configured allowlist

### Layer 2: OpenShell Policy Proxy (Process-Level)

Inside each sandbox, the supervisor's policy proxy provides:
- Per-destination enforcement (not just per-pod)
- L7 inspection (HTTP CONNECT interception)
- Per-binary rules (e.g., only `npm` can reach `registry.npmjs.org`)
- OPA-based policy evaluation
- OCSF audit of every connection attempt

The combination gives defense-in-depth: even if the in-pod proxy is
somehow bypassed, K8s NetworkPolicy blocks unauthorized egress at the
network layer.

---

## 13. GC and Layer Reclamation

Same model as K8s backend (§5.6, §5.12 of `sandbox-backends.md`), but GC
operations run inside OpenShell-managed sandboxes instead of direct pods:

```
1. Create short-lived sandbox via OpenShell:
     name: "astn-gc-<timestamp>"
     labels: astonish.io/type=gc
     layers PVC: mounted RW (via Landlock policy for GC type)
2. Exec: list orphan layer directories on /mnt/layers
3. Compare with PG layer registry
4. Exec: rm -rf orphan directories
5. Delete sandbox
```

The GC sandbox has a minimal policy (filesystem access to layers PVC only,
no outbound network). Still benefits from OpenShell audit logging.

---

## 14. Port Exposure and Tunneling

### Port Exposure (K8s Services)

The OpenShell backend creates K8s Services directly (same as K8s backend
§5.7). This requires the Astonish control plane to retain a K8s client
with Service CRUD permissions in the sandbox namespace.

```go
func (b *Backend) ExposePort(ctx, sessionID string, port int, proto string) {
    // Get sandbox details from gateway to find pod name/labels
    sandbox := b.gateway.GetSandbox(ctx, sandboxName)
    // Create K8s Service targeting the pod via label selector
    b.k8sClient.CoreV1().Services(namespace).Create(ctx, buildService(...))
}
```

### Tunneling (Direct TCP over Relay)

OpenShell's supervisor supports `direct-tcpip` SSH channels. This can
replace the current `socat STDIO TCP:127.0.0.1:<port>` approach:

```
Astonish backend opens a direct-tcpip channel:
  → Gateway relay → supervisor SSH → direct-tcpip to localhost:port
  → Bidirectional TCP stream
```

This is more efficient than spawning a `socat` process per connection
and benefits from the supervisor's network namespace isolation (the
connection happens inside the sandbox's netns, reaching the correct
loopback).

### Browser CDP Tunnel

The browser CDP (Chrome DevTools Protocol) endpoint is tunneled the same
way — direct-tcpip to `localhost:9222` (or whatever port Chromium
binds). No change to the browser stack itself.

---

## 15. Fleet Containers

Fleet containers follow the same lifecycle as sessions:

```
Name: "astn-fleet-<plan>-<instance>"
Labels: astonish.io/type=fleet
```

Created via the same OpenShell gateway API. `EnsureFleetContainer` is
idempotent: checks if sandbox exists, creates if not.

Fleet containers have the same security enforcement as session sandboxes.
The policy may differ (fleet containers often need different network
access), generated from the fleet plan configuration.

---

## 16. Configuration

### Astonish Application Config

```yaml
sandbox:
  enabled: true
  backend: openshell   # New default; "k8s" for legacy

  openshell:
    # OpenShell Gateway connection
    gateway_endpoint: "openshell-gateway.openshell-system.svc:443"
    gateway_tls: true
    gateway_ca_path: ""  # Empty = use system CAs

    # Sandbox image
    sandbox_image: "registry.example.com/astonish-openshell-sandbox:v1.0.0"

    # Resources
    default_cpu: "2"
    default_memory: "2Gi"
    cpu_limit: "4"
    memory_limit: "4Gi"

    # Overlay / Layers
    layers_pvc_name: "astonish-layers"
    uppers_pvc_name: "astonish-uppers"
    overlay_mode: "fuse"              # "fuse" or "native"
    fuse_device_resource: "smarter-devices/fuse"  # Empty if using native overlay

    # K8s driver config (forwarded via driver-config-json)
    runtime_class_name: ""
    node_selector: {}
    tolerations: []
    priority_class_name: ""

    # Session behavior
    idle_timeout: "30m"
    max_session_duration: "24h"
    eviction_enabled: true

    # Network policy defaults
    privacy_router_enabled: false
    local_inference_url: ""
    default_egress_allowlist:
      - "registry.npmjs.org:443"
      - "proxy.golang.org:443"
      - "pypi.org:443"
      - "github.com:443"
      - "api.github.com:443"

    # Webhook
    webhook_namespace: "astonish-system"

  # K8s backend config (unchanged, used when backend=k8s)
  kubernetes:
    namespace: "astonish-sandbox"
    # ... existing config ...
```

### Backend Registration

```go
// pkg/sandbox/openshell/init.go
package openshell

import "github.com/schardosin/astonish/pkg/sandbox"

func init() {
    sandbox.RegisterBackendFactory("openshell", NewBackend)
}
```

Add `BackendKindOpenShell = "openshell"` to
`pkg/sandbox/backend.go::BackendKind` constants.

Add `OpenShell SandboxOpenShellConfig` to `pkg/config/app_config.go`.

---

## 17. Deployment Topology

```
┌─────────────────────────────────────────────────────────────────┐
│ Namespace: openshell-system                                      │
│   OpenShell Gateway (Deployment, 2+ replicas)                    │
│   OpenShell K8s Driver (in-process with gateway)                 │
│   Sandbox CRD: agents.x-k8s.io/v1alpha1                         │
│   PostgreSQL / SQLite (gateway state)                            │
├──────────────────────────────────────────────────────────────────┤
│ Namespace: astonish-system                                       │
│   Astonish API/Worker (Deployment, 2+ replicas)                  │
│   Astonish Webhook (Deployment, 2 replicas for HA)               │
│   PostgreSQL (application state)                                 │
├──────────────────────────────────────────────────────────────────┤
│ Namespace: astonish-sandboxes                                    │
│   Sandbox Pods (created by OpenShell K8s driver)                 │
│   PVC: astonish-layers (RWX, CephFS/NFS/EFS)                    │
│   PVC: astonish-uppers (RWX, CephFS/NFS/EFS)                    │
│   Services (port exposure)                                       │
│   NetworkPolicies (per-org)                                      │
└──────────────────────────────────────────────────────────────────┘
```

### Helm Chart Changes

- New subchart or values section for OpenShell Gateway deployment
- New Deployment for the MutatingAdmissionWebhook
- New `MutatingWebhookConfiguration` resource
- Updated sandbox image reference
- RBAC: Astonish ServiceAccount needs `get/list/watch` on Sandbox CRDs
  in addition to existing pod/service permissions

---

## 18. Migration Path from K8s Backend

### Gradual Migration

Both backends coexist. Operators migrate by:

1. **Deploy OpenShell Gateway** alongside existing Astonish cluster
2. **Deploy MutatingAdmissionWebhook** (targets only pods with
   `astonish.io/type` label — no effect on existing K8s backend pods
   which don't have that label via OpenShell)
3. **Build and push** `astonish-openshell-sandbox` image
4. **Update seed Job** to use new image (re-seeds `@base` with
   `sandbox` user and updated exclusions)
5. **Switch config**: `sandbox.backend: openshell`
6. **Validate** all operations (session create, exec, template save,
   eviction, resume, fleet)
7. **Remove K8s backend config** when satisfied

### Rollback

Switch `sandbox.backend: k8s`. Existing K8s backend code is unchanged.
Running OpenShell-managed sandboxes can be deleted via the gateway CLI.

### Breaking Changes

None for the interface callers. The `SandboxBackend` interface is
identical. Backend selection is a config change. PG schema is unchanged
(sessions, templates, layers tables are backend-agnostic).

---

## 19. Implementation Phases

| Phase | Deliverable | Dependencies |
|-------|-------------|--------------|
| **0** | Spike: validate `pivot_root` over `fuse-overlayfs` in target cluster | None |
| **1** | `cmd/astonish-boot` binary (overlay + pivot_root + exec) | None |
| **2** | `docker/sandbox-openshell/Dockerfile` + image build pipeline | Phase 1 |
| **3** | Deploy OpenShell Gateway (Helm chart integration) | None |
| **4** | `deploy/webhook/` — MutatingAdmissionWebhook | None |
| **5** | `pkg/sandbox/openshell/` — config, client, CreateSession, DestroySession | Phases 2, 3, 4 |
| **6** | Exec methods (Exec, ExecInteractive, ExecStreaming) via relay | Phase 5 |
| **7** | WaitForSessionReady + node binary protocol validation | Phase 6 |
| **8** | Template operations (Build, Save, Capture) | Phase 7 |
| **9** | Session eviction (Stop) and resume (Start) | Phase 7 |
| **10** | GC reconciler (adapted for OpenShell sandboxes) | Phase 7 |
| **11** | Policy generation from org/team config | Phase 5 |
| **12** | Port exposure + browser CDP tunnel via direct-tcpip | Phase 7 |
| **13** | Fleet containers | Phase 7 |
| **14** | Integration tests (backend contract tests) | All |

### Spike: Validate `pivot_root` Feasibility (Phase 0)

Before Phase 1, deploy a test pod that:
1. Mounts the layers PVC
2. Runs `fuse-overlayfs`
3. Copies a binary into the overlay upper
4. Calls `pivot_root`
5. Successfully execs the binary from inside the new root

This validates that `pivot_root` over `fuse-overlayfs` works in the
target cluster. Some kernel versions or container runtimes may restrict
`pivot_root` inside containers. Known requirements:
- `CAP_SYS_ADMIN` in the container's security context
- The new root must be a mount point (fuse-overlayfs creates one)
- The process must be in its own mount namespace (containers are by default)

---

## 20. Risk Assessment

| Risk | Severity | Mitigation |
|------|----------|-----------|
| `pivot_root` over `fuse-overlayfs` blocked by kernel/runtime | High | Spike test (Phase 0). Fallback: use kernel overlay with `hostUsers: false` (K8s 1.33+). |
| OpenShell Gateway API changes between versions | Medium | Pin to specific OpenShell release. Integration tests in CI. |
| Exec latency increase (extra hop: gateway → relay → supervisor) | Medium | Benchmark. Expected ~20-50ms additional per exec. For hundreds of tool calls, consider connection pooling / batching. |
| `CAP_SYS_ADMIN` in sandbox pods widens attack surface | Medium | Only needed during `astonish-boot` (pre-supervisor). Supervisor's seccomp blocks mount/pivot_root after startup hardening. Same risk profile as current FUSE-device-plugin path. |
| Supervisor crash = pod restart = session state loss (emptyDir upper) | Medium | Periodic upper checkpoint to uppers PVC as background task. Alternatively accept the risk (current K8s backend has same limitation). |
| MutatingAdmissionWebhook unavailability blocks pod creation | Medium | Deploy with 2+ replicas. Use `failurePolicy: Fail` to prevent pods without volumes (which would fail at boot anyway). |
| Template capture payload: large tars through exec relay | Low | Capture writes directly to PVC inside the sandbox (in-pod tar → PVC). Only the SHA result passes through exec stdout. |
| Node binary version skew (stale image in registry) | Low | Binary is copied from container image on every boot. Image tag includes version. CI enforces tag = code version. |

---

## 21. Differences from K8s Backend (Summary)

| Aspect | K8s Backend | OpenShell Backend |
|--------|-------------|-------------------|
| Pod creation | Direct K8s API (`Pods.Create`) | OpenShell Gateway → Sandbox CRD → K8s driver |
| PID 1 | `astonish-sandbox-entrypoint` → `sleep infinity` | `astonish-boot` → `openshell-sandbox` |
| Root filesystem | `chroot /sandbox/rootfs` | `pivot_root` — overlay IS `/` |
| Exec transport | SPDY (`kubectl exec` equivalent) | Gateway relay → supervisor SSH |
| Security model | K8s NetworkPolicy only | Landlock + seccomp + network namespace + OCSF audit |
| Credential handling | Env vars visible to all processes | Supervisor strips secrets before child spawn |
| Network enforcement | Pod-level NetworkPolicy | Per-process, L7, OPA-based proxy |
| Audit trail | K8s audit log (pod/exec events only) | OCSF structured logs (every process, denial, connection) |
| Binary injection | Bind-mount from host layer (`astonish-host`) | Copied into overlay upper by `astonish-boot` |
| Eviction trigger | Orchestrator exec (tar via SPDY) | Orchestrator exec (tar via gateway relay) |
| PVC injection | Direct in PodSpec | MutatingAdmissionWebhook |
| Ready signal | Poll for `/etc/astonish-overlay-ok` file | Supervisor connects to gateway relay |

---

## 22. Files to Create/Modify

### New Files

| Path | Purpose |
|------|---------|
| `cmd/astonish-boot/main.go` | ENTRYPOINT binary: overlay composition + pivot_root + exec supervisor |
| `pkg/sandbox/openshell/backend.go` | Main Backend interface implementation |
| `pkg/sandbox/openshell/config.go` | Configuration struct + YAML loading |
| `pkg/sandbox/openshell/client.go` | OpenShell Gateway gRPC client wrapper |
| `pkg/sandbox/openshell/exec.go` | Exec/ExecInteractive/ExecStreaming via relay |
| `pkg/sandbox/openshell/session.go` | Create/Destroy/Start/Stop/State |
| `pkg/sandbox/openshell/template.go` | Template operations (build, save, capture) |
| `pkg/sandbox/openshell/fleet.go` | Fleet container management |
| `pkg/sandbox/openshell/gc.go` | GC reconciler (OpenShell-adapted) |
| `pkg/sandbox/openshell/policy.go` | Policy YAML generation from org/team config |
| `pkg/sandbox/openshell/network.go` | Network policy → OpenShell policy mapping |
| `pkg/sandbox/openshell/init.go` | Factory registration (`RegisterBackendFactory`) |
| `deploy/webhook/main.go` | MutatingAdmissionWebhook server |
| `deploy/webhook/mutate.go` | Pod mutation logic (PVC + FUSE + SYS_ADMIN injection) |
| `deploy/webhook/Dockerfile` | Webhook container image |
| `docker/sandbox-openshell/Dockerfile` | OpenShell-enabled sandbox image |
| `docs/architecture/openshell-sandbox-backend.md` | This document |

### Modified Files

| Path | Change |
|------|--------|
| `pkg/sandbox/backend.go` | Add `BackendKindOpenShell = "openshell"` constant |
| `pkg/config/app_config.go` | Add `SandboxOpenShellConfig` struct, wire into `SandboxConfig` |
| `docker/sandbox-base/Dockerfile` | Add `sandbox` user (`RUN useradd -m -d /sandbox -s /bin/bash sandbox`) |
| `deploy/helm/astonish/templates/sandbox/seed-job.yaml` | Update tar exclusions for `/opt/astonish`, `/opt/openshell`, `/overlay` |
| `deploy/helm/astonish/values.yaml` | Add `sandbox.openshell.*` values section |
| `deploy/helm/astonish/templates/` | Add webhook Deployment + MutatingWebhookConfiguration |
| `go.mod` | Add OpenShell gRPC/proto dependencies |
| `Makefile` | Add `build-boot` target for `cmd/astonish-boot` |
