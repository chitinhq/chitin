// client.go — Temporal client dial helper shared by spec 097 subcommands.
//
// The hostport is resolved in flag → env → default order, matching the
// existing worker-host's TEMPORAL_HOSTPORT environment variable convention.
// Subcommand handlers call dialTemporal once and defer Close().

package main

import (
	"context"
	"fmt"
	"os"

	"go.temporal.io/sdk/client"
)

// dialTemporal opens a Temporal client connection to the given hostport,
// falling back to $TEMPORAL_HOSTPORT and then client.DefaultHostPort
// (127.0.0.1:7233) when the flag is empty. Returns a client the caller must
// Close().
//
// Caller-facing error message matches the contract in
// specs/097-operator-scheduler-entrypoint/contracts/{schedule,status,cancel}-subcommand.md
// — when the caller surfaces it to stderr it reads
//   error: Temporal unreachable at <host:port> — is the temporal-dev service running?
// followed by exit code 2 (runtime error).
func dialTemporal(ctx context.Context, flagHost string) (client.Client, string, error) {
	host := flagHost
	if host == "" {
		host = os.Getenv("TEMPORAL_HOSTPORT")
	}
	if host == "" {
		host = client.DefaultHostPort
	}
	c, err := client.Dial(client.Options{HostPort: host})
	if err != nil {
		return nil, host, fmt.Errorf("Temporal unreachable at %s: %w", host, err)
	}
	return c, host, nil
}
