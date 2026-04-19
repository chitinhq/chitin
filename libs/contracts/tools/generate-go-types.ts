// Generates the Go struct for libs/contracts/src/event.schema.ts.
// Phase 1 scope: hand-emitted Go code (not a generic zod→Go converter).
// Regenerate with: pnpm exec nx run @chitin/contracts:generate-go-types
import { mkdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const HERE = dirname(fileURLToPath(import.meta.url));
const OUTPUT = resolve(HERE, '../../../go/execution-kernel/internal/event/event.go');

const GO_SOURCE = `// Code generated from libs/contracts/src/event.schema.ts — DO NOT EDIT.
// Regenerate with: pnpm exec nx run @chitin/contracts:generate-go-types
package event

import "time"

// ActionType is one of 6 canonical categories.
type ActionType string

const (
\tActionRead      ActionType = "read"
\tActionWrite     ActionType = "write"
\tActionExec      ActionType = "exec"
\tActionGit       ActionType = "git"
\tActionNet       ActionType = "net"
\tActionDangerous ActionType = "dangerous"
)

// Result is the decision outcome for an event.
type Result string

const (
\tResultSuccess Result = "success"
\tResultError   Result = "error"
\tResultDenied  Result = "denied"
)

// Event matches the zod EventSchema in libs/contracts/src/event.schema.ts.
type Event struct {
\tRunID         string         \`json:"run_id"\`
\tSessionID     string         \`json:"session_id"\`
\tSurface       string         \`json:"surface"\`
\tDriver        string         \`json:"driver"\`
\tAgentID       string         \`json:"agent_id"\`
\tToolName      string         \`json:"tool_name"\`
\tRawInput      map[string]any \`json:"raw_input"\`
\tCanonicalForm map[string]any \`json:"canonical_form"\`
\tActionType    ActionType     \`json:"action_type"\`
\tResult        Result         \`json:"result"\`
\tDurationMs    int64          \`json:"duration_ms"\`
\tError         *string        \`json:"error"\`
\tTS            time.Time      \`json:"ts"\`
\tMetadata      map[string]any \`json:"metadata"\`
}
`;

mkdirSync(dirname(OUTPUT), { recursive: true });
writeFileSync(OUTPUT, GO_SOURCE);
console.log(`Wrote ${OUTPUT}`);
