// @chitin/scheduler — scheduler library entrypoint.
//
// Slice scope: this PR ships only the Slack notifier (`./notify/slack`).
// Schema, sqlite store, and other notifier transports land in follow-up
// PRs. The barrel re-exports what's currently shipped; future PRs add
// their own re-exports here.

export * from './notify/slack.js';
