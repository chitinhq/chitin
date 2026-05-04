import { Command } from 'commander';
import { printSkillCostReport } from '../../libs/telemetry/src/skillCostReport';

export const skillCostReportCommand = new Command('skill-cost-report')
  .description('Report per-skill runtime cost and tool-call stats')
  .option('--skill <name>', 'Skill name to report on')
  .option('--since <duration>', 'Limit to events since duration (e.g. 7d, 24h)')
  .option('--tier <tier>', 'Limit to a specific tier (T0, T1, T2, etc)')
  .option('--format <format>', 'Output format: text|json|markdown', 'text')
  .action(async (opts) => {
    await printSkillCostReport({
      skill: opts.skill,
      since: opts.since,
      tier: opts.tier,
      format: opts.format,
    });
  });
