import {
  existsSync,
  mkdirSync,
  openSync,
  closeSync,
  readFileSync,
  readdirSync,
  renameSync,
  rmSync,
  statSync,
  writeFileSync,
} from 'node:fs';
import { createHash } from 'node:crypto';
import { dirname, join, relative, resolve, sep } from 'node:path';

export interface WikiConfig {
  sources: string[];
  staleDays: number;
}

export interface WikiPaths {
  chitinDir: string;
  wikiDir: string;
  rawDir: string;
  compiledDir: string;
  manifestPath: string;
  compiledIndexPath: string;
  compiledMarkdownPath: string;
  lockPath: string;
}

export interface RawSection {
  id: string;
  level: number;
  title: string;
  anchor: string;
  headingPath: string[];
  content: string;
}

export interface RawDocument {
  sourcePath: string;
  absolutePath: string;
  hash: string;
  bytes: number;
  mtimeMs: number;
  ingestedAt: string;
  sections: RawSection[];
}

export interface IngestedSource {
  sourcePath: string;
  hash: string;
  bytes: number;
  mtimeMs: number;
  rawFile: string;
  sectionCount: number;
  ingestedAt: string;
}

export interface IngestManifest {
  generatedAt: string;
  workspaceRoot: string;
  config: WikiConfig;
  sources: IngestedSource[];
}

export interface CompiledReference {
  text: string;
  target: string;
}

export interface CompiledSection {
  id: string;
  sourcePath: string;
  sourceTitle: string;
  title: string;
  anchor: string;
  headingPath: string[];
  content: string;
  references: CompiledReference[];
  text: string;
  tokenCount: number;
}

export interface CompileReport {
  generatedAt: string;
  sourceCount: number;
  sectionCount: number;
  sourceBytes: number;
  compiledBytes: number;
  coverageRatio: number;
  partialFailures: string[];
  sections: CompiledSection[];
}

export interface LintIssue {
  severity: 'warn' | 'error';
  code: 'broken-reference' | 'stale-source' | 'low-coverage';
  message: string;
  sourcePath?: string;
  referenceTarget?: string;
}

export interface LintReport {
  sourceCount: number;
  sectionCount: number;
  referenceCount: number;
  brokenReferenceCount: number;
  coveragePercent: number;
  compiledBytes: number;
  sourceBytes: number;
  issues: LintIssue[];
}

export interface AskResult {
  answer: string;
  citations: string[];
  matches: Array<{ section: CompiledSection; score: number }>;
}

const DEFAULT_CONFIG: WikiConfig = {
  sources: ['docs', '.specify/specs', 'docs/decisions'],
  staleDays: 90,
};

const SOURCE_EXTENSIONS = new Set(['.md', '.mdx']);
const STOP_WORDS = new Set([
  'a', 'an', 'and', 'are', 'as', 'at', 'be', 'by', 'for', 'from', 'how', 'in', 'is', 'it',
  'of', 'on', 'or', 'that', 'the', 'this', 'to', 'what', 'when', 'where', 'which', 'with',
]);

export function resolveWikiPaths(workspace: string): WikiPaths {
  const chitinDir = join(resolve(workspace), '.chitin');
  const wikiDir = join(chitinDir, 'wiki');
  return {
    chitinDir,
    wikiDir,
    rawDir: join(wikiDir, 'raw'),
    compiledDir: join(wikiDir, 'compiled'),
    manifestPath: join(wikiDir, 'ingest-manifest.json'),
    compiledIndexPath: join(wikiDir, 'compiled', 'index.json'),
    compiledMarkdownPath: join(wikiDir, 'compiled', 'knowledge-base.md'),
    lockPath: join(wikiDir, '.lock'),
  };
}

export function loadWikiConfig(workspace: string): WikiConfig {
  const configPath = join(resolve(workspace), 'chitin.wiki.json');
  if (!existsSync(configPath)) return DEFAULT_CONFIG;
  const parsed = JSON.parse(readFileSync(configPath, 'utf8')) as Partial<WikiConfig>;
  return {
    sources: Array.isArray(parsed.sources) && parsed.sources.length > 0
      ? parsed.sources.map((s) => String(s))
      : DEFAULT_CONFIG.sources,
    staleDays: typeof parsed.staleDays === 'number' && parsed.staleDays > 0
      ? parsed.staleDays
      : DEFAULT_CONFIG.staleDays,
  };
}

export function ingestWorkspace(workspace: string): {
  paths: WikiPaths;
  manifest: IngestManifest;
  processed: number;
  skipped: number;
  deleted: number;
} {
  const absWorkspace = resolve(workspace);
  const config = loadWikiConfig(absWorkspace);
  const paths = resolveWikiPaths(absWorkspace);
  return withWikiLock(paths, () => {
    ensureDir(paths.rawDir);
    ensureDir(paths.compiledDir);

    const previous = readJsonIfExists<IngestManifest>(paths.manifestPath);
    const previousBySource = new Map(previous?.sources.map((s) => [s.sourcePath, s]) ?? []);
    const currentSources = findSourceFiles(absWorkspace, config.sources);
    const nextSources: IngestedSource[] = [];
    let processed = 0;
    let skipped = 0;

    for (const file of currentSources) {
      const st = statSync(file);
      const sourcePath = normalizePath(relative(absWorkspace, file));
      const prev = previousBySource.get(sourcePath);
      const hash = sha256(readFileSync(file));
      if (prev && prev.hash === hash && prev.bytes === st.size && prev.mtimeMs === st.mtimeMs) {
        nextSources.push(prev);
        skipped += 1;
        continue;
      }

      const raw = parseMarkdownDocument(absWorkspace, file, hash, st.size, st.mtimeMs);
      const rawFile = fileNameForSource(sourcePath);
      writeJsonAtomic(join(paths.rawDir, rawFile), raw);
      nextSources.push({
        sourcePath,
        hash,
        bytes: st.size,
        mtimeMs: st.mtimeMs,
        rawFile,
        sectionCount: raw.sections.length,
        ingestedAt: raw.ingestedAt,
      });
      processed += 1;
    }

    const nextSet = new Set(nextSources.map((s) => s.sourcePath));
    const deletedSources = (previous?.sources ?? []).filter((s) => !nextSet.has(s.sourcePath));
    for (const source of deletedSources) {
      rmSync(join(paths.rawDir, source.rawFile), { force: true });
    }

    const manifest: IngestManifest = {
      generatedAt: new Date().toISOString(),
      workspaceRoot: absWorkspace,
      config,
      sources: nextSources.sort((a, b) => a.sourcePath.localeCompare(b.sourcePath)),
    };
    writeJsonAtomic(paths.manifestPath, manifest);

    return { paths, manifest, processed, skipped, deleted: deletedSources.length };
  });
}

export function compileWorkspace(workspace: string): { paths: WikiPaths; report: CompileReport } {
  const absWorkspace = resolve(workspace);
  const paths = resolveWikiPaths(absWorkspace);
  return withWikiLock(paths, () => {
    const manifest = readJsonIfExists<IngestManifest>(paths.manifestPath);
    if (!manifest || manifest.sources.length === 0) {
      const empty: CompileReport = {
        generatedAt: new Date().toISOString(),
        sourceCount: 0,
        sectionCount: 0,
        sourceBytes: 0,
        compiledBytes: 0,
        coverageRatio: 0,
        partialFailures: [],
        sections: [],
      };
      ensureDir(paths.compiledDir);
      writeJsonAtomic(paths.compiledIndexPath, empty);
      writeFileAtomic(paths.compiledMarkdownPath, '');
      return { paths, report: empty };
    }

    const partialFailures: string[] = [];
    const sections: CompiledSection[] = [];
    const docs: string[] = [];
    let sourceBytes = 0;

    for (const source of manifest.sources) {
      sourceBytes += source.bytes;
      try {
        const raw = readJson<RawDocument>(join(paths.rawDir, source.rawFile));
        const title = raw.sections[0]?.title || raw.sourcePath;
        docs.push(`<!-- source: ${raw.sourcePath} -->`);
        for (const section of raw.sections) {
          const references = extractReferences(section.content);
          const text = `${section.title}\n${section.content}`.trim();
          const compiled: CompiledSection = {
            id: section.id,
            sourcePath: raw.sourcePath,
            sourceTitle: title,
            title: section.title,
            anchor: section.anchor,
            headingPath: section.headingPath,
            content: section.content,
            references,
            text,
            tokenCount: tokenize(text).length,
          };
          sections.push(compiled);
          docs.push(renderCompiledSection(compiled));
        }
      } catch (err) {
        partialFailures.push(`${source.sourcePath}: ${String(err)}`);
      }
    }

    const markdown = docs.join('\n\n').trim();
    const compiledBytes = Buffer.byteLength(markdown, 'utf8');
    const report: CompileReport = {
      generatedAt: new Date().toISOString(),
      sourceCount: manifest.sources.length,
      sectionCount: sections.length,
      sourceBytes,
      compiledBytes,
      coverageRatio: sourceBytes === 0 ? 0 : compiledBytes / sourceBytes,
      partialFailures,
      sections,
    };
    ensureDir(paths.compiledDir);
    writeJsonAtomic(paths.compiledIndexPath, report);
    writeFileAtomic(paths.compiledMarkdownPath, markdown);
    return { paths, report };
  });
}

export function lintWorkspace(workspace: string): { paths: WikiPaths; report: LintReport } {
  const absWorkspace = resolve(workspace);
  const paths = resolveWikiPaths(absWorkspace);
  const manifest = readJsonIfExists<IngestManifest>(paths.manifestPath);
  const compiled = readJsonIfExists<CompileReport>(paths.compiledIndexPath);
  const issues: LintIssue[] = [];
  const sections = compiled?.sections ?? [];
  const anchors = new Set(sections.map((s) => citationForSection(s)));
  const pathsOnly = new Set(sections.map((s) => s.sourcePath));
  let referenceCount = 0;
  let brokenReferenceCount = 0;

  for (const section of sections) {
    for (const ref of section.references) {
      referenceCount += 1;
      if (isExternalReference(ref.target)) continue;
      const normalized = normalizeReferenceTarget(section.sourcePath, ref.target);
      if (!anchors.has(normalized) && !pathsOnly.has(normalized)) {
        brokenReferenceCount += 1;
        issues.push({
          severity: 'error',
          code: 'broken-reference',
          message: `Broken reference ${ref.target} from ${section.sourcePath}`,
          sourcePath: section.sourcePath,
          referenceTarget: ref.target,
        });
      }
    }
  }

  const staleCutoff = Date.now() - (manifest?.config.staleDays ?? DEFAULT_CONFIG.staleDays) * 24 * 60 * 60 * 1000;
  for (const source of manifest?.sources ?? []) {
    if (source.mtimeMs < staleCutoff) {
      issues.push({
        severity: 'warn',
        code: 'stale-source',
        message: `Potentially stale source older than ${(manifest?.config.staleDays ?? DEFAULT_CONFIG.staleDays)} days`,
        sourcePath: source.sourcePath,
      });
    }
  }

  const sourceBytes = compiled?.sourceBytes ?? 0;
  const compiledBytes = compiled?.compiledBytes ?? 0;
  const coveragePercent = sourceBytes === 0 ? 0 : Math.round((compiledBytes / sourceBytes) * 100);
  if (sourceBytes > 0 && compiledBytes / sourceBytes < 0.5) {
    issues.push({
      severity: 'warn',
      code: 'low-coverage',
      message: `Compiled KB is only ${coveragePercent}% of source bytes`,
    });
  }

  return {
    paths,
    report: {
      sourceCount: manifest?.sources.length ?? 0,
      sectionCount: sections.length,
      referenceCount,
      brokenReferenceCount,
      coveragePercent,
      compiledBytes,
      sourceBytes,
      issues,
    },
  };
}

export function askWorkspace(workspace: string, question: string): { paths: WikiPaths; result: AskResult | null } {
  const absWorkspace = resolve(workspace);
  const paths = resolveWikiPaths(absWorkspace);
  const compiled = readJsonIfExists<CompileReport>(paths.compiledIndexPath);
  if (!compiled || compiled.sections.length === 0) return { paths, result: null };

  const queryTerms = tokenize(question);
  const matches = compiled.sections
    .map((section) => ({ section, score: scoreSection(section, queryTerms) }))
    .filter((m) => m.score > 0)
    .sort((a, b) => b.score - a.score || a.section.sourcePath.localeCompare(b.section.sourcePath))
    .slice(0, 3);

  if (matches.length === 0) {
    return {
      paths,
      result: {
        answer: 'No relevant compiled wiki sections matched that question.',
        citations: [],
        matches: [],
      },
    };
  }

  const answerLines = matches.map(({ section }) => summarizeSection(section, queryTerms));
  const citations = matches.map(({ section }) => citationForSection(section));
  return {
    paths,
    result: {
      answer: answerLines.join('\n'),
      citations,
      matches,
    },
  };
}

export function parseMarkdownDocument(
  workspaceRoot: string,
  absolutePath: string,
  hash: string,
  bytes: number,
  mtimeMs: number,
): RawDocument {
  const content = readFileSync(absolutePath, 'utf8');
  return {
    sourcePath: normalizePath(relative(workspaceRoot, absolutePath)),
    absolutePath,
    hash,
    bytes,
    mtimeMs,
    ingestedAt: new Date().toISOString(),
    sections: splitMarkdownIntoSections(normalizeLineEndings(content), absolutePath),
  };
}

export function splitMarkdownIntoSections(content: string, absolutePath = 'doc.md'): RawSection[] {
  const lines = content.split('\n');
  const sections: RawSection[] = [];
  const headingStack: Array<{ level: number; title: string }> = [];
  let currentTitle = relativeTitleFromPath(absolutePath);
  let currentLevel = 1;
  let currentBody: string[] = [];
  let inCodeFence = false;
  let sawHeading = false;

  const pushSection = () => {
    const body = currentBody.join('\n').trim();
    if (!sawHeading && !body) return;
    if (!sawHeading && body) {
      sections.push({
        id: `${slugify(normalizePath(absolutePath))}#${slugify(currentTitle)}`,
        level: currentLevel,
        title: currentTitle,
        anchor: slugify(currentTitle),
        headingPath: [],
        content: body,
      });
      return;
    }
    if (!body && sections.length > 0) return;
    const headingPath = headingStack.map((h) => h.title);
    const title = headingPath.at(-1) ?? currentTitle;
    sections.push({
      id: `${slugify(normalizePath(absolutePath))}#${slugify(headingPath.join('-') || title)}`,
      level: currentLevel,
      title,
      anchor: slugify(title),
      headingPath,
      content: body,
    });
  };

  for (const line of lines) {
    if (/^\s*```/.test(line)) inCodeFence = !inCodeFence;
    const heading = !inCodeFence ? /^(#{1,6})\s+(.*)$/.exec(line) : null;
    if (heading) {
      pushSection();
      sawHeading = true;
      currentLevel = heading[1].length;
      currentTitle = heading[2].trim();
      while (headingStack.length > 0 && headingStack[headingStack.length - 1].level >= currentLevel) {
        headingStack.pop();
      }
      headingStack.push({ level: currentLevel, title: currentTitle });
      currentBody = [];
      continue;
    }
    currentBody.push(line);
  }

  pushSection();
  return sections.filter((section) => section.title.length > 0);
}

function renderCompiledSection(section: CompiledSection): string {
  const headingLevel = Math.min(section.headingPath.length + 1, 6);
  return [
    `${'#'.repeat(headingLevel)} ${section.title}`,
    `Source: ${citationForSection(section)}`,
    section.content,
  ].join('\n');
}

function summarizeSection(section: CompiledSection, queryTerms: string[]): string {
  const sentences = section.text
    .split(/(?<=[.!?])\s+/)
    .map((s) => s.trim())
    .filter(Boolean);
  const ranked = sentences
    .map((sentence) => ({
      sentence,
      score: tokenize(sentence).reduce((acc, term) => acc + (queryTerms.includes(term) ? 1 : 0), 0),
    }))
    .sort((a, b) => b.score - a.score || b.sentence.length - a.sentence.length);
  const best = ranked[0]?.sentence ?? section.text.slice(0, 280);
  return `- ${best} [${citationForSection(section)}]`;
}

function scoreSection(section: CompiledSection, queryTerms: string[]): number {
  if (queryTerms.length === 0) return 0;
  const titleTerms = tokenize(section.title);
  const headingTerms = tokenize(section.headingPath.join(' '));
  const bodyTerms = tokenize(section.content);
  let score = 0;
  for (const term of queryTerms) {
    if (titleTerms.includes(term)) score += 8;
    if (headingTerms.includes(term)) score += 5;
    if (bodyTerms.includes(term)) score += 1;
  }
  return score;
}

function extractReferences(content: string): CompiledReference[] {
  const refs: CompiledReference[] = [];
  const re = /\[([^\]]+)\]\(([^)]+)\)/g;
  for (const match of content.matchAll(re)) {
    refs.push({ text: match[1], target: match[2] });
  }
  return refs;
}

function tokenize(text: string): string[] {
  return text
    .toLowerCase()
    .split(/[^a-z0-9]+/g)
    .filter((token) => token.length > 1 && !STOP_WORDS.has(token));
}

function normalizeReferenceTarget(sourcePath: string, target: string): string {
  if (target.startsWith('#')) return `${sourcePath}${target}`;
  const [filePart, anchorPart] = target.split('#', 2);
  const resolved = normalizePath(join(dirname(sourcePath), filePart));
  return anchorPart ? `${resolved}#${anchorPart}` : resolved;
}

function citationForSection(section: CompiledSection): string {
  return `${section.sourcePath}#${section.anchor}`;
}

function isExternalReference(target: string): boolean {
  return /^[a-z]+:\/\//i.test(target) || target.startsWith('mailto:');
}

function relativeTitleFromPath(path: string): string {
  const base = path.split(sep).at(-1) ?? path;
  return base.replace(/\.(md|mdx)$/i, '');
}

function findSourceFiles(workspace: string, configured: string[]): string[] {
  const files = new Set<string>();
  for (const source of configured) {
    const abs = resolve(workspace, source);
    walkMarkdownFiles(abs, files);
  }
  return [...files].sort();
}

function walkMarkdownFiles(path: string, files: Set<string>): void {
  if (!existsSync(path)) return;
  const stat = statSync(path);
  if (stat.isFile()) {
    if (hasMarkdownExtension(path)) files.add(path);
    return;
  }
  if (!stat.isDirectory()) return;
  for (const entry of readdirSync(path, { withFileTypes: true })) {
    if (entry.name.startsWith('.git')) continue;
    walkMarkdownFiles(join(path, entry.name), files);
  }
}

function hasMarkdownExtension(path: string): boolean {
  const lower = path.toLowerCase();
  return [...SOURCE_EXTENSIONS].some((ext) => lower.endsWith(ext));
}

function fileNameForSource(sourcePath: string): string {
  return `${slugify(sourcePath)}-${sha256(sourcePath).slice(0, 12)}.json`;
}

function slugify(value: string): string {
  return value
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    || 'root';
}

function normalizePath(path: string): string {
  return path.split(sep).join('/');
}

function normalizeLineEndings(content: string): string {
  return content.replace(/\r\n/g, '\n');
}

function sha256(value: string | Uint8Array): string {
  return createHash('sha256').update(value).digest('hex');
}

function ensureDir(path: string): void {
  mkdirSync(path, { recursive: true });
}

function withWikiLock<T>(paths: WikiPaths, fn: () => T): T {
  ensureDir(paths.wikiDir);
  let fd: number | null = null;
  try {
    fd = openSync(paths.lockPath, 'wx');
    return fn();
  } finally {
    if (fd !== null) closeSync(fd);
    rmSync(paths.lockPath, { force: true });
  }
}

function readJsonIfExists<T>(path: string): T | null {
  if (!existsSync(path)) return null;
  return readJson<T>(path);
}

function readJson<T>(path: string): T {
  return JSON.parse(readFileSync(path, 'utf8')) as T;
}

function writeJsonAtomic(path: string, value: unknown): void {
  writeFileAtomic(path, `${JSON.stringify(value, null, 2)}\n`);
}

function writeFileAtomic(path: string, content: string): void {
  ensureDir(dirname(path));
  const tempPath = `${path}.tmp-${process.pid}`;
  writeFileSync(tempPath, content, 'utf8');
  renameSync(tempPath, path);
}
