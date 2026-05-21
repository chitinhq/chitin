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

// The Chitin Orchestrator — full wiring (specs 070, 075, 076, 077, 078, 079).
// One deterministic Temporal worker host replaces ~36 crons + ~52 shell
// scripts: a spec compiles to a Work-Unit DAG, the scheduler walks it, every
// work unit runs in an isolated worktree and ships a reviewable PR.
const DIAGRAM = `flowchart TB
  subgraph SRC["1 · Spec sources"]
    SK["spec-kit<br/>.specify/specs/NNN"]
    OS["OpenSpec<br/>changes/"]
  end
  ADP["Spec-Kit Adapter · 077<br/>adapter.Compile — pure, deterministic"]
  DAG["Work-Unit DAG<br/>agent nodes + deterministic nodes"]
  SK --> ADP
  OS --> ADP
  ADP --> DAG

  subgraph ORCH["2 · Chitin Orchestrator — one Temporal worker host · 070"]
    SCHED["SchedulerWorkflow · 076<br/>runnable frontier · priority order<br/>append signal · Continue-As-New every 500 ticks"]
    SEL["SelectDriver activity<br/>capability tag → driver"]
    subgraph WUW["WorkUnitWorkflow — one durable child per node · 076"]
      CW["CreateWorktree<br/>fresh isolated git worktree"]
      KIND{"node kind"}
      INV["InvokeDriver per driver<br/>agent node — spawns a real agent"]
      DET["RunDeterministicStep<br/>deterministic node — no driver, no tokens"]
      DLV["DeliverWorkProduct<br/>commit → push branch → open PR"]
      TDN["TeardownWorktree<br/>deferred — always runs"]
      CW --> KIND
      KIND -->|agent| INV
      KIND -->|deterministic| DET
      INV --> DLV
      DET --> DLV
      DLV --> TDN
    end
    SCHED -->|per runnable node| SEL
    SCHED -->|dispatch durable child| WUW
    SEL -.->|selected driver| WUW
  end
  DAG --> SCHED

  subgraph DRV["3 · Agent drivers · 075 — agent-agnostic, capability-matched"]
    D1["claudecode"]
    D2["codex"]
    D3["hermes"]
    D4["openclaw"]
    D5["local LLM"]
    D6["gemini"]
    D7["copilot"]
  end
  SEL --> DRV
  INV --> DRV

  PR["Pull Request on a chitin/wu/* branch<br/>— the PR-out gate"]
  DLV --> PR

  subgraph SIDE["4 · Continuous workflows"]
    LOOP["ImprovementLoopWorkflow · 078<br/>telemetry → analysis → spec proposals → human gate"]
    INGEST["IngestionWorkflow · 079<br/>research and sources → knowledge base"]
  end

  subgraph GT["5 · Governance and telemetry — observe, never drive"]
    KRN["Chitin Kernel<br/>gates every tool call vs chitin.yaml"]
    BRD["Board read-model<br/>ProjectToBoard activity"]
    TLM["OTLP tick telemetry<br/>EmitTickTelemetry activity"]
    CHN["chitin chain<br/>durable audit trail"]
  end
  DRV -.->|every tool call| KRN
  SCHED --> BRD
  SCHED --> TLM
  KRN --> CHN
  BRD --> LOOP
  TLM --> LOOP
  CHN --> LOOP
  INGEST -.->|curated knowledge| LOOP
  LOOP -.->|proposed specs → human gate| SK

  subgraph HUMAN["6 · Human surfaces — review and observe, never dispatch"]
    DISCORD["Discord channels<br/>notifications — posted out, never in"]
    CONSOLE["chitin-console<br/>board · KPIs · this diagram"]
    REVIEW["GitHub PR review<br/>a human reads and merges"]
  end
  SCHED -.->|work events| DISCORD
  WUW -.->|work events| DISCORD
  BRD --> CONSOLE
  PR --> REVIEW
  REVIEW -->|merge → main| TLM
`;

@Component({
  selector: 'cc-orchestrator-diagram',
  standalone: true,
  imports: [CommonModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .head { margin-bottom: 16px; }
    .title { margin: 0; font-size: 20px; font-weight: 600; }
    .subtitle { margin: 4px 0 0; color: var(--muted); font-size: 11.5px; max-width: 78ch; }
    .legend {
      margin: 12px 0 0; display: flex; flex-wrap: wrap; gap: 6px 14px;
      color: var(--muted); font-size: 11px;
    }
    .legend code {
      font-family: var(--font-mono); font-size: 10.5px;
      color: var(--fg, #d8d8e0);
    }
    .diagram-wrap {
      margin-top: 16px; padding: 20px; overflow: auto;
      border: 1px solid var(--border, #2a2a32); border-radius: 8px;
      background: var(--panel, #14141a);
    }
    .diagram-wrap :global(svg) { max-width: 100%; height: auto; }
    .err { color: #e6736b; font-family: var(--font-mono); font-size: 12px; }
  `],
  template: `
    <div class="head">
      <h1 class="title">Orchestrator</h1>
      <p class="subtitle">
        The Chitin Orchestrator end to end &mdash; one deterministic Temporal
        worker host in place of ~36 crons and ~52 shell scripts. A spec
        compiles to a Work-Unit DAG; the scheduler walks it; every work unit
        runs in its own isolated git worktree, spawns a capability-matched
        agent or a deterministic step, and ships a reviewable PR.
      </p>
      <div class="legend">
        <span><code>solid</code> = work flow and dispatch</span>
        <span><code>dotted</code> = governance, telemetry and feedback</span>
        <span><code>diamond</code> = branch on node kind</span>
      </div>
    </div>
    <div class="diagram-wrap"><div #graph></div></div>
    @if (err()) { <p class="err">diagram render failed: {{ err() }}</p> }
  `,
})
export class OrchestratorDiagramPage implements AfterViewInit {
  @ViewChild('graph', { static: true }) graph!: ElementRef<HTMLDivElement>;
  readonly err = signal<string | null>(null);

  async ngAfterViewInit(): Promise<void> {
    try {
      mermaid.initialize({ startOnLoad: false, theme: 'dark', securityLevel: 'strict' });
      const { svg } = await mermaid.render('orchestrator-graph', DIAGRAM);
      this.graph.nativeElement.innerHTML = svg;
    } catch (e) {
      this.err.set(String(e));
    }
  }
}
