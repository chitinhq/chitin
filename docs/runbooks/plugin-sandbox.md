# Plugin sandbox (bubblewrap)

Status: opt-in MVP. Off by default.

## What it does

Wraps a router plugin's subprocess in a `bwrap` namespace:

- new PID/UTS/IPC/cgroup/net namespaces
- read-only filesystem (entire `/usr`, `/etc`, `/lib*`, `/bin`, `/sbin`)
- writable `tmpfs` at `/tmp`
- network dropped (override with `allow_network: true`)
- `$HOME` not visible by default; opt in with `allow_write: true` to bind it read-write
- requires bubblewrap **0.4 or newer** (uses `--ro-bind-try` / `--bind-try`)

## When to enable

A heuristic that just reads stdin + writes JSON is already low-blast-radius — sandboxing is overkill. Turn it on when:

- the plugin links to third-party native code (numpy, regex JIT)
- the plugin reads files outside its module path
- the plugin is community-contributed (someone else's code)
- the operator wants belt + suspenders against a future plugin author who pulls in a malicious dep

## Wiring

```yaml
router:
  plugins:
    - name: shell-anomaly-scorer
      type: heuristic
      runtime: python3
      module: /home/op/.chitin/plugins/shell_anomaly.py
      sandbox:
        mode: bwrap
        # default: AllowNetwork=false, AllowWrite=false
        extra_ro_binds:
          - /home/op/.chitin/plugins/lib   # shared helper module
```

## Cost

- ~5-10 ms extra per spawn vs unsandboxed (negligible alongside python3 cold-start of 50-100 ms)
- bwrap binary must be on PATH; install with `apt install bubblewrap`

## Limitations

### AppArmor on Ubuntu 24+

Recent Ubuntu kernels ship with `kernel.apparmor_restrict_unprivileged_userns=1`. Under this default, an unprivileged `bwrap` invocation that tries to set up its loopback inside the new netns gets `Operation not permitted`, and uid-map setup fails with `Permission denied`.

Symptom (in plugin stderr):

```
bwrap: setting up uid map: Permission denied
bwrap: loopback: Failed RTM_NEWADDR: Operation not permitted
```

Fixes (pick one):

1. **Install an AppArmor profile** for `/usr/bin/bwrap` that grants `userns,` — the standard distro way. Several distros ship one; check `/etc/apparmor.d/`.
2. **Disable the restriction system-wide** (less safe; opens up other userns users): `sysctl -w kernel.apparmor_restrict_unprivileged_userns=0`
3. **Skip sandboxing on this host** — leave `sandbox.mode` unset for affected plugins. The kernel falls open with a one-line warning.

### Non-Linux

bwrap is Linux-only. On macOS/Windows the wrapper logs a warning and runs the plugin unsandboxed.

## What this does NOT cover

- **CPU/memory caps**: bwrap doesn't apply cgroup limits. Use `systemd-run --scope -p MemoryMax=...` if you need that. (Future work — `sandbox.mode: systemd-scope`.)
- **Outbound DNS**: dropped along with the netns, by default. Plugins that need DNS must set `allow_network: true`.
- **Seccomp filters**: not applied. bwrap supports `--seccomp FD` but our MVP doesn't wire it. (Future work.)

## Verifying

To prove your plugin really lost network access, drop in a one-line check:

```python
import socket
try:
    socket.create_connection(("1.1.1.1", 53), timeout=0.5).close()
    network_ok = True
except OSError:
    network_ok = False
```

Emit `network_ok` in the plugin's `axis` field; flip `sandbox.allow_network` and watch it change.
