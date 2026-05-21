import type { Command } from 'commander';
import {
  askWorkspace,
  compileWorkspace,
  ingestWorkspace,
  lintWorkspace,
} from '../wiki.js';

export function registerWiki(program: Command): void {
  const wiki = program.command('wiki').description('Ingest, compile, lint, and query repository docs');

  wiki.command('ingest')
    .option('--workspace <dir>', 'workspace dir (default: cwd)')
    .action((opts: { workspace?: string }) => {
      const workspace = opts.workspace ?? process.cwd();
      const { paths, manifest, processed, skipped, deleted } = ingestWorkspace(workspace);
      console.log(`wiki ingest: ${manifest.sources.length} sources`);
      console.log(`processed: ${processed}`);
      console.log(`skipped: ${skipped}`);
      console.log(`deleted: ${deleted}`);
      console.log(`workspace: ${paths.wikiDir}`);
    });

  wiki.command('compile')
    .option('--workspace <dir>', 'workspace dir (default: cwd)')
    .action((opts: { workspace?: string }) => {
      const workspace = opts.workspace ?? process.cwd();
      const { paths, report } = compileWorkspace(workspace);
      console.log(`wiki compile: ${report.sourceCount} sources, ${report.sectionCount} sections`);
      console.log(`source bytes: ${report.sourceBytes}`);
      console.log(`compiled bytes: ${report.compiledBytes}`);
      console.log(`coverage ratio: ${report.coverageRatio.toFixed(2)}`);
      if (report.partialFailures.length > 0) {
        console.log('partial failures:');
        for (const failure of report.partialFailures) console.log(`- ${failure}`);
      }
      console.log(`compiled index: ${paths.compiledIndexPath}`);
    });

  wiki.command('lint')
    .option('--workspace <dir>', 'workspace dir (default: cwd)')
    .action((opts: { workspace?: string }) => {
      const workspace = opts.workspace ?? process.cwd();
      const { report } = lintWorkspace(workspace);
      console.log(`wiki lint: ${report.sourceCount} sources, ${report.sectionCount} sections`);
      console.log(`references: ${report.referenceCount}`);
      console.log(`broken references: ${report.brokenReferenceCount}`);
      console.log(`coverage: ${report.coveragePercent}%`);
      if (report.issues.length === 0) {
        console.log('issues: none');
        return;
      }
      for (const issue of report.issues) {
        console.log(`${issue.severity}: ${issue.code}: ${issue.message}`);
      }
      if (report.issues.some((issue) => issue.severity === 'error')) {
        process.exitCode = 1;
      }
    });

  wiki.command('ask <question>')
    .option('--workspace <dir>', 'workspace dir (default: cwd)')
    .action((question: string, opts: { workspace?: string }) => {
      const workspace = opts.workspace ?? process.cwd();
      const { result } = askWorkspace(workspace, question);
      if (!result) {
        console.log('No compiled knowledge base available. Run `chitin wiki ingest` and `chitin wiki compile` first.');
        return;
      }
      console.log(result.answer);
      if (result.citations.length > 0) {
        console.log('');
        console.log('Citations:');
        for (const citation of result.citations) console.log(`- ${citation}`);
      }
    });
}
