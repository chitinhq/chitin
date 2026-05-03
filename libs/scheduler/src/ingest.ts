import { ItemSchema } from './schema.js';
import type { Item, ItemDecision } from './schema.js';

export interface AnthropicMessage {
  role: 'user' | 'assistant';
  content: string;
}

export interface AnthropicClient {
  createMessage(opts: {
    model: string;
    max_tokens: number;
    system: string;
    messages: AnthropicMessage[];
  }): Promise<{ content: Array<{ type: string; text: string }> }>;
}

export interface IngestTelemetry {
  parse_failure?: { reason: string; raw_response?: string };
  item_count: number;
  model: string;
  ts: string;
  event_type: 'ingest_result';
}

export interface IngestResult {
  items: Item[];
  telemetry: IngestTelemetry;
  decisions: ItemDecision[];
}

const FEW_SHOT_EXAMPLES = `
Example 1 — task with deadline:
Input: "Submit the quarterly report by Friday"
Output: [{"item_type":"task","id":"<uuid>","title":"Submit the quarterly report","status":"open","created_at":"<now>","deadline":"<this-friday>","priority":2}]

Example 2 — calendar event:
Input: "Team standup tomorrow at 9am for 30 minutes"
Output: [{"item_type":"event","id":"<uuid>","title":"Team standup","status":"open","created_at":"<now>","start":"<tomorrow-9am>","duration_min":30}]

Example 3 — backlog entry:
Input: "Add dark mode to the dashboard — T2 priority, touches apps/dashboard"
Output: [{"item_type":"backlog","id":"<uuid>","title":"Add dark mode to the dashboard","status":"open","created_at":"<now>","tier":"T2","file_scope":["apps/dashboard/**"]}]

Example 4 — multiple items:
Input: "Fix the login bug (urgent, due tomorrow) and refactor the auth module next week"
Output: [{"item_type":"task","id":"<uuid>","title":"Fix the login bug","status":"open","created_at":"<now>","deadline":"<tomorrow>","priority":1},{"item_type":"task","id":"<uuid>","title":"Refactor the auth module","status":"open","created_at":"<now>","deadline":"<next-week>","priority":3}]
`.trim();

const SYSTEM_PROMPT = `You are a scheduling assistant that parses natural language into structured items.

Parse the user's input into a JSON array of Item objects. Each item must be one of:
- task: work to be done (has title, optional deadline, priority 1-5, est_min, window_pref)
- event: calendar event (has title, start datetime, optional duration_min)
- backlog: development backlog entry (has title, optional tier T0-T5, file_scope)

All items require: id (UUID v4), title, status ("open"), created_at (RFC3339 now).

Window preferences for tasks: morning, deep, shallow, evening, any.

Return ONLY a valid JSON array. No explanation, no markdown, no code blocks.
If you cannot parse anything useful, return [].

${FEW_SHOT_EXAMPLES}`;

function defaultClient(apiKey: string): AnthropicClient {
  return {
    async createMessage(opts) {
      const res = await fetch('https://api.anthropic.com/v1/messages', {
        method: 'POST',
        headers: {
          'x-api-key': apiKey,
          'anthropic-version': '2023-06-01',
          'content-type': 'application/json',
        },
        body: JSON.stringify({
          model: opts.model,
          max_tokens: opts.max_tokens,
          system: opts.system,
          messages: opts.messages,
        }),
      });
      if (!res.ok) {
        const text = await res.text();
        throw new Error(`Anthropic API error ${res.status}: ${text}`);
      }
      return res.json() as Promise<{ content: Array<{ type: string; text: string }> }>;
    },
  };
}

export async function ingest(
  text: string,
  opts?: {
    now?: string;
    preferred_model?: string;
    client?: AnthropicClient;
  },
): Promise<IngestResult> {
  const now = opts?.now ?? new Date().toISOString();
  const model = opts?.preferred_model ?? 'claude-opus-4-7';
  const ts = now;

  const client =
    opts?.client ??
    defaultClient(process.env['ANTHROPIC_API_KEY'] ?? '');

  let rawResponse = '';
  try {
    const response = await client.createMessage({
      model,
      max_tokens: 2048,
      system: SYSTEM_PROMPT.replace(/<now>/g, now),
      messages: [{ role: 'user', content: text }],
    });

    const textBlock = response.content.find((b) => b.type === 'text');
    rawResponse = textBlock?.text ?? '';

    const parsed = JSON.parse(rawResponse) as unknown[];
    const items: Item[] = [];
    for (const raw of parsed) {
      const result = ItemSchema.safeParse(raw);
      if (result.success) items.push(result.data);
    }

    const decisions = items.map((item) => ({
      event_type: 'item_decision' as const,
      item_id: item.id,
      rationale: `ingested:${item.item_type}`,
      ts,
    }));

    return {
      items,
      telemetry: { event_type: 'ingest_result', item_count: items.length, model, ts },
      decisions,
    };
  } catch (err) {
    const reason = err instanceof Error ? err.message : String(err);
    return {
      items: [],
      telemetry: {
        event_type: 'ingest_result',
        item_count: 0,
        model,
        ts,
        parse_failure: { reason, raw_response: rawResponse },
      },
      decisions: [],
    };
  }
}
