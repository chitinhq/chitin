#!/usr/bin/env node
import { Command } from 'commander';
import { eventsListCommand } from './commands/events-list.js';
import { eventsTailCommand } from './commands/events-tail.js';
import { replayCommand } from './commands/replay.js';
import { initClaudeCodeCommand } from './commands/init-claude-code.js';
import { registerRun } from './commands/run.js';
import { registerEventsTree } from './commands/events-tree.js';

const program = new Command();
program.name('chitin').description('Observability-first substrate for AI coding agents');

const init = program.command('init').description('Wire a surface');
init.command('claude-code')
  .description('Wire the PreToolUse hook for Claude Code')
  .option('--workspace <dir>', 'workspace dir (default: cwd)')
  .action((opts) => initClaudeCodeCommand(opts));

const events = program.command('events').description('Inspect captured events');
events.command('list')
  .option('--workspace <dir>', 'workspace dir (default: cwd)')
  .option('--surface <s>', 'filter by surface')
  .option('--run <id>', 'filter by run_id')
  .option('--action <t>', 'filter by action_type')
  .option('--limit <n>', 'max rows', (v) => parseInt(v, 10))
  .action((opts) => eventsListCommand(opts));

events.command('tail')
  .option('--workspace <dir>', 'workspace dir (default: cwd)')
  .option('--surface <s>', 'filter by surface')
  .action((opts) => eventsTailCommand(opts));

program.command('replay <run_id>')
  .option('--workspace <dir>', 'workspace dir (default: cwd)')
  .action((runId, opts) => replayCommand(runId, opts));

registerRun(program);
registerEventsTree(program);

program.parseAsync(process.argv).catch((err) => {
  console.error(err);
  process.exit(1);
});
