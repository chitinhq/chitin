// simulate_webhook.go — `chitin-orchestrator simulate-webhook` subcommand
// (spec 098 US2 / FR-006). Constructs a synthetic GitHub push payload
// describing one tasks.md add, signs it with the listener's secret, and
// POSTs to the local factory-listen endpoint. The result of an
// end-to-end demo without a real GitHub webhook configured.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func cmdSimulateWebhook(args []string) int {
	return runSimulateWebhook(context.Background(), args, os.Stdout, os.Stderr)
}

func runSimulateWebhook(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("simulate-webhook", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 8765, "listener port to POST to (default 8765)")
	specRef := fs.String("spec-ref", "", "spec ref to dispatch (e.g., 098-fixture). Required.")
	branch := fs.String("branch", "main", "branch name to construct the push for (default main)")
	secretFile := fs.String("secret-file", defaultFactorySecretFile(), "HMAC secret file used by the listener")
	commitSHA := fs.String("commit-sha", "0000000000000000000000000000000000000000", "synthetic commit sha for the payload")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator simulate-webhook --spec-ref <ref> [--port N] [--branch name] [--secret-file path]")
	}
	if err := fs.Parse(args); err != nil {
		return exitUserError
	}
	if *specRef == "" {
		fs.Usage()
		fmt.Fprintln(stderr, "error: --spec-ref is required")
		return exitUserError
	}

	secret, err := loadFactorySecret(*secretFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	body := constructSyntheticPushPayload(*specRef, *branch, *commitSHA)
	sig := signPayload(secret, body)

	url := fmt.Sprintf("http://127.0.0.1:%d/webhook/push", *port)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(stderr, "error: cannot build request: %v\n", err)
		return exitRuntimeError
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Hub-Signature-256", sig)
	req.Header.Set("X-GitHub-Event", "push")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(stderr, "error: listener unreachable at 127.0.0.1:%d: %v\n", *port, err)
		return exitRuntimeError
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// Pretty-print the listener's JSON response so the operator sees what happened.
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, respBody, "", "  "); err == nil {
		fmt.Fprintln(stdout, pretty.String())
	} else {
		// Listener may have returned non-JSON on error — print raw.
		fmt.Fprintln(stdout, strings.TrimSpace(string(respBody)))
	}

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(stderr, "error: listener returned HTTP %d\n", resp.StatusCode)
		return exitRuntimeError
	}
	return exitSuccess
}

// constructSyntheticPushPayload builds a minimal GitHub push payload
// covering exactly the fields the factory listener consumes:
// ref + after + commits[].added. Just enough to test the dispatch
// path; not a faithful reproduction of GitHub's full payload.
func constructSyntheticPushPayload(specRef, branch, commitSHA string) []byte {
	payload := pushPayload{
		Ref:   "refs/heads/" + branch,
		After: commitSHA,
		Commits: []struct {
			Added    []string `json:"added"`
			Modified []string `json:"modified"`
		}{
			{
				Added: []string{
					fmt.Sprintf(".specify/specs/%s/tasks.md", specRef),
				},
			},
		},
	}
	b, _ := json.Marshal(payload)
	return b
}
