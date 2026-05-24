import {
  AfterViewInit,
  ChangeDetectionStrategy,
  Component,
  ElementRef,
  ViewChild,
  signal,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import mermaid from 'mermaid';

// System state — every factory surface with status. Snapshot 2026-05-23.
// Companion doc: docs/architecture/system-state-2026-05-23.md.
//
// Status legend mirrors the markdown doc:
//   done       — on main, exercised in production
//   merged     — on main but no live end-to-end run yet
//   inProgress — open PR or partial impl
//   planned    — spec authored, no impl yet
//   idea       — discussed in-session, not yet spec'd
//   retired    — being or already removed
const DIAGRAM = `flowchart TB
  subgraph AUTH["1 · Spec authoring"]
    SK["spec-kit + lint<br/>.specify/specs/NNN"]:::done
    IDX["INDEX.md convention<br/>spec 024 §1.3"]:::done
    SLT["chitin-kernel speckit-lint<br/>spec 088 — deterministic linter"]:::done
  end

  subgraph TRIG["2 · Trigger surface — three paths"]
    CLI["chitin-orchestrator schedule<br/>spec 097 — CLI"]:::done
    WH["factory-listen<br/>spec 098 — webhook<br/>PR #946"]:::inProgress
    GHI["chitin-orchestrator schedule --driver copilot<br/>spec 099 — GitHub issue<br/>PR #947 design only"]:::planned
  end

  subgraph ORCH["3 · Orchestrator"]
    SCH["SchedulerWorkflow<br/>spec 076"]:::done
    WUW["WorkUnitWorkflow<br/>spec 076"]:::done
    WT["Worktree manager<br/>spec 070"]:::done
    ADP["Spec-kit adapter<br/>spec 077 — tasks.md → DAG"]:::done
    REG["Driver registry<br/>spec 075 — capability cards"]:::done
    TSCH["Temporal Schedules<br/>spec 081 — partial migration"]:::inProgress
    LOOP["ImprovementLoopWorkflow<br/>spec 078 — nil deps"]:::inProgress
    INGEST["IngestionWorkflow<br/>spec 079 — nil deps"]:::inProgress
  end

  subgraph DRV["4 · Drivers — capability-matched"]
    D1["claudecode"]:::done
    D2["codex"]:::done
    D3["gemini"]:::done
    D4["hermes"]:::done
    D5["copilot CLI"]:::done
    D6["openclaw"]:::done
    D7["local — operator hand-off"]:::done
    D8["Copilot — GitHub-native<br/>spec 099 — runs OFF-machine"]:::planned
  end

  subgraph REV["5 · Review"]
    PRW["PRReviewWorkflow<br/>spec 094 — on main, awaiting live run"]:::merged
    DM["dialectic verdict math<br/>4-value StructuredVerdict"]:::merged
    SDC["short-circuit on agreement"]:::merged
    ARB["class-routed arbiter<br/>spec 094 — not yet live"]:::idea
    SIG["re-review / override-review signals<br/>spec 094 v1.1"]:::idea
  end

  subgraph MQ["6 · Merge"]
    MQW["MergeQueueWorkflow<br/>spec 093 — design only"]:::planned
    POL["6-class policy table"]:::planned
    HM["Human merge<br/>current state"]:::done
  end

  subgraph GOV["7 · Governance — observe + enforce"]
    KRN["chitin-kernel<br/>gates every tool call"]:::done
    SES["session unlock / lock / status<br/>spec 096 — sole chain writer"]:::done
    SHK["stop-hook recovery<br/>spec 091 v1.1 — closes clawta loop"]:::done
    GIT["git-ops recorder<br/>process-tree attribution"]:::done
    BND["bounds:max_lines_changed<br/>2000-line ceiling — fired 3× this session"]:::done
    BNX["bounds-gate escape hatch<br/>not spec'd"]:::idea
  end

  subgraph TLM["8 · Telemetry"]
    CHN["kernel chain<br/>events-&lt;run_id&gt;.jsonl"]:::done
    SNT["sentinel<br/>analyzer + Neon execution_events"]:::done
    DSC["Discord notifier<br/>spec 080"]:::done
    EVL["/evolve<br/>workspace PR #424"]:::retired
    TRA["telemetry-recovery sentinel adapter<br/>closes spec 099 blind spot"]:::idea
  end

  AUTH --> TRIG
  CLI --> SCH
  WH --> SCH
  GHI -.->|creates issue, NOT scheduler| GHX["GitHub Copilot drafts PR"]:::planned
  GHX --> WH
  SCH --> WUW
  ADP --> SCH
  WUW --> WT
  WUW --> REG
  REG --> DRV
  D1 -.-> REV
  D2 -.-> REV
  D3 -.-> REV
  D4 -.-> REV
  D5 -.-> REV
  D6 -.-> REV
  D8 -.->|PR detected via webhook| REV
  REV --> MQ
  MQ --> HM
  GOV -.->|gates every call| DRV
  GOV -.->|observes| ORCH
  DRV -->|kernel emit| CHN
  CHN --> SNT
  SCH -.->|work events| DSC

  classDef done       fill:#1d3a26,stroke:#3a7d50,color:#cce8d4
  classDef merged     fill:#3a3318,stroke:#7d6f3a,color:#e8dcc4
  classDef inProgress fill:#3a2a14,stroke:#7d5a2c,color:#e8c490
  classDef planned    fill:#1d2a3a,stroke:#3a5d7d,color:#c4d8e8
  classDef idea       fill:#2a1d3a,stroke:#5d3a7d,color:#d8c4e8
  classDef retired    fill:#3a1d1d,stroke:#7d3a3a,color:#e8c4c4
`;

@Component({
  selector: 'cc-system-state',
  standalone: true,
  imports: [CommonModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .head { margin-bottom: 16px; }
    .title { margin: 0; font-size: 20px; font-weight: 600; }
    .subtitle { margin: 4px 0 0; color: var(--muted); font-size: 11.5px; max-width: 80ch; }
    .legend {
      margin: 12px 0 0; display: flex; flex-wrap: wrap; gap: 6px 14px;
      color: var(--muted); font-size: 11px;
    }
    .legend .swatch {
      display: inline-flex; align-items: center; gap: 6px;
      font-family: var(--font-mono); font-size: 10.5px;
      color: var(--fg, #d8d8e0);
    }
    .legend .dot {
      width: 10px; height: 10px; border-radius: 2px;
      border: 1px solid currentColor;
    }
    .swatch.done       { color: #cce8d4; } .swatch.done       .dot { background: #1d3a26; border-color: #3a7d50; }
    .swatch.merged     { color: #e8dcc4; } .swatch.merged     .dot { background: #3a3318; border-color: #7d6f3a; }
    .swatch.inProgress { color: #e8c490; } .swatch.inProgress .dot { background: #3a2a14; border-color: #7d5a2c; }
    .swatch.planned    { color: #c4d8e8; } .swatch.planned    .dot { background: #1d2a3a; border-color: #3a5d7d; }
    .swatch.idea       { color: #d8c4e8; } .swatch.idea       .dot { background: #2a1d3a; border-color: #5d3a7d; }
    .swatch.retired    { color: #e8c4c4; } .swatch.retired    .dot { background: #3a1d1d; border-color: #7d3a3a; }
    .diagram-wrap {
      margin-top: 16px; padding: 20px; overflow: auto;
      border: 1px solid var(--border, #2a2a32); border-radius: 8px;
      background: var(--panel, #14141a);
    }
    .diagram-wrap :global(svg) { max-width: 100%; height: auto; }
    .err { color: #e6736b; font-family: var(--font-mono); font-size: 12px; }
    .footer-link {
      margin-top: 14px; color: var(--muted); font-size: 11px;
      font-family: var(--font-mono);
    }
  `],
  template: `
    <div class="head">
      <h1 class="title">System state — 2026-05-23</h1>
      <p class="subtitle">
        Every factory surface with a status marker. Companion doc:
        <code>docs/architecture/system-state-2026-05-23.md</code> (5 levels,
        spec inventory, open PRs, operator follow-ups). This diagram is the
        single-pane summary &mdash; the doc has the detail.
      </p>
      <div class="legend">
        <span class="swatch done"><span class="dot"></span>done — on main, in production</span>
        <span class="swatch merged"><span class="dot"></span>merged — on main, awaiting live exercise</span>
        <span class="swatch inProgress"><span class="dot"></span>in progress — open PR or partial impl</span>
        <span class="swatch planned"><span class="dot"></span>planned — spec'd, not impl'd</span>
        <span class="swatch idea"><span class="dot"></span>idea — discussed, not spec'd</span>
        <span class="swatch retired"><span class="dot"></span>retired — being or already removed</span>
      </div>
    </div>
    <div class="diagram-wrap"><div #graph></div></div>
    @if (err()) { <p class="err">diagram render failed: {{ err() }}</p> }
    <p class="footer-link">
      solid arrows = work flow &middot; dotted arrows = governance, telemetry,
      and feedback &middot; subgraphs numbered in operator-loop order
    </p>
  `,
})
export class SystemStatePage implements AfterViewInit {
  @ViewChild('graph', { static: true }) graph!: ElementRef<HTMLDivElement>;
  readonly err = signal<string | null>(null);

  async ngAfterViewInit(): Promise<void> {
    try {
      mermaid.initialize({ startOnLoad: false, theme: 'dark', securityLevel: 'strict' });
      const { svg } = await mermaid.render('system-state-graph', DIAGRAM);
      this.graph.nativeElement.innerHTML = svg;
    } catch (e) {
      this.err.set(String(e));
    }
  }
}
