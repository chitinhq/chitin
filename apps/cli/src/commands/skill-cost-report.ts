import { Command } from 'commander';
import { printTable } from '../utils/print-table';
import { getSkillCostRollup } from 'libs/telemetry/src/skill-cost-rollup';
import { writeFileSync } from 'fs';

export const skillCostReport = new Command('skill-cost-report')
  .description('Report skill runtime cost and tool-call stats by skill/tier')
  .option('--skill <name>', 'Skill name to filter')
  .option('--since <duration>', 'Limit to events since duration (e.g. 7d, 24h)')
  .option('--tier <tier>', 'Tier to filter (T0, T1, T2, ...)')
  .option('--format <format>', 'Output format: text|json|markdown', 'text')
  .action(async (opts) => {
    const { skill, since, tier, format } = opts;
    const rollup = await getSkillCostRollup({ skill, since, tier });
    if (format === 'json') {
      console.log(JSON.stringify(rollup, null, 2));
    } else if (format === 'markdown') {
      // Simple markdown table output
      console.log('# Skill Cost Report\n');
      for (const entry of rollup) {
        console.log(`## ${entry.skill} (Tier ${entry.tier})`);
        console.log();
        console.log('| Metric | Value |');
        console.log('|--------|-------|');
        for (const [k, v] of Object.entries(entry.metrics)) {
          console.log(`| ${k} | ${v} |`);
        }
        if (entry.flags.length) {
          console.log('\n**Flags:** ' + entry.flags.join(', '));
        }
        console.log();
      }
    } else {
      // Text table output
      printTable(rollup.map(entry => ({
        Skill: entry.skill,
        Tier: entry.tier,
        'Dispatches': entry.metrics.dispatches,
        'Tokens (cached)': entry.metrics.tokens_cached,
        'Tokens (uncached)': entry.metrics.tokens_uncached,
        'Tool calls (mean)': entry.metrics.tool_calls_mean,
        'Tool calls (p95)': entry.metrics.tool_calls_p95,
        'Output size (med)': entry.metrics.output_size_median,
        'Output size (p95)': entry.metrics.output_size_p95,
        'Cache hit %': entry.metrics.cache_hit_rate,
        'Cost (USD)': entry.metrics.cost_usd,
        'Advisor consults': entry.metrics.advisor_consult_rate,
        'Flags': entry.flags.join(', ')
      })));
    }
  });
