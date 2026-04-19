export interface OllamaUsage {
  input_tokens: number;
  output_tokens: number;
}

export interface ParsedOllamaResponse {
  text: string;
  usage: OllamaUsage;
  model: string;
  tsEnd: string;
}

/**
 * Parse an Ollama HTTP /api/generate JSON response (non-streaming).
 */
export function parseOllamaJSONResponse(body: string): ParsedOllamaResponse {
  let json: Record<string, unknown> = {};
  try {
    json = JSON.parse(body);
  } catch {
    return { text: '', usage: { input_tokens: 0, output_tokens: 0 }, model: '', tsEnd: '' };
  }
  return {
    text: typeof json.response === 'string' ? json.response : '',
    usage: {
      input_tokens: Number(json.prompt_eval_count ?? 0),
      output_tokens: Number(json.eval_count ?? 0),
    },
    model: typeof json.model === 'string' ? json.model : '',
    tsEnd: typeof json.created_at === 'string' ? json.created_at : '',
  };
}
