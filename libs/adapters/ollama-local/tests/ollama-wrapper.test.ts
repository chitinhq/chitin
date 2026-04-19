import { describe, expect, it } from 'vitest';
import { parseOllamaJSONResponse } from '../src/ollama-wrapper';

describe('parseOllamaJSONResponse', () => {
  it('extracts text + usage from a non-streaming generate response', () => {
    const body = JSON.stringify({
      model: 'llama3',
      created_at: '2026-04-19T12:00:01Z',
      response: 'hi back',
      done: true,
      prompt_eval_count: 10,
      eval_count: 5,
      total_duration: 1200000000,
    });
    const got = parseOllamaJSONResponse(body);
    expect(got.text).toBe('hi back');
    expect(got.usage.input_tokens).toBe(10);
    expect(got.usage.output_tokens).toBe(5);
    expect(got.model).toBe('llama3');
    expect(got.tsEnd).toBe('2026-04-19T12:00:01Z');
  });

  it('returns empty on missing fields', () => {
    const got = parseOllamaJSONResponse('{}');
    expect(got.text).toBe('');
    expect(got.usage.input_tokens).toBe(0);
  });
});
