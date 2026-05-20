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

// The swarm-vs-SDLC model (2026-05-20 refocus). Kept in sync with
// docs/strategy/chitin-swarm-sdlc-model-2026-05-20.md.
const DIAGRAM = `flowchart LR
  subgraph SDLC["Autonomous SDLC loop — spec-kit driven"]
    direction LR
    SPEC["Spec<br/>speckit-specify"] --> PLAN["Plan<br/>speckit-plan"]
    PLAN --> TASKS["Tasks<br/>speckit-tasks → task DAG"]
    TASKS --> ANALYZE["Analyze<br/>speckit-analyze"]
    ANALYZE --> IMPL["Implement"]
    IMPL --> REVIEW["Review<br/>Copilot + peer agent"]
    REVIEW --> MERGE["Merge → main"]
  end
  ORCH[["CHITIN ORCHESTRATOR (Temporal)<br/>sequences the task DAG deterministically<br/>— THE DRIVER"]]
  TASKS -->|"task DAG"| ORCH
  ORCH -->|"dispatches work units"| IMPL
  ORCH -->|"schedules"| REVIEW
  subgraph AGENTS["Agents — agent-agnostic; each work unit = a worker in its own worktree"]
    ARES["Ares"]
    CLAWTA["Clawta"]
    CC["Claude Code"]
    COPILOT["Copilot"]
    OWN["future: first-party agent"]
  end
  ORCH --> AGENTS
  KERNEL[["CHITIN KERNEL<br/>gates every tool call vs chitin.yaml"]]
  AGENTS -.->|"every tool call"| KERNEL
  subgraph TELEM["CHITIN TELEMETRY — observe, never drive"]
    CHAIN["chitin chain"]
    ARGUS["Argus / Sentinel"]
    LOG["activity log<br/>(former kanban — read surface)"]
  end
  KERNEL --> CHAIN
  ORCH --> CHAIN
  MERGE --> TELEM
  TELEM -->|"feedback → next spec"| SPEC
`;

@Component({
  selector: 'cc-sdlc-diagram',
  standalone: true,
  imports: [CommonModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [`
    :host { display: block; }
    .head { margin-bottom: 16px; }
    .title { margin: 0; font-size: 20px; font-weight: 600; }
    .subtitle { margin: 4px 0 0; color: var(--muted); font-size: 11.5px; max-width: 70ch; }
    .diagram-wrap {
      padding: 20px; overflow: auto;
      border: 1px solid var(--border, #2a2a32); border-radius: 8px;
      background: var(--panel, #14141a);
    }
    .diagram-wrap :global(svg) { max-width: 100%; height: auto; }
    .err { color: #e6736b; font-family: var(--font-mono); font-size: 12px; }
  `],
  template: `
    <div class="head">
      <h1 class="title">Swarm &times; SDLC</h1>
      <p class="subtitle">
        How the Chitin swarm runs the software-development lifecycle:
        the orchestrator drives, the kernel gates, telemetry observes.
        The board is a read-surface, not a steering wheel.
      </p>
    </div>
    <div class="diagram-wrap"><div #graph></div></div>
    @if (err()) { <p class="err">diagram render failed: {{ err() }}</p> }
  `,
})
export class SdlcDiagramPage implements AfterViewInit {
  @ViewChild('graph', { static: true }) graph!: ElementRef<HTMLDivElement>;
  readonly err = signal<string | null>(null);

  async ngAfterViewInit(): Promise<void> {
    try {
      mermaid.initialize({ startOnLoad: false, theme: 'dark', securityLevel: 'strict' });
      const { svg } = await mermaid.render('sdlc-graph', DIAGRAM);
      this.graph.nativeElement.innerHTML = svg;
    } catch (e) {
      this.err.set(String(e));
    }
  }
}
