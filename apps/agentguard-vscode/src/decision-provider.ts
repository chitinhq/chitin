import * as vscode from 'vscode';
import type { DecisionRecord } from './decision-types';

function relativeTime(ts: string): string {
  const when = new Date(ts).getTime();
  const deltaSeconds = Math.max(0, Math.floor((Date.now() - when) / 1000));
  if (deltaSeconds < 60) {
    return `${deltaSeconds}s ago`;
  }
  if (deltaSeconds < 3600) {
    return `${Math.floor(deltaSeconds / 60)}m ago`;
  }
  return `${Math.floor(deltaSeconds / 3600)}h ago`;
}

export class DecisionItem extends vscode.TreeItem {
  constructor(readonly decision: DecisionRecord) {
    const verdict = decision.ruleId === 'lockdown' ? 'lockdown' : decision.decision;
    super(`${verdict.toUpperCase()} ${decision.actionType}`, vscode.TreeItemCollapsibleState.None);
    this.description = relativeTime(decision.ts);
    this.tooltip = `${decision.ts}\n${decision.actionType} ${decision.actionTarget}\n${decision.reason}`;
    this.command = {
      command: 'chitinAgentguard.openDecision',
      title: 'Open Decision Details',
      arguments: [decision],
    };
    this.contextValue = 'decision';
  }
}

export class DecisionsTreeProvider implements vscode.TreeDataProvider<DecisionItem> {
  private readonly changed = new vscode.EventEmitter<DecisionItem | undefined | void>();
  private decisions: readonly DecisionRecord[] = [];

  readonly onDidChangeTreeData = this.changed.event;

  setDecisions(decisions: readonly DecisionRecord[]): void {
    this.decisions = decisions;
    this.changed.fire();
  }

  getTreeItem(element: DecisionItem): vscode.TreeItem {
    return element;
  }

  getChildren(): DecisionItem[] {
    return this.decisions.map((decision) => new DecisionItem(decision));
  }
}
