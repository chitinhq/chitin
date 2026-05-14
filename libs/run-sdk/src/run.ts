import {
  EventSchema,
  type Event,
  hashEvent,
  type RunManifest,
  type SessionEndPayload,
} from '@chitin/contracts';

type EventVariant<K extends Event['event_type']> = Extract<Event, { event_type: K }>;

export type EmitEventInput = {
  [K in Event['event_type']]: {
    eventType: K;
    payload: EventVariant<K>['payload'];
    chainId?: string;
    chainType?: EventVariant<K>['chain_type'];
    parentChainId?: string | null;
    ts?: string;
    labels?: Record<string, string>;
  };
}[Event['event_type']];

interface ChainCursor {
  seq: number;
  thisHash: string;
}

export class Run {
  readonly manifest: RunManifest;
  private readonly eventsInternal: Event[] = [];
  private readonly chainState = new Map<string, ChainCursor>();
  private closed = false;

  constructor(manifest: RunManifest) {
    this.manifest = manifest;
  }

  emitEvent(input: EmitEventInput): Event {
    // A session_end closes the run — the kernel emitter rejects any event
    // appended after the terminal tail, so the SDK enforces the same.
    if (this.closed) {
      throw new Error('run is finalized; no further events may be emitted');
    }
    const chainId = input.chainId ?? this.manifest.session_id;
    const chainType = input.chainType ?? 'session';
    const cursor = this.chainState.get(chainId);

    const candidate = {
      schema_version: this.manifest.schema_version,
      run_id: this.manifest.run_id,
      session_id: this.manifest.session_id,
      surface: this.manifest.surface,
      driver_identity: this.manifest.driver_identity,
      agent_instance_id: this.manifest.agent_instance_id,
      parent_agent_id: this.manifest.parent_agent_id,
      agent_fingerprint: this.manifest.agent_fingerprint,
      event_type: input.eventType,
      chain_id: chainId,
      chain_type: chainType,
      parent_chain_id: input.parentChainId ?? (chainType === 'tool_call' ? this.manifest.session_id : null),
      seq: cursor ? cursor.seq + 1 : 0,
      prev_hash: cursor ? cursor.thisHash : null,
      this_hash: '',
      ts: input.ts ?? new Date().toISOString(),
      labels: { ...this.manifest.labels, ...(input.labels ?? {}) },
      payload: input.payload,
    };

    // Parse first, then hash the schema-normalized event. EventSchema
    // strips unknown payload keys — hashing the raw candidate would bake
    // stripped keys into this_hash, so the stored event wouldn't verify.
    // Parse needs a schema-valid this_hash, so use a placeholder, then
    // hash with this_hash blanked (the contract hashEvent expects).
    const normalized = EventSchema.parse({ ...candidate, this_hash: '0'.repeat(64) });
    const event: Event = {
      ...normalized,
      this_hash: hashEvent({ ...normalized, this_hash: '' }),
    };
    // Freeze before storing + returning: the same object is the internal
    // record and the caller's handle; a mutation must not be able to make
    // toJSONL() diverge from this_hash.
    Object.freeze(event);
    Object.freeze(event.payload);
    Object.freeze(event.labels);

    this.chainState.set(chainId, { seq: event.seq, thisHash: event.this_hash });
    this.eventsInternal.push(event);
    return event;
  }

  finalize(payload: SessionEndPayload, input: Omit<EmitEventInput, 'eventType' | 'payload'> = {}): Event {
    const event = this.emitEvent({
      ...input,
      eventType: 'session_end',
      payload,
    });
    this.closed = true;
    return event;
  }

  get events(): readonly Event[] {
    return this.eventsInternal;
  }

  toJSONL(): string {
    return this.eventsInternal.map((event) => JSON.stringify(event)).join('\n');
  }
}

export function createRun(manifest: RunManifest): Run {
  return new Run(manifest);
}
