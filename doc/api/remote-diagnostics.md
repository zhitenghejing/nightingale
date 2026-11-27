# SSH diagnostics for the built-in Agent

`run_target_diagnostic` lets the native conversational Agent collect bounded
device evidence over password-authenticated SSH. The model chooses the query
for Linux or Huawei VRP. The server validates the query before opening SSH.
The center must be able to reach the target over SSH, directly or through one
SSH jump host. The target needs no Ibex or categraf execution agent.

This capability is independent of `feat/ai-alert-correlation`. It extends the
existing `ops-troubleshooting` skill and does not enable unattended diagnosis
on every incoming alert, merge other branches, or change alert notifications.
The conversation initiator's identity and current permissions are checked for
each call. These Center-only records do not add edge/alert read contracts.

## Deployment

The feature is off by default. Configure the center:

```toml
[Center.RemoteDiagnostics]
Enable = true
KeyFile = "/etc/n9e/private/ops-ssh-keys.json"
TimeoutSeconds = 30
MaxOutputBytes = 32768
MaxConcurrent = 4
```

Provision a private regular key file (mode `0600`, no symlink) outside the
worktree and database. Generate a random 32-byte key and encode it as base64:

```json
{"active":"v1","keys":{"v1":"BASE64_OF_32_RANDOM_BYTES"}}
```

Back up this file separately with the database; losing it makes saved credentials
unrecoverable. Every Center accessing these records needs the same keyring.
To rotate, retain old key IDs, add a new random key, change `active`, and restart
the Centers; editing a credential reseals it with the active key. Do not remove
an old key until every referencing record has been resealed and verified.

Under **集成中心 → 模板中心 → 设备添加表单**, a template can declare
SSH or RDP as the default console protocol. When a device is added or edited,
**设备控制台（可选）** accepts the address, port and a manually entered
account/password for that device. Windows templates suggest RDP by default.
One-hop SSH can have its own device-specific jump-host account and fingerprint.
Target and console changes made in the managed device form commit atomically.

`ops_credential` encrypts username and password with AES-256-GCM, random nonces
and credential ID/group as authenticated data. `ops_ssh_connection` stores the
protocol, physical Target ID, endpoint metadata and credential references.
`ops_access_audit` stores mutation metadata. GET exposes neither plaintext,
ciphertext nor key IDs. Leaving both credential fields blank preserves the
same device and endpoint binding. Changing protocol, address, port, or
business group requires fresh credentials. New credentials are scoped to the
device and removed when replaced or when its binding is removed.
Existing SSH bindings remain usable and can be rotated from the device form.
A moved or recreated Target cannot inherit an old binding.

SSH credentials can be configured on HTTP and HTTPS pages. HTTPS encrypts the
browser-to-Center connection, including credentials entered in the form. Center
always uses encrypted SSH to connect to the device. Request-body logging is
prohibited for this API and collection device writes, even when enabled in log
settings.
No browser storage, chat arguments, collection snapshots or publications contain
SSH account secrets.

`ConnectionsFile` remains a deprecated deployment-only compatibility mode for
older installations without `KeyFile`. A configured database store never falls
back to the legacy file on missing/revoked credentials. New deployments should
use the encrypted database store; there is no automatic import of old files.

`ident` must name an existing native Target. `group_id` must remain one of that
Target's current business groups. Use one unique connection per ident. The
operator binds the address; alert labels and model arguments cannot replace it.
Both endpoints use literal IPv4/IPv6 addresses, fixed ports, passwords and
SHA-256 host-key fingerprints. Unspecified, multicast and link-local addresses
are rejected. Loopback is allowed only when explicitly provisioned by the
operator (for locally forwarded SSH); the model cannot select it dynamically.

The device form discovers the SSH host key when saving; it does not require a
manually entered fingerprint. First use opens an explicit trust confirmation
showing the address, key type and SHA256 fingerprint. An unchanged saved key
needs no repeated confirmation. A changed key shows the old and new values and
requires explicit acceptance before replacing the pin. Automatic discovery alone
is not proof of device identity; compare with a trusted source when needed.
Cancelling leaves the connection and inline credentials unsaved. Diagnostic execution
still fails closed on any fingerprint mismatch.

For a jump connection, approve the jump key first. Only then may the backend
use its credential and SSH forwarding to discover the target key. The selected
endpoint's discovery handshake stops before authentication and executes no
command. Discovery is bounded to 20 seconds and four concurrent requests per
Center; it requires connection-configuration permission and group write access.
A credential supplied inline for jump authentication also requires credential
management permission. Closing the editor cancels pending confirmation.

For one-hop access, enable the optional jump-host fields in the device SSH form.

The jump must support standard SSH `direct-tcpip` forwarding to the configured
target. This is not interactive commercial-bastion terminal automation: web
login, OTP/MFA prompts, keyboard-interactive menus, multiple jumps, and
ProxyCommand scripts are not supported. The target host key is independently
verified inside the tunnel. Configure the jump account's forwarding policy to
allow only the intended targets.

Linux targets need `/bin/bash`, `/usr/bin/env`, GNU `/usr/bin/timeout`, coreutils
and the utilities required for the chosen check (`ps`, `sed`, `df`, `free`, or systemd
tools). Use a dedicated non-root diagnostic account with access only to approved
logs and status queries; no sudo is used. Fixed read commands are enforced by
this application, while the remote OS account is the final permission layer.
Reading a path may follow a remote symlink, so approved log paths must be
operator-controlled and the account must not have access to unrelated secrets.

## Tool contract and permissions

The only model inputs are `ident`, `check`, `service`, `log_path`, `since`,
`until`, `lines`, `query`, `severity`, and `interface`. Additional fields (including command, IP, password,
group ID or confirmation flags) are rejected.

| Check | Evidence | Inputs |
| --- | --- | --- |
| `linux_cli` | Linux read-only query output | `query` |
| `huawei_cli` | Huawei VRP read-only CLI output | `query`, optional `interface`, log-only `severity` and `lines` |
| `overview` | UTC clock, kernel, uptime, memory | ident |
| `processes` | CPU-sorted process snapshot without command arguments | lines |
| `disk` | Filesystem and inode usage | ident |
| `service` | Selected systemd state and main-process exit properties | service unit |
| `journal` | Bounded UTC journal window, optionally for one unit | since/until Unix seconds, service, lines |
| `log_tail` | Last lines of an approved file at inspection time | exact approved log_path, lines |

The model-facing tool uses `linux_cli` and `huawei_cli`. Earlier Linux check
names remain valid for existing callers. For `linux_cli`, the model can use
`lscpu | grep "Model name"; free -m` to collect CPU and memory in one SSH
session. The syntax supports literal arguments, up to four pipe stages, and
up to three queries separated by semicolons. The server validates executables
and arguments, quotes each argument, and stops the batch at the first failed
pipeline. Queries start with a system read, and filters only consume the
preceding command's output. `tail` requires an approved log path and a line
count, `systemctl show` restricts property names, and `journalctl` limits
line count and explicit time windows. Shell expansions, redirects, script
execution and network commands are outside the query language.

Huawei checks share one SSH terminal implementation. Each `huawei_cli` call
accepts a single approved `display` query. The policy includes interface
configuration and status, VLAN, VRP version, hardware, CPU, memory,
environment readings and the log buffer. Interface queries accept the full
physical interface name or a port number string. A bare number is resolved
from `display interface brief`. If several slots match, the result contains
`candidate_interfaces` and no interface query is sent. The former
`huawei_interface_config` check remains a compatibility alias.

Operators can extend `Center.RemoteDiagnostics.HuaweiReadCommands` with reviewed
literal `display` commands or a final `{interface}` or `{vlan}` placeholder. For
example, `["display transceiver interface {interface}"]` permits a physical-port
transceiver query once tested on the intended model. Command separators, CLI
pipes, output redirects and full configuration queries are rejected. The SSH
account also needs permissions for each query it runs.

`display logbuffer` defaults to `severity: error` (levels 0–3) and 20 entries.
`severity: all` includes all recognized entries. The limit is 1–50 entries and
applied after filtering on the server. Error queries use a fixed filter on the
device where supported and validate severity again before returning entries.
`log_buffer` reports scanned, matched and returned entries and whether the device
filtered the buffer, plus any device-reported current, dropped and overwritten
message counts. A capped scan marks the result incomplete. The CLI shares
one timeout and byte budget per command, including paged output.

Default line limit is 200, maximum 1000. Journal defaults to the last 30 minutes;
one call may cover at most 24 hours. These are bounded snapshots, not exhaustive
searches: a line limit can omit earlier evidence even when the byte-truncation
flags are false. File tails do not enforce an incident time window.

The user must be enabled, have `/remote-access/execute`, and have read or write
membership in the connection's current business group. Configuring connections
requires `/remote-access/connections/write` plus group write membership; managing
credentials requires `/remote-access/credentials/write` plus group write membership. Admins still need a real Target and matching business-group
binding. Merely seeing monitoring data does not grant SSH access. A missing
connection or failed authorization does not cause fallback to arbitrary shell.

Results include `execution_id`, the normalized request, approved log paths
(returned only after authorization), start/finish UTC times,
whether a jump was used, exit status, stdout/stderr, per-stream byte truncation,
`incomplete` and `error`. Output is fenced as untrusted data for the Agent.
Configured passwords are redacted if they appear verbatim in captured output;
this is not general-purpose log-secret detection. Avoid collecting sensitive
logs. Secrets are not passed as tool arguments or logged in execution audit.

Timeout covers TCP, both handshakes and command execution. On Linux the target
also uses `timeout` with a TERM/KILL grace period. Cancellation
closes the SSH transports; a broken connection does not prove termination on
the target. Nonzero status, network failure, cancellation and truncation remain
visible and cannot be reported as a healthy result. SSH login/session hooks are
controlled by the remote administrator and are not sandboxed by the center.

Timeout configuration defaults to 30 seconds (supported 1–120); each stdout and
stderr defaults to 32 KiB (maximum 256 KiB); per-center concurrency defaults to
4 (maximum 32). Out-of-range deployment settings fall back to defaults. Limits
apply per center process, not across a multi-center deployment.

The tool requires no separate confirmation for each permitted read. It cannot
restart services, write configuration, delete files or run model-generated
commands. It does not attach a general-purpose SSH session to the skill sandbox.

## Acceptance and evidence

1. Provision a test Target, group, keyring and encrypted credential. Enable the
   feature in an isolated preview; do not point a disposable preview at a
   production database or unapproved device.
2. Ask the built-in Agent: “分析 web-01 在 14:30 的 nginx 告警，检查该时间段的
   服务日志、进程和磁盘，只读取数据。” The Agent resolves the Target and window,
   calls the checks, and explains observed facts, hypotheses and missing evidence
   with execution IDs and log timestamps.
3. Verify an unprivileged user, changed group, wrong password/key, unapproved log
   path and attempts to supply shell commands fail. Repeat via a test jump host.
4. Cancel or time out a check and exercise output truncation; the answer must
   retain incomplete-evidence status. Inspect audit metadata and the chat's tool
   results without publishing private raw logs or keys.

Automated SSH protocol tests run local test servers with password authentication,
exec output and forwarding; they do not execute received commands on the host.
Tests establish transport, restrictions and result handling, not correctness of
an LLM diagnosis, a commercial bastion integration or human acceptance.

The **测试 SSH 连接** button confirms host keys and then verifies password
login, including optional jump forwarding. It opens no SSH session and sends no
command, and does not create credentials or save a device/connection. Successful
and failed tests are shown in the editor. Accepted fingerprints remain only in
the open draft, and are checked again before saving; a changed key prompts again.

## Management API

All paths are under `/api/n9e/remote-access`, require native user authentication,
and return `{dat,err}` with `Cache-Control: no-store`. They are not exposed via
the Skill API gateway or service API.

| Method / path | Behavior |
| --- | --- |
| GET `/features` | enabled, configure, manage_credentials, execute, terminal, rdp, and legacy secure_input |
| POST `/connection/test` | SSH password login test with accepted pins, without persistence |
| POST `/host-key` | discover a target or jump SSH fingerprint without saving |
| GET `/connection?ident=web-01` | redacted endpoint metadata, version, configured/stale flags |
| PUT `/connection?ident=web-01` | protocol, group_id, enabled, target, optional SSH jump/log_paths, record_version |
| DELETE `/connection?ident=web-01` | remove binding and its unreferenced device credentials |
| POST `/terminal/ticket` | get a one-time WebSocket ticket for ident and protocol |

The device list opens **控制台** using the configured protocol. Operators need
`/remote-access/terminal` and business-group write access. SSH requires
`EnableTerminal = true`. RDP requires `RDPGatewayAddress` set to the loopback
address and port of Apache Guacamole's guacd service, for example
`RDPGatewayAddress = "127.0.0.1:14822"`. Run guacd on the Center host, bind
its listener to loopback, persist its trust store, and ensure it can reach the
device's RDP port. The gateway remembers the first RDP certificate and rejects
a changed certificate. Windows must have Remote Desktop enabled and grant the
account remote logon rights. A domain account can be entered as `DOMAIN\username`.
See [Apache Guacamole deployment](https://guacamole.apache.org/doc/gug/guacamole-docker.html).

The console uses `ws` for HTTP and `wss` for HTTPS. Its short-lived one-time
ticket is bound to the user, device, protocol and page origin. The Center uses
the forwarded HTTPS scheme only from a local proxy that overwrites
`X-Forwarded-Proto` using the incoming scheme. RDP credentials go from Center
to the local gateway and are never sent to the browser in the stream.

An endpoint contains `host`, `port`, and either the same endpoint's
`credential_id` or `new_credential: {username,password}`. SSH also requires
`fingerprint`. New connections use version zero; updates and deletions
require the current version and return 409 on conflicts. Secret-bearing bodies
are strictly decoded, bounded and never echoed in errors.

Managed device POST/PUT accepts optional `remote_access` with the same connection
input. It must use the device's group; failure rolls back Target, device,
credential, binding and publication together. Probe creation discards this
optional input and never forwards SSH credentials to collectors.

`Center.Collection.BackgroundDisabled=true` serves collection management APIs
without running reachability initialization, probe or rule-arming workers. It is
for a secondary Center sharing a database with an existing worker owner. It does
not disable user-authorized device mutations or other Center background tasks.
