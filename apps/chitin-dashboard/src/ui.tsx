import { useEffect, useMemo, useState } from 'react';

type DashboardSession = {
  session_id: string;
  ts: string;
  driver?: string;
  agent?: string;
  ticket_id?: string;
  cost_usd: number;
  success?: boolean | null;
  elo_delta?: number | null;
};

type EloRow = {
  driver: string;
  model: string;
  role?: string;
  task_class?: string;
  elo_score: number;
  dispatches_count: number;
  last_dispatch_id?: string;
};

type Timeline = {
  session_id: string;
  started_at?: string;
  ended_at?: string;
  summary: {
    total_cost_usd: number;
    total_tokens: number;
    tool_call_count: number;
  };
  steps: TimelineStep[];
};

type TimelineStep = {
  event_id?: string;
  type: string;
  ts: string;
  driver?: string;
  agent?: string;
  tool?: string;
  input?: unknown;
  output?: unknown;
  decision?: {
    allowed: boolean;
    mode?: string;
    rule_id?: string;
    reason?: string;
    suggestion?: string;
  };
  cost?: {
    usd?: number;
    total_tokens?: number;
    input_bytes?: number;
    output_bytes?: number;
  };
  prediction?: {
    predicted_blast?: number;
    floundering_score?: number;
    drift_score?: number;
    routing_decision?: string;
  };
  duration_ms?: number;
};

type SessionsResponse = { sessions: DashboardSession[]; error?: string };
type EloResponse = { rows: EloRow[]; placeholder: boolean; error?: string };
type TimelineResponse = { timeline?: Timeline; error?: string };
type PolicyResponse = { path?: string; body?: string; error?: string };

const nav = [
  { href: '/', label: 'Sessions' },
  { href: '/policy', label: 'Policy' },
];

export function App() {
  const [path, setPath] = useState(window.location.pathname);

  useEffect(() => {
    const onPop = () => setPath(window.location.pathname);
    window.addEventListener('popstate', onPop);
    return () => window.removeEventListener('popstate', onPop);
  }, []);

  const navigate = (href: string) => {
    window.history.pushState({}, '', href);
    setPath(href);
  };

  return (
    <div className="min-h-screen bg-[radial-gradient(circle_at_top_left,_rgba(221,239,229,0.9),_transparent_28%),linear-gradient(180deg,_#f6f2ea,_#efe7da_50%,_#e8e5df)] text-ink">
      <div className="mx-auto max-w-[1440px] px-8 py-10">
        <header className="mb-10 flex items-end justify-between gap-6">
          <div>
            <p className="mb-3 text-xs uppercase tracking-[0.28em] text-muted">Chitin Ledger Replay</p>
            <h1 className="text-5xl font-semibold tracking-[-0.04em]">Operator dashboard</h1>
          </div>
          <nav className="flex gap-2 rounded-full border border-line/80 bg-white/70 p-2 shadow-frame backdrop-blur">
            {nav.map((item) => (
              <button
                key={item.href}
                className={buttonClass(path === item.href || (item.href === '/' && path.startsWith('/session/')))}
                onClick={() => navigate(item.href)}
                type="button"
              >
                {item.label}
              </button>
            ))}
          </nav>
        </header>
        {path === '/policy' ? (
          <PolicyView />
        ) : path.startsWith('/session/') ? (
          <TimelineView sessionID={decodeURIComponent(path.replace('/session/', ''))} navigate={navigate} />
        ) : (
          <SessionsView navigate={navigate} />
        )}
      </div>
    </div>
  );
}

function SessionsView({ navigate }: { navigate: (href: string) => void }) {
  const sessions = useFetch<SessionsResponse>('/api/sessions', { sessions: [] });
  const elo = useFetch<EloResponse>('/api/elo', { rows: [], placeholder: true });

  return (
    <div className="grid gap-6 lg:grid-cols-[1.2fr_0.8fr]">
      <section className="rounded-[28px] border border-white/70 bg-panel/90 p-6 shadow-frame backdrop-blur">
        <SectionHeading
          kicker="Recent sessions"
          title="Cross-driver session ledger"
          meta={sessions.error ?? `${sessions.sessions.length} sessions`}
        />
        <div className="overflow-hidden rounded-[22px] border border-line">
          <table className="w-full border-collapse text-left text-sm">
            <thead className="bg-stone-900 text-stone-100">
              <tr>
                {['ts', 'driver', 'agent', 'ticket id', 'cost', 'success', 'ELO delta'].map((label) => (
                  <th key={label} className="px-4 py-3 font-medium">{label}</th>
                ))}
              </tr>
            </thead>
            <tbody className="divide-y divide-line bg-white/80">
              {sessions.sessions.map((item) => (
                <tr
                  key={item.session_id}
                  className="cursor-pointer transition hover:bg-stone-100"
                  onClick={() => navigate(`/session/${encodeURIComponent(item.session_id)}`)}
                >
                  <td className="px-4 py-3 font-mono text-xs">{formatTS(item.ts)}</td>
                  <td className="px-4 py-3">{dash(item.driver)}</td>
                  <td className="px-4 py-3">{dash(item.agent)}</td>
                  <td className="px-4 py-3 font-mono text-xs">{dash(item.ticket_id)}</td>
                  <td className="px-4 py-3">{formatUSD(item.cost_usd)}</td>
                  <td className="px-4 py-3">{successBadge(item.success)}</td>
                  <td className="px-4 py-3">{formatDelta(item.elo_delta)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
      <section className="rounded-[28px] border border-white/70 bg-stone-950 p-6 text-stone-100 shadow-frame">
        <SectionHeading
          kicker="Leaderboard"
          title="Swarm ELO"
          meta={elo.placeholder ? 'placeholder rows' : `${elo.rows.length} ranked lanes`}
          inverted
        />
        <div className="space-y-3">
          {elo.rows.map((row, index) => (
            <div key={`${row.driver}-${row.model}-${row.role}-${row.task_class}`} className="rounded-[22px] border border-white/10 bg-white/5 p-4">
              <div className="mb-2 flex items-center justify-between">
                <span className="font-mono text-xs uppercase tracking-[0.24em] text-stone-400">#{index + 1}</span>
                <span className="text-lg font-semibold">{row.elo_score.toFixed(1)}</span>
              </div>
              <div className="text-base font-medium">{row.driver} / {row.model}</div>
              <div className="mt-1 text-sm text-stone-400">
                {[dash(row.role), dash(row.task_class), `${row.dispatches_count} dispatches`].join(' · ')}
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}

function TimelineView({ sessionID, navigate }: { sessionID: string; navigate: (href: string) => void }) {
  const response = useFetch<TimelineResponse>(`/api/session/${encodeURIComponent(sessionID)}`, {});
  const [activeStepID, setActiveStepID] = useState<string | undefined>();
  const timeline = response.timeline;
  const activeStep = useMemo(
    () => timeline?.steps.find((step) => step.event_id === activeStepID) ?? timeline?.steps[0],
    [activeStepID, timeline],
  );

  if (!timeline) {
    return <ErrorPanel error={response.error ?? 'Loading timeline...'} onBack={() => navigate('/')} />;
  }

  return (
    <div className="grid gap-6 lg:grid-cols-[1.4fr_0.6fr]">
      <section className="rounded-[28px] border border-white/70 bg-panel/90 p-6 shadow-frame backdrop-blur">
        <div className="mb-6 flex items-start justify-between gap-4">
          <div>
            <button className="mb-3 text-sm text-muted underline underline-offset-4" onClick={() => navigate('/')} type="button">
              Back to sessions
            </button>
            <h2 className="text-3xl font-semibold tracking-[-0.03em]">{timeline.session_id}</h2>
            <p className="mt-2 text-sm text-muted">
              {formatTS(timeline.started_at)} to {formatTS(timeline.ended_at)} · {timeline.summary.tool_call_count} tool calls · {formatUSD(timeline.summary.total_cost_usd)}
            </p>
          </div>
          <div className="rounded-[22px] border border-line bg-white/80 px-4 py-3 text-sm">
            <div className="font-medium">{timeline.summary.total_tokens.toLocaleString()} tokens</div>
            <div className="text-muted">{timeline.steps.length} timeline steps</div>
          </div>
        </div>
        <div className="space-y-3">
          {timeline.steps.map((step, index) => (
            <button
              key={`${step.event_id ?? step.ts}-${index}`}
              className={`grid w-full grid-cols-[180px_minmax(0,1fr)_120px] items-center gap-4 rounded-[22px] border px-4 py-4 text-left transition ${railClass(step, activeStep?.event_id === step.event_id)}`}
              onMouseEnter={() => setActiveStepID(step.event_id)}
              onFocus={() => setActiveStepID(step.event_id)}
              type="button"
            >
              <div>
                <div className="font-mono text-xs uppercase tracking-[0.18em] text-muted">{formatTS(step.ts)}</div>
                <div className="mt-2 text-sm font-medium">{dash(step.agent || step.driver)}</div>
              </div>
              <div>
                <div className="text-base font-medium">{step.tool || step.type}</div>
                <div className="mt-1 text-sm text-muted">
                  {[step.type, step.decision?.rule_id, step.prediction?.routing_decision].filter(Boolean).join(' · ') || 'ledger event'}
                </div>
              </div>
              <div className="text-right text-sm">
                <div>{decisionLabel(step)}</div>
                <div className="mt-1 font-mono text-xs text-muted">{step.cost?.usd ? formatUSD(step.cost.usd) : dash(undefined)}</div>
              </div>
            </button>
          ))}
        </div>
      </section>
      <aside className="rounded-[28px] border border-stone-900/10 bg-stone-950 p-6 text-stone-100 shadow-frame">
        <SectionHeading kicker="Event detail" title={activeStep?.tool || activeStep?.type || 'Select an event'} inverted />
        {activeStep ? (
          <div className="space-y-4 text-sm">
            <DetailBlock title="Decision" body={activeStep.decision} />
            <DetailBlock title="Prompt / Input" body={activeStep.input} />
            <DetailBlock title="Thinking / Output" body={activeStep.output} />
            <DetailBlock title="Signals" body={activeStep.prediction} />
            <DetailBlock title="Cost" body={activeStep.cost} />
          </div>
        ) : null}
      </aside>
    </div>
  );
}

function PolicyView() {
  const policy = useFetch<PolicyResponse>('/api/policy', {});
  if (policy.error) {
    return <ErrorPanel error={policy.error} />;
  }
  return (
    <section className="rounded-[28px] border border-white/70 bg-panel/90 p-6 shadow-frame backdrop-blur">
      <SectionHeading kicker="Read-only" title="Resolved policy" meta={policy.path} />
      <pre className="overflow-x-auto rounded-[22px] bg-stone-950 p-6 font-mono text-sm text-stone-100">
        <code>{policy.body}</code>
      </pre>
    </section>
  );
}

function SectionHeading({ kicker, title, meta, inverted = false }: { kicker: string; title: string; meta?: string; inverted?: boolean }) {
  return (
    <div className="mb-5">
      <p className={`mb-2 text-xs uppercase tracking-[0.24em] ${inverted ? 'text-stone-400' : 'text-muted'}`}>{kicker}</p>
      <div className="flex items-end justify-between gap-4">
        <h2 className={`text-2xl font-semibold tracking-[-0.03em] ${inverted ? 'text-stone-100' : 'text-ink'}`}>{title}</h2>
        {meta ? <p className={`text-sm ${inverted ? 'text-stone-400' : 'text-muted'}`}>{meta}</p> : null}
      </div>
    </div>
  );
}

function DetailBlock({ title, body }: { title: string; body: unknown }) {
  return (
    <div className="rounded-[20px] border border-white/10 bg-white/5 p-4">
      <h3 className="mb-2 text-xs uppercase tracking-[0.2em] text-stone-400">{title}</h3>
      <pre className="overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs leading-6 text-stone-100">
        {stringify(body)}
      </pre>
    </div>
  );
}

function ErrorPanel({ error, onBack }: { error: string; onBack?: () => void }) {
  return (
    <section className="rounded-[28px] border border-red-200 bg-white p-8 shadow-frame">
      <h2 className="text-2xl font-semibold text-red-900">Dashboard unavailable</h2>
      <p className="mt-3 text-sm text-red-800">{error}</p>
      {onBack ? <button className={buttonClass(false) + ' mt-5'} onClick={onBack} type="button">Back</button> : null}
    </section>
  );
}

function useFetch<T extends Record<string, unknown>>(url: string, initial: T): T {
  const [state, setState] = useState<T>(initial);
  useEffect(() => {
    let active = true;
    fetch(url)
      .then((res) => res.json() as Promise<T>)
      .then((data) => {
        if (active) {
          setState(data);
        }
      })
      .catch((err: unknown) => {
        if (active) {
          setState({ ...initial, error: err instanceof Error ? err.message : 'request failed' });
        }
      });
    return () => {
      active = false;
    };
  }, [url]);
  return state;
}

function buttonClass(active: boolean) {
  return `rounded-full px-4 py-2 text-sm transition ${active ? 'bg-stone-900 text-stone-100' : 'bg-transparent text-ink hover:bg-stone-200'}`;
}

function railClass(step: TimelineStep, active: boolean) {
  const tone = step.prediction?.routing_decision
    ? 'border-signal/50 bg-signal/10'
    : step.decision?.allowed === false
      ? 'border-deny/50 bg-deny/10'
      : step.decision?.mode === 'guide'
        ? 'border-heuristic/50 bg-heuristic/10'
        : step.decision?.allowed
          ? 'border-allow/50 bg-allow/10'
          : 'border-line bg-white/80';
  return `${tone} ${active ? 'ring-2 ring-stone-900/10' : ''}`;
}

function decisionLabel(step: TimelineStep) {
  if (step.prediction?.routing_decision) {
    return <span className="inline-flex rounded-full bg-signal px-2 py-1 text-xs font-medium text-stone-900">router</span>;
  }
  if (step.decision?.allowed === false) {
    return <span className="inline-flex rounded-full bg-deny px-2 py-1 text-xs font-medium text-white">deny</span>;
  }
  if (step.decision?.mode === 'guide') {
    return <span className="inline-flex rounded-full bg-heuristic px-2 py-1 text-xs font-medium text-stone-900">heuristic</span>;
  }
  if (step.decision?.allowed) {
    return <span className="inline-flex rounded-full bg-allow px-2 py-1 text-xs font-medium text-stone-900">allow</span>;
  }
  return <span className="inline-flex rounded-full bg-stone-200 px-2 py-1 text-xs font-medium text-stone-700">event</span>;
}

function stringify(body: unknown) {
  if (body == null) {
    return '—';
  }
  if (typeof body === 'string') {
    return body;
  }
  return JSON.stringify(body, null, 2);
}

function formatTS(ts?: string) {
  if (!ts) {
    return '—';
  }
  return ts.replace('T', ' ').replace('Z', ' UTC');
}

function formatUSD(value?: number) {
  if (!value) {
    return '—';
  }
  return `$${value.toFixed(4)}`;
}

function formatDelta(value?: number | null) {
  if (value == null) {
    return '—';
  }
  return value > 0 ? `+${value.toFixed(1)}` : value.toFixed(1);
}

function dash(value?: string) {
  return value && value.length > 0 ? value : '—';
}

function successBadge(success?: boolean | null) {
  if (success == null) {
    return '—';
  }
  return success ? 'yes' : 'no';
}
