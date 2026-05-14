import { execFile } from 'node:child_process';
import { promisify } from 'node:util';
import * as vscode from 'vscode';
import { findWorkspaceChitinDir } from './chitin-locator';
import { DecisionsTreeProvider } from './decision-provider';
import { reduceDecisionSnapshot, emptyDecisionSnapshot } from './decision-state';
import { DecisionStream } from './decision-stream';
import type { DecisionRecord, DecisionSnapshot } from './decision-types';

const execFileAsync = promisify(execFile);

interface LockStatus {
  readonly locked: boolean;
}

export async function activate(context: vscode.ExtensionContext): Promise<void> {
  const workspaceFolders = vscode.workspace.workspaceFolders ?? [];
  const chitinDir = findWorkspaceChitinDir(workspaceFolders.map((folder) => folder.uri.fsPath));
  if (!chitinDir) {
    return;
  }

  const provider = new DecisionsTreeProvider();
  const tree = vscode.window.createTreeView('chitinDecisions', { treeDataProvider: provider });
  const statusBar = vscode.window.createStatusBarItem(vscode.StatusBarAlignment.Left, 10);
  statusBar.command = 'workbench.view.extension.chitinAgentguard';
  statusBar.show();

  let snapshot: DecisionSnapshot = emptyDecisionSnapshot();
  let transportMode: 'socket' | 'tail' = 'tail';
  const lockStatuses = new Map<string, LockStatus>();

  const refreshStatusBar = () => {
    const anyLockdown = [...lockStatuses.values()].some((status) => status.locked) || snapshot.lockdown;
    const lastBlocked = snapshot.lastBlocked
      ? `${snapshot.lastBlocked.actionType}${snapshot.lastBlocked.actionTarget ? ` ${snapshot.lastBlocked.actionTarget}` : ''}`
      : 'none';
    statusBar.text = anyLockdown
      ? `$(warning) chitin lockdown | last block: ${lastBlocked}`
      : `$(shield) chitin normal | last block: ${lastBlocked}`;
    statusBar.tooltip = `Source: ${transportMode}\nLast blocked action: ${lastBlocked}`;
  };

  const probeLockStatus = async (decision: DecisionRecord) => {
    if (!decision.agent) {
      return;
    }
    try {
      const workspacePath = workspaceFolders[0]?.uri.fsPath ?? '.';
      const { stdout } = await execFileAsync('chitin-kernel', [
        'gate',
        'status',
        '--cwd',
        workspacePath,
        '--agent',
        decision.agent,
      ]);
      const parsed = JSON.parse(stdout) as { locked?: unknown };
      lockStatuses.set(decision.agent, { locked: parsed.locked === true });
      refreshStatusBar();
    } catch {
      // Kernel lookups are best-effort; the live stream still updates the view.
    }
  };

  const openDecision = async (decision: DecisionRecord) => {
    const workspacePath = workspaceFolders[0]?.uri.fsPath ?? '.';
    let body = '';
    try {
      const { stdout } = await execFileAsync('chitin-kernel', [
        'explain',
        decision.eventId,
        '--cwd',
        workspacePath,
        '--dir',
        chitinDir,
      ]);
      body = stdout.trim();
    } catch {
      body = JSON.stringify(decision.raw, null, 2);
    }

    const panel = vscode.window.createWebviewPanel(
      'chitinDecisionDetail',
      `Decision ${decision.eventId.slice(0, 8)}`,
      vscode.ViewColumn.Beside,
      { enableFindWidget: true },
    );
    panel.webview.html = renderDecisionPanel(decision, body);
  };

  context.subscriptions.push(
    tree,
    statusBar,
    vscode.commands.registerCommand('chitinAgentguard.openDecision', openDecision),
    vscode.commands.registerCommand('chitinAgentguard.refresh', () => {
      provider.setDecisions(snapshot.recent);
      refreshStatusBar();
    }),
  );

  const stream = new DecisionStream({
    chitinDir,
    socketPaths: [
      vscode.workspace.getConfiguration('chitinAgentguard').get<string>('socketPath') ?? '',
      `${chitinDir}/gate.sock`,
    ].filter(Boolean),
    onModeChange: (mode) => {
      transportMode = mode;
      refreshStatusBar();
    },
    onDecision: (decision) => {
      snapshot = reduceDecisionSnapshot(snapshot, decision);
      provider.setDecisions(snapshot.recent);
      refreshStatusBar();
      void probeLockStatus(decision);
    },
  });

  await stream.start();
  refreshStatusBar();
  context.subscriptions.push({ dispose: () => stream.stop() });
}

export function deactivate(): void {
  // No-op. Disposables registered during activate tear down the stream.
}

function renderDecisionPanel(decision: DecisionRecord, explainBody: string): string {
  const escapedExplain = escapeHtml(explainBody);
  const escapedTarget = escapeHtml(decision.actionTarget);
  return `<!DOCTYPE html>
<html lang="en">
  <body style="font-family: var(--vscode-editor-font-family); padding: 16px;">
    <h2 style="margin-top: 0;">${escapeHtml(decision.decision.toUpperCase())} ${escapeHtml(decision.actionType)}</h2>
    <p><strong>When:</strong> ${escapeHtml(decision.ts)}</p>
    <p><strong>Target:</strong> ${escapedTarget || '(none)'}</p>
    <p><strong>Rule:</strong> ${escapeHtml(decision.ruleId)}</p>
    <pre style="white-space: pre-wrap; line-height: 1.4;">${escapedExplain}</pre>
  </body>
</html>`;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;');
}
