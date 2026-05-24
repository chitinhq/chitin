# Spec 103 — Phase 1 Data Model

Five storage surfaces:
1. **Chain** (existing) — append-only event ledger, kernel-managed
2. **swarm-results.db** (new) — SQLite queue at `~/.chitin/swarm-results.db`
3. **swarm-schedule.yml** (new) — operator-managed YAML config
4. **ingestion-sources.yml** (new) — operator-managed YAML config
5. **sentinel-findings/*.json** (convention) — agent-written, sentinel-watched

## Chain event types (new, kernel-emitted)

### `swarm_invocation`

Emitted once per `SwarmInvocationWorkflow` firing.

| Field | Type | Notes |
|---|---|---|
| `schedule_id` | string | from swarm-schedule.yml entry's `id` |
| `agent` | string | `ares` \| `clawta` |
| `gateway` | string | resolved gateway: `hermes-mcp` \| `openclaw-cli` |
| `gateway_session` | string | session id; resolved or auto-detected |
| `cadence` | string | e.g. `6h`, `24h` — verbatim from config |
| `message` | string | literal message sent to the agent |
| `tag` | string \| null | optional operator tag |
| `skills` | []string | optional hint list (FR-002); empty if not declared |
| `temporal_run_id` | string | for replay |
| `ts` | RFC 3339 | server-emit timestamp |

Required FR: FR-004.

### `swarm_finding_queued`

Emitted by `FetchAndRead` activity (vault ingestion) after writing a queue row.

| Field | Type | Notes |
|---|---|---|
| `queue_id` | string | ULID, primary key in findings table |
| `source` | string | e.g. `obsidian-vault`, `ares-direct` |
| `agent_attribution` | string \| null | agent name if recoverable from path/frontmatter |
| `tag` | string \| null | inherited from schedule's `tag` if correlated; null for unsolicited drops |
| `topic` | string \| null | extracted from path or frontmatter |
| `file_path` | string | relative to source root |
| `triggered_by_chain_event` | string \| null | correlated `swarm_invocation` event ID, best-effort |
| `ts` | RFC 3339 | |

### `swarm_finding_triaged`

Emitted by `chitin-orchestrator swarm-queue mark <id> <transition>`.

| Field | Type | Notes |
|---|---|---|
| `queue_id` | string | |
| `from_status` | string | prior status |
| `to_status` | string | new status |
| `spec_ref` | string \| null | populated when transition is `spec-drafted` |
| `actor` | string | always `operator` in v1 |
| `notes` | string \| null | operator-provided note |
| `ts` | RFC 3339 | |

## SQLite schema — `~/.chitin/swarm-results.db`

DDL exactly per spec.md FR-016:

```sql
CREATE TABLE findings (
  queue_id TEXT PRIMARY KEY,                -- ULID
  ts TEXT NOT NULL,
  source TEXT NOT NULL,                     -- e.g. 'obsidian-vault', 'ares-direct'
  agent_attribution TEXT,                   -- 'ares' | 'clawta' | NULL
  tag TEXT,                                 -- free-form from schedule entry, NULL if ad-hoc
  topic TEXT,                               -- best-effort from path or frontmatter
  file_path TEXT,
  frontmatter_json TEXT,
  body_excerpt TEXT,                        -- first ~512 chars of body text
  status TEXT NOT NULL,                     -- 'unprocessed' | 'spec_drafted' | 'discarded' | 'deferred' | 'source_deleted'
  spec_drafted_ref TEXT,
  triggered_by_chain_event TEXT,
  confidence_signal REAL,
  novelty_signal TEXT,                      -- 'novel' | 'partial-overlap' | 'covered'
  affects_core_infra INTEGER DEFAULT 0,     -- bool 0/1
  estimated_loc_range TEXT,                 -- 's' | 'm' | 'l' | 'xl'
  notes TEXT                                -- operator notes
);
CREATE INDEX findings_status_tag ON findings(status, tag);
CREATE INDEX findings_ts ON findings(ts);

-- migrations metadata
CREATE TABLE _schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);
```

Status state machine:

```
unprocessed ──→ spec_drafted   ← via swarm-queue mark spec-drafted <REF>
            ──→ discarded      ← via swarm-queue mark discarded
            ──→ deferred       ← via swarm-queue mark deferred
            ──→ source_deleted ← via fsnotify IN_DELETE (automatic, no operator action)

deferred ──→ unprocessed       ← via swarm-queue mark unprocessed (un-defer)
deferred ──→ spec_drafted | discarded

source_deleted is terminal from the ingestion side, but operator can still
mark it discarded or spec_drafted for record-keeping.
```

## YAML config schemas (operator-managed)

### `~/.chitin/swarm-schedule.yml` (per FR-001)

```yaml
schedules:
  - id: string                  # unique within the file; Schedule ID = "swarm-" + id
    agent: ares | clawta        # must be in driver registry
    cadence: string             # parseable duration: 5m | 1h | 6h | 24h | "0 */6 * * *" cron
    message: string             # literal; sent verbatim to the agent
    skills: [string]            # optional, free-form hint list (FR-002); chitin does NOT verify
    tag: string                 # optional, free-form operator tag
    gateway_override: string    # optional; defaults from agent identity
    gateway_session: string     # optional; auto-resolved from agent if omitted
    wait_for_reply: bool        # optional; default false (for on-demand swarm-ask only)
```

Validation: structural only. Required fields: `id`, `agent`, `cadence`, `message`. Cadence parseable. Agent in registry. No taxonomy enforcement on `message`, `tag`, or `skills`.

### `~/.chitin/ingestion-sources.yml` (per FR-010)

```yaml
sources:
  - name: string                # unique within file; used in queue's `source` column
    type: string                # 'obsidian-vault' | 'sentinel-findings' | custom
    root: string                # absolute path
    patterns: [string]          # glob(s) relative to root; matched files are ingested
    watch: bool                 # true = fsnotify watch; false = poll-on-demand
    extract:                    # optional frontmatter mapping (key -> column)
      topic: frontmatter.topic
      tag: frontmatter.tag
      confidence: frontmatter.confidence
    tag_default: string         # optional; default tag for rows from this source
```

Default sources for v1:

```yaml
sources:
  - name: obsidian-vault
    type: obsidian-vault
    root: /home/red/Documents/Obsidian Vault/Research
    patterns: ["**/sources/*.md", "**/index.md"]
    watch: true
    extract:
      topic: frontmatter.topic
      tag: frontmatter.tag
    tag_default: research

  - name: sentinel-mined
    type: sentinel-findings
    root: /home/red/.chitin/sentinel-findings
    patterns: ["swarm-mined-*.json"]
    watch: true
    extract:
      agent_attribution: payload.agent
      confidence: payload.confidence
      triggered_by_chain_event: payload.originated_from_chain_event
```

## Sentinel-findings JSON schema (per FR-014)

```json
{
  "id": "ulid",
  "source": "swarm-mined",
  "agent": "ares|clawta",
  "originated_from_chain_event": "<swarm_invocation event id>",
  "ts": "RFC3339",
  "question": "string — the original mining question",
  "window": "iso8601 duration",
  "pattern": "string — what the agent found",
  "supporting_event_ids": ["..."],
  "confidence": 0.0-1.0
}
```

Required: `id`, `source`, `agent`, `ts`, `pattern`. Others optional.

## Transient Go types

### `internal/swarm/schedule_config.go`

```go
type Schedule struct {
    ID              string   `yaml:"id"`
    Agent           string   `yaml:"agent"`
    Cadence         string   `yaml:"cadence"`
    Message         string   `yaml:"message"`
    Skills          []string `yaml:"skills,omitempty"`
    Tag             string   `yaml:"tag,omitempty"`
    GatewayOverride string   `yaml:"gateway_override,omitempty"`
    GatewaySession  string   `yaml:"gateway_session,omitempty"`
    WaitForReply    bool     `yaml:"wait_for_reply,omitempty"`
}

type ScheduleConfig struct {
    Schedules []Schedule `yaml:"schedules"`
}

func LoadScheduleConfig(path string) (*ScheduleConfig, error)
func (c *ScheduleConfig) Validate(driverRegistry DriverRegistry) []error
```

### `internal/swarm/queue_repo.go`

```go
type Finding struct {
    QueueID                 string
    Ts                      time.Time
    Source                  string
    AgentAttribution        *string
    Tag                     *string
    Topic                   *string
    FilePath                string
    FrontmatterJSON         *string
    BodyExcerpt             string
    Status                  string
    SpecDraftedRef          *string
    TriggeredByChainEvent   *string
    ConfidenceSignal        *float64
    NoveltySignal           *string
    AffectsCoreInfra        bool
    EstimatedLOCRange       *string
    Notes                   *string
}

type Repo interface {
    Insert(ctx context.Context, f Finding) error
    Upsert(ctx context.Context, source, filePath string, f Finding) error
    List(ctx context.Context, filter ListFilter) ([]Finding, error)
    Show(ctx context.Context, id string) (*Finding, error)
    Transition(ctx context.Context, id, newStatus string, specRef, notes *string) error
    MarkSourceDeleted(ctx context.Context, source, filePath string) error
}
```

### `internal/gateway/openclaw.go`

```go
type OpenClawClient struct {
    Binary string // default "openclaw"; testable via injection
}

type SendInput struct {
    Session  string
    Message  string
    Timeout  time.Duration
}

type SendOutput struct {
    Stdout    string
    Stderr    string
    ExitCode  int
    Elapsed   time.Duration
}

func (c *OpenClawClient) Send(ctx context.Context, in SendInput) (SendOutput, error)
```

### `internal/gateway/hermes_mcp.go`

```go
type HermesMCPClient struct {
    Binary  string // "hermes"
    Args    []string // ["mcp", "serve"]
    HomeDir string // pinned via HERMES_HOME env
}

type MCPSendInput struct {
    Channel       string
    Message       string
    WaitForReply  bool
    ReplyTimeout  time.Duration
}

type MCPSendOutput struct {
    Sent       bool
    Reply      *string // populated iff WaitForReply
    ExitCode   int
    Elapsed    time.Duration
}

func (c *HermesMCPClient) Send(ctx context.Context, in MCPSendInput) (MCPSendOutput, error)
```

## State transitions visible in `swarm-queue`

```
fsnotify IN_CREATE/IN_MODIFY
   │
   ▼
FetchAndRead activity
   │
   ▼
Repo.Upsert
   │
   ▼  status=unprocessed
findings row + chain swarm_finding_queued
   │
   ├──→ operator swarm-queue mark spec-drafted REF
   │       │
   │       ▼  status=spec_drafted
   │     chain swarm_finding_triaged
   │
   ├──→ operator swarm-queue mark discarded
   │       │
   │       ▼  status=discarded
   │     chain swarm_finding_triaged
   │
   ├──→ operator swarm-queue mark deferred
   │       │
   │       ▼  status=deferred
   │     chain swarm_finding_triaged
   │
   └──→ fsnotify IN_DELETE
           │
           ▼  status=source_deleted
         no chain event (automatic upkeep)
```
