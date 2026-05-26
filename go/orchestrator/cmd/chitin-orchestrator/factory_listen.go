// factory_listen.go — `chitin-orchestrator factory-listen` subcommand
// (spec 098). HTTP receiver for GitHub-style push webhooks that detects
// `.specify/specs/NNN/tasks.md` changes and dispatches via the existing
// `runSchedule` flow.
//
// The listener is local-only by default (binds 127.0.0.1). Production
// deployment with a public endpoint requires a tunnel or reverse proxy
// in front — out of scope per spec.

package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

// specPathPattern matches a tasks.md file path inside `.specify/specs/NNN-name/`
// across any leading directory prefix. Capture 1 is the full `NNN-name`.
var specPathPattern = regexp.MustCompile(`(?:^|/)\.specify/specs/(\d+-[a-z0-9._-]+)/tasks\.md$`)

// pushPayload is the subset of GitHub's push webhook payload this listener
// consumes. Full schema:
// https://docs.github.com/en/webhooks/webhook-events-and-payloads#push
type pushPayload struct {
	Ref     string `json:"ref"`   // e.g. "refs/heads/main"
	After   string `json:"after"` // new commit sha
	Commits []struct {
		Added    []string `json:"added"`
		Modified []string `json:"modified"`
	} `json:"commits"`
}

// factoryResponse is the JSON returned to the webhook caller. Designed
// to be readable by both humans (operators inspecting curl output) and
// by `gh pr comment`-style integrations that render listener status.
type factoryResponse struct {
	Dispatched     bool     `json:"dispatched"`
	SpecRefs       []string `json:"spec_refs"`
	RunIDs         []string `json:"run_ids"`
	SkippedReasons []string `json:"skipped_reasons,omitempty"`
	Error          string   `json:"error,omitempty"`
}

func cmdFactoryListen(args []string) int {
	return runFactoryListen(context.Background(), args, os.Stdout, os.Stderr)
}

func runFactoryListen(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("factory-listen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 8765, "HTTP listen port (binds 127.0.0.1)")
	mainBranch := fs.String("main-branch", "main", "branch name that triggers dispatch")
	secretFile := fs.String("secret-file", defaultFactorySecretFile(), "HMAC secret file (default: ~/.chitin/factory-webhook.secret)")
	repoRoot := fs.String("repo-root", "", "repo root passed to chitin-orchestrator schedule (default: $CHITIN_REPO_ROOT)")
	temporalHost := fs.String("temporal-host", "", "Temporal host:port passed to schedule (default: $TEMPORAL_HOSTPORT)")
	targetRepo := fs.String("target-repo", "", "target-repo passed to schedule (default: same as --repo-root)")
	baseRef := fs.String("base-ref", "main", "base-ref passed to schedule (default: main)")
	logFile := fs.String("log-file", defaultFactoryLogFile(), "request log path (default: ~/.cache/chitin/factory-listen.jsonl)")

	fs.Usage = func() {
		fmt.Fprintln(stderr, "usage: chitin-orchestrator factory-listen [--port N] [--secret-file path] [--repo-root path] [--main-branch name]")
	}
	if err := fs.Parse(args); err != nil {
		return exitUserError
	}

	secret, err := loadFactorySecret(*secretFile)
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return exitRuntimeError
	}

	if err := os.MkdirAll(filepath.Dir(*logFile), 0o755); err != nil {
		fmt.Fprintf(stderr, "error: cannot create log directory: %v\n", err)
		return exitRuntimeError
	}

	resolvedRepo := *repoRoot
	if resolvedRepo == "" {
		// Lazy resolution at request-handle time gives operators flexibility:
		// they can edit $CHITIN_REPO_ROOT mid-flight without restarting.
	}

	handler := &factoryHandler{
		secret:       secret,
		mainBranch:   *mainBranch,
		repoRootFlag: resolvedRepo,
		temporalHost: *temporalHost,
		targetRepo:   *targetRepo,
		baseRef:      *baseRef,
		logFile:      *logFile,
		stderr:       stderr,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook/push", handler.handlePush)
	mux.HandleFunc("/webhook/pr", handler.handlePR)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Graceful shutdown — finish in-flight requests on SIGTERM/SIGINT (FR-009).
	shutdownCtx, cancelShutdown := context.WithCancel(ctx)
	defer cancelShutdown()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigCh:
			fmt.Fprintln(stderr, "factory-listen: shutdown signal received")
			shutCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			_ = srv.Shutdown(shutCtx)
			cancelShutdown()
		case <-shutdownCtx.Done():
			return
		}
	}()

	fmt.Fprintf(stdout, "factory-listen: listening on http://%s (main=%s, repo=%s)\n", addr, *mainBranch, resolvedRepo)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		fmt.Fprintf(stderr, "error: listen failed: %v\n", err)
		return exitRuntimeError
	}
	return exitSuccess
}

// factoryHandler bundles the listener's request-time deps so each
// handler method has no globals to coordinate.
type factoryHandler struct {
	secret       []byte
	mainBranch   string
	repoRootFlag string
	temporalHost string
	targetRepo   string
	baseRef      string
	logFile      string
	stderr       io.Writer

	// Spec 112 US2 injectables — production paths default to nil and the
	// dispatcher uses listOpenSiblingsViaGH + dialTemporalAsStarter. Tests
	// inject stubs so the merged-PR sibling-rebase trigger can be exercised
	// without shelling out to `gh` or dialing a Temporal dev server.
	siblingLister  siblingLister
	temporalDialer temporalDialer
	dispatchFunc   func(context.Context, string) (string, error)

	logMu sync.Mutex
}

// handlePR is the spec 099 slice 3 route. POST /webhook/pr.
//
// Accepts `pull_request`, `pull_request_review`, and `issue_comment`
// events (dispatched on the X-GitHub-Event header). HMAC-verified via
// the same scheme as /webhook/push.
//
// Slice 3 scope:
//   - HMAC verification
//   - Eligibility check (FR-007) via checkPREligibility
//   - Always-on copilot_pr_activity chain emit (FR-013) — the
//     PR-level telemetry stream — for any PR carrying chitin-dispatch
//   - Response shape per contracts/factory-listen-pr-events.md
//
// Deferred to slice 4:
//   - Idempotent chain dedup against prior copilot_pr_detected (FR-008)
//   - PRReviewWorkflow router invocation (FR-009)
//   - copilot_pr_detected + copilot_review_posted/_failed emit
func (h *factoryHandler) handlePR(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		http.Error(w, `{"error":"cannot read body"}`, http.StatusBadRequest)
		return
	}

	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if !verifyHMAC(h.secret, body, sigHeader) {
		h.logRequest(map[string]any{
			"route":              "/webhook/pr",
			"signature_verified": false,
			"reason":             "invalid signature",
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid signature"}`))
		return
	}

	eventType := r.Header.Get("X-GitHub-Event")
	deliveryID := r.Header.Get("X-GitHub-Delivery")

	var p prPayload
	if err := json.Unmarshal(body, &p); err != nil {
		http.Error(w, `{"error":"cannot parse payload"}`, http.StatusBadRequest)
		return
	}

	// Resolve the PR number. Payload shapes vary by event type:
	//   - pull_request: top-level `.number`
	//   - issue_comment: nested `.issue.number`
	//   - pull_request_review / pull_request_review_comment: nested
	//     `.pull_request.number` (NOT top-level — the comment in this
	//     block previously claimed otherwise and produced pr_number=0
	//     for spec 113's iteration trigger).
	prNumber := p.Number
	if prNumber == 0 {
		prNumber = p.PullRequest.Number
	}
	if prNumber == 0 {
		prNumber = p.Issue.Number
	}

	resp := prResponse{
		Received:  true,
		EventType: eventType,
		Action:    p.Action,
		PRNumber:  prNumber,
	}

	elig := checkPREligibility(eventType, &p)
	resp.Eligible = elig.Eligible
	if !elig.Eligible && len(elig.Reasons) > 0 {
		resp.SkippedReason = elig.Reasons[0]
	}

	// Slice 4: dedup-gated PRReviewWorkflow dispatch (FR-008 + FR-009).
	if elig.Eligible {
		dispatchOut := dispatchPRReview(r.Context(), prDispatchInput{
			Repo:         p.Repository.FullName,
			PRNumber:     prNumber,
			PRURL:        p.PullRequest.URL,
			SpecRef:      elig.SpecRef,
			IssueNumber:  elig.IssueNumber,
			Commits:      p.PullRequest.Commits,
			Additions:    p.PullRequest.Additions,
			Deletions:    p.PullRequest.Deletions,
			ChangedFiles: p.PullRequest.Changed,
			TemporalHost: h.temporalHost,
		}, nil, h.stderr)
		resp.ReviewStarted = dispatchOut.ReviewStarted
		resp.ReviewRunID = dispatchOut.ReviewRunID
		resp.DedupSkipped = dispatchOut.DedupSkipped
		if dispatchOut.FailureKind != "" {
			resp.SkippedReason = dispatchOut.FailureKind
		}
	}

	// FR-013: always-on telemetry. Emit copilot_pr_activity for any
	// event on a PR carrying the chitin-dispatch label, regardless of
	// eligibility outcome. (Activity events ARE deduped by delivery ID
	// via the chain's natural append-only history — operators query
	// by delivery_id if they need de-dup.)
	if carriesChitinDispatchLabel(&p) {
		emitCopilotPRActivity(r.Context(), CopilotPRActivityPayload{
			Repo:        p.Repository.FullName,
			PRNumber:    prNumber,
			EventType:   eventType,
			EventAction: p.Action,
			DeliveryID:  deliveryID,
			Payload:     json.RawMessage(body),
			ReceivedAt:  time.Now().UTC().Format(time.RFC3339),
		}, h.stderr)
	}

	// Spec 112 US2: auto-rebase siblings on a chitin-authored merge. The
	// trigger is a pull_request.closed event whose PR is merged AND carries
	// a sched/run/<id> label — every other open PR with the same label is
	// a sibling whose branch may now be stale against the new base.
	if eventType == "pull_request" && p.Action == "closed" && p.PullRequest.Merged {
		if runID := labelSchedRunID(p.PullRequest.Labels); runID != "" {
			localRepo := h.targetRepo
			if localRepo == "" {
				localRepo = h.repoRootFlag
			}
			rebOut := dispatchSiblingRebase(r.Context(), siblingRebaseDispatchInput{
				Repo:           p.Repository.FullName,
				SourcePRNumber: prNumber,
				SchedulerRunID: runID,
				BaseBranch:     p.PullRequest.Base.Ref,
				TargetRepo:     localRepo,
				TemporalHost:   h.temporalHost,
			}, h.siblingLister, h.temporalDialer, h.stderr)
			resp.SiblingRebaseDispatched = rebOut.Dispatched
			resp.SiblingRebaseSiblings = rebOut.Siblings
			resp.SiblingRebasePRs = rebOut.PRNumbers
			if rebOut.FailureKind != "" && resp.SkippedReason == "" {
				resp.SkippedReason = "sibling_rebase:" + rebOut.FailureKind
			}
		}
	}

	// Spec 113 US1: PR comment-respond loop. The trigger is a
	// pull_request_review event with action=submitted, on a PR whose head
	// branch matches chitin/wu/* (factory-authored), submitted by a
	// reviewer in the Copilot allowlist. Non-allowlisted reviewers (humans)
	// route to the escalation path — v1 just no-ops; spec 113 US3 builds
	// the explicit escalation event.
	if eventType == "pull_request_review" && p.Action == "submitted" {
		if chitinWUBranchPattern.MatchString(p.PullRequest.Head.Ref) &&
			isCopilotReviewer(p.Review.User.Login) &&
			(p.Review.State == "commented" || p.Review.State == "changes_requested") {
			localRepo := h.targetRepo
			if localRepo == "" {
				localRepo = h.repoRootFlag
			}
			iterOut := dispatchPRIteration(r.Context(), prIterationDispatchInput{
				Repo:         p.Repository.FullName,
				PRNumber:     prNumber,
				PRBranch:     p.PullRequest.Head.Ref,
				ReviewID:     p.Review.ID,
				DriverID:     driverIDFromBranch(p.PullRequest.Head.Ref),
				TargetRepo:   localRepo,
				TemporalHost: h.temporalHost,
			}, h.temporalDialer, h.stderr)
			resp.PRIterationDispatched = iterOut.Dispatched
			resp.PRIterationWorkflowID = iterOut.WorkflowID
			if iterOut.FailureKind != "" && resp.SkippedReason == "" {
				resp.SkippedReason = "pr_iteration:" + iterOut.FailureKind
			}
		}
	}

	h.logRequest(map[string]any{
		"route":                     "/webhook/pr",
		"signature_verified":        true,
		"event_type":                eventType,
		"event_action":              p.Action,
		"pr_number":                 prNumber,
		"eligible":                  elig.Eligible,
		"skipped_reason":            resp.SkippedReason,
		"sibling_rebase_siblings":   resp.SiblingRebaseSiblings,
		"sibling_rebase_dispatched": resp.SiblingRebaseDispatched,
	})

	respBody, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
}

// carriesChitinDispatchLabel checks both PR and Issue label slots —
// pull_request events carry labels on .pull_request.labels;
// issue_comment events carry them on .issue.labels.
func carriesChitinDispatchLabel(p *prPayload) bool {
	if hasLabel(p.PullRequest.Labels, chitinDispatchLabel) {
		return true
	}
	if hasLabel(p.Issue.Labels, chitinDispatchLabel) {
		return true
	}
	return false
}

// handlePush is the load-bearing route. POST /webhook/push.
func (h *factoryHandler) handlePush(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 8<<20)) // 8 MiB cap
	if err != nil {
		http.Error(w, `{"error":"cannot read body"}`, http.StatusBadRequest)
		return
	}

	// FR-002: verify HMAC. Reject unsigned or mis-signed bodies before
	// any payload parsing.
	sigHeader := r.Header.Get("X-Hub-Signature-256")
	if !verifyHMAC(h.secret, body, sigHeader) {
		h.logRequest(map[string]any{"signature_verified": false, "reason": "invalid signature"})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid signature"}`))
		return
	}

	var payload pushPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, `{"error":"cannot parse payload"}`, http.StatusBadRequest)
		return
	}

	resp := h.process(r.Context(), &payload)
	respBody, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(respBody)
}

// process is the dispatch core: extract spec refs, schedule each one,
// build the response. Pure with respect to its inputs (apart from the
// chain emit + log + child schedule invocations).
func (h *factoryHandler) process(ctx context.Context, p *pushPayload) factoryResponse {
	wantRef := "refs/heads/" + h.mainBranch
	if p.Ref != wantRef {
		h.logRequest(map[string]any{
			"signature_verified": true,
			"branch":             p.Ref,
			"reason":             "non-main branch",
			"dispatched":         false,
		})
		return factoryResponse{Dispatched: false, SkippedReasons: []string{"non-main branch"}}
	}

	specRefs := extractSpecRefs(p)
	if len(specRefs) == 0 {
		h.logRequest(map[string]any{
			"signature_verified": true,
			"branch":             p.Ref,
			"commit":             p.After,
			"dispatched":         false,
			"reason":             "no tasks.md changes",
		})
		return factoryResponse{Dispatched: false, SkippedReasons: []string{"no tasks.md changes"}}
	}

	resp := factoryResponse{}
	for _, ref := range specRefs {
		runID, err := h.dispatch(ctx, ref)
		if err != nil {
			h.emitFailureEvent(ctx, ref, classifyDispatchError(err), err.Error())
			resp.SkippedReasons = append(resp.SkippedReasons, fmt.Sprintf("schedule failed for %s: %v", ref, err))
			continue
		}
		h.emitTriggeredEvent(ctx, ref, runID, "github_webhook", p.Ref, p.After)
		resp.SpecRefs = append(resp.SpecRefs, ref)
		resp.RunIDs = append(resp.RunIDs, runID)
	}
	resp.Dispatched = len(resp.RunIDs) > 0
	h.logRequest(map[string]any{
		"signature_verified": true,
		"branch":             p.Ref,
		"commit":             p.After,
		"dispatched":         resp.Dispatched,
		"spec_refs":          specRefs,
		"run_ids":            resp.RunIDs,
		"skipped_reasons":    resp.SkippedReasons,
	})
	return resp
}

// dispatch invokes the existing runSchedule flow in-process and returns
// the new RunID. Reuses everything spec 097's CLI built: spec ref
// resolution, DAG compile, validation, Temporal client dial, chain
// event emission via emit.go.
func (h *factoryHandler) dispatch(ctx context.Context, specRef string) (string, error) {
	if h.dispatchFunc != nil {
		return h.dispatchFunc(ctx, specRef)
	}
	args := []string{}
	if h.repoRootFlag != "" {
		args = append(args, "--repo-root", h.repoRootFlag)
	}
	if h.temporalHost != "" {
		args = append(args, "--temporal-host", h.temporalHost)
	}
	if h.targetRepo != "" {
		args = append(args, "--target-repo", h.targetRepo)
	}
	if h.baseRef != "" {
		args = append(args, "--base-ref", h.baseRef)
	}
	args = append(args, specRef)

	var stdout, stderr strings.Builder
	code := runSchedule(ctx, args, &stdout, &stderr)
	if code != exitSuccess {
		return "", fmt.Errorf("runSchedule exit=%d: %s", code, strings.TrimSpace(stderr.String()))
	}
	// Parse "run_id=<uuid>" from stdout (matches schedule's success line).
	const marker = "run_id="
	i := strings.Index(stdout.String(), marker)
	if i == -1 {
		return "", fmt.Errorf("schedule succeeded but no run_id in stdout: %q", stdout.String())
	}
	runID := strings.TrimSpace(stdout.String()[i+len(marker):])
	return runID, nil
}

// extractSpecRefs walks the payload's commits and returns the unique
// set of spec refs whose tasks.md was added or modified. Sorted for
// deterministic output.
func extractSpecRefs(p *pushPayload) []string {
	seen := map[string]struct{}{}
	for _, c := range p.Commits {
		for _, path := range append(append([]string{}, c.Added...), c.Modified...) {
			if m := specPathPattern.FindStringSubmatch(path); m != nil {
				seen[m[1]] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for ref := range seen {
		out = append(out, ref)
	}
	// Deterministic order — matters for the response JSON and the test assertions.
	sortStrings(out)
	return out
}

// verifyHMAC validates GitHub's X-Hub-Signature-256 header against the
// configured secret. Format: "sha256=<hex>". Empty header / empty secret /
// length mismatch all return false. Constant-time comparison.
func verifyHMAC(secret, body []byte, sigHeader string) bool {
	if sigHeader == "" || len(secret) == 0 {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(sigHeader, prefix) {
		return false
	}
	wanted, err := hex.DecodeString(sigHeader[len(prefix):])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	got := mac.Sum(nil)
	return hmac.Equal(got, wanted)
}

// signPayload is the inverse of verifyHMAC — used by simulate-webhook to
// construct a signed request the listener will accept. Returns the value
// for the X-Hub-Signature-256 header.
func signPayload(secret, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func defaultFactorySecretFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".chitin/factory-webhook.secret"
	}
	return filepath.Join(home, ".chitin", "factory-webhook.secret")
}

func defaultFactoryLogFile() string {
	if env := os.Getenv("CHITIN_FACTORY_LOG"); env != "" {
		return env
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".cache/chitin/factory-listen.jsonl"
	}
	return filepath.Join(home, ".cache", "chitin", "factory-listen.jsonl")
}

func loadFactorySecret(path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("--secret-file is required (default %s)", defaultFactorySecretFile())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read secret file %s: %w (generate with: openssl rand -hex 32 > %s)", path, err, path)
	}
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 {
		return nil, fmt.Errorf("secret file %s is empty", path)
	}
	return b, nil
}

func (h *factoryHandler) logRequest(fields map[string]any) {
	h.logMu.Lock()
	defer h.logMu.Unlock()
	fields["ts"] = time.Now().UTC().Format(time.RFC3339Nano)
	line, err := json.Marshal(fields)
	if err != nil {
		return
	}
	f, err := os.OpenFile(h.logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(line, '\n'))
}

// emitTriggeredEvent writes a factory_triggered chain event for a
// successful dispatch. Reuses the existing chain-emit helper.
func (h *factoryHandler) emitTriggeredEvent(ctx context.Context, specRef, runID, source, branch, commit string) {
	payload := map[string]any{
		"spec_ref":   specRef,
		"run_id":     runID,
		"source":     source,
		"branch":     branch,
		"commit_sha": commit,
	}
	emitChainEvent(ctx, "factory_triggered", runID, payload, h.stderr)
}

// emitFailureEvent writes a factory_dispatch_failed chain event for a
// schedule invocation that errored. The chain uses the spec_ref as the
// run_id (we have no scheduler run_id to key by — dispatch never reached
// ExecuteWorkflow). That keeps the failure event findable by spec.
func (h *factoryHandler) emitFailureEvent(ctx context.Context, specRef string, failureKind FactoryDispatchFailureKind, errMsg string) {
	if !failureKind.Valid() {
		failureKind = FactoryDispatchFailureKindInternal
	}
	payload := map[string]any{
		"spec_ref":     specRef,
		"failure_kind": string(failureKind),
		"error":        errMsg,
	}
	emitChainEvent(ctx, "factory_dispatch_failed", "factory-"+specRef, payload, h.stderr)
}

// sortStrings is a tiny inline replacement for sort.Strings to avoid
// importing sort just for this; matches deterministic ordering tests.
func sortStrings(s []string) {
	// Simple insertion sort — n is small (typically 1-3 spec refs per push).
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
