// Package copilot wraps the Copilot Go SDK v0.2.2 for use as an in-kernel
// driver with inline governance via the chitin gov package.
//
// All governance decisions happen library-direct via gov.Gate.Evaluate().
// The SDK is treated as a subprocess orchestrator; the copilot CLI binary
// must be on PATH (or explicitly provided via ClientOpts.CLIPath).
//
// Invariant: NewClient returns a non-nil *Client if and only if the copilot
// binary path is resolvable. The SDK subprocess is NOT started until
// Start(ctx) is called — NewClient is side-effect-free.
package copilot

import (
	"context"
	"errors"
	"fmt"
	"os/exec"

	copilotsdk "github.com/github/copilot-sdk/go"
)

// ClientOpts configures a Copilot driver client.
type ClientOpts struct {
	// CLIPath, if set, overrides the exec.LookPath resolution of the copilot
	// binary. Useful for tests or non-standard installs.
	CLIPath string

	// UseLoggedInUser determines whether auth reuses the gh-keychain session.
	// Nil is treated as true — that is the proven auth path from the spike.
	UseLoggedInUser *bool
}

// Client is a thin wrapper over the Copilot SDK client.
//
// BinaryPath is the resolved path to the copilot CLI binary.
// sdkClient is the underlying SDK client; its subprocess is not started
// until Start(ctx) is called.
type Client struct {
	// BinaryPath is the resolved path to the copilot CLI binary. Exported
	// so callers and tests can inspect what was resolved without digging
	// through unexported SDK state.
	BinaryPath string

	sdkClient *copilotsdk.Client
}

// NewClient constructs a Client and resolves the copilot binary path.
//
// Binary resolution order:
//  1. opts.CLIPath — used verbatim, skips PATH search.
//  2. exec.LookPath("copilot") — fails fast with an actionable error if
//     the binary is not found.
//
// The SDK subprocess is NOT started here. Call Start(ctx) before
// CreateSession. NewClient is safe to call from tests with a fake binary.
func NewClient(opts ClientOpts) (*Client, error) {
	bin := opts.CLIPath
	if bin == "" {
		resolved, err := exec.LookPath("copilot")
		if err != nil {
			return nil, fmt.Errorf(
				"copilot binary not found on PATH: %w — install via `gh extension install github/gh-copilot` or the Copilot CLI release page",
				err,
			)
		}
		bin = resolved
	}

	useLoggedIn := true
	if opts.UseLoggedInUser != nil {
		useLoggedIn = *opts.UseLoggedInUser
	}

	// NewClient on the SDK is side-effect-free: it stores options and returns.
	// The subprocess is launched by sdkClient.Start(ctx).
	sdkClient := copilotsdk.NewClient(&copilotsdk.ClientOptions{
		CLIPath:         bin,
		UseLoggedInUser: &useLoggedIn,
	})

	return &Client{BinaryPath: bin, sdkClient: sdkClient}, nil
}

// Start starts the underlying SDK subprocess and verifies it is reachable.
// Call this before CreateSession.
func (c *Client) Start(ctx context.Context) error {
	if c.sdkClient == nil {
		return errors.New("copilot client not initialized")
	}
	return c.sdkClient.Start(ctx)
}

// Close gracefully shuts down the subprocess and releases resources.
// Wraps SDK's Stop() method under the conventional Go io.Closer name.
func (c *Client) Close() error {
	if c.sdkClient == nil {
		return nil
	}
	return c.sdkClient.Stop()
}

// SDKClient exposes the underlying SDK client for code paths that need
// direct access (e.g., CreateSession in driver.go). The abstraction is
// intentionally leaky — this package is a thin wrapper, not a
// re-implementation.
func (c *Client) SDKClient() *copilotsdk.Client {
	return c.sdkClient
}
