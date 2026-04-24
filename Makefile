.PHONY: drive-copilot-live

# Run the live integration test against the real Copilot backend.
# Requires: gh auth, Copilot seat, network access.
# NOT run in CI — invoke manually before each rehearsal.
drive-copilot-live:
	cd go/execution-kernel && go test -tags=live ./internal/driver/copilot -run TestLive -v
