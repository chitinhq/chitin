#!/usr/bin/env node
import { Command } from 'commander';
import { registerGuard } from './commands/guard.js';
import { registerStatus } from './commands/status.js';

const program = new Command();
program.name('chitin').description('Observability-first substrate for AI coding agents');

const init = program.command('init').description('Wire a surface');
init.command('claude-code')
  .description('Wire the PreToolUse hook for Claude Code')
  .option('--workspace <dir>', 'workspace dir (default: cwd)')
  .action(async (opts) => {
    const { initClaudeCodeCommand } = await import('./commands/init-claude-code.js');
    return initClaudeCodeCommand(opts);
  });

const events = program.command('events').description('Inspect captured events');
events.command('list')
  .option('--workspace <dir>', 'workspace dir (default: cwd)')
  .option('--surface <s>', 'filter by surface')
  .option('--run <id>', 'filter by run_id')
  .option('--limit <n>', 'max rows', (v) => parseInt(v, 10))
  .action(async (opts) => {
    const { eventsListCommand } = await import('./commands/events-list.js');
    return eventsListCommand(opts);
  });

events.command('tail')
  .option('--workspace <dir>', 'workspace dir (default: cwd)')
  .option('--surface <s>', 'filter by surface')
  .action(async (opts) => {
    const { eventsTailCommand } = await import('./commands/events-tail.js');
    return eventsTailCommand(opts);
  });

program.command('replay <session_id>')
  .option('--workspace <dir>', 'workspace dir (default: cwd)')
  .action(async (sessionId, opts) => {
    const { replayCommand } = await import('./commands/replay.js');
    return replayCommand(sessionId, opts);
  });

{
  const { registerRun } = await import('./commands/run.js');
  registerRun(program);
}
{
  const { registerEventsTree } = await import('./commands/events-tree.js');
  registerEventsTree(events);
}
{
  const { registerInstall } = await import('./commands/install.js');
  registerInstall(program);
}
{
  const { registerHealth } = await import('./commands/health.js');
  registerHealth(program);
}
{
  const { registerLedger } = await import('./commands/ledger.js');
  registerLedger(program);
}
{
  const { registerReview } = await import('./commands/review.js');
  registerReview(program);
}
registerGuard(program);
registerStatus(program);

program.parseAsync(process.argv).catch((err) => {
  console.error(err);
  process.exit(1);
});
