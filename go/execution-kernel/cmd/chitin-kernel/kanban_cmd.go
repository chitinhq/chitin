package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	_ "modernc.org/sqlite"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/boardconfig"
	"github.com/chitinhq/chitin/go/execution-kernel/internal/kanban"
)

// cmdKanban handles `chitin-kernel kanban <subcommand>`.
func cmdKanban(args []string) {
	if len(args) == 0 {
		kanbanExit(2, "kanban_args", "usage: chitin-kernel kanban migrate <board>")
	}
	switch args[0] {
	case "migrate":
		cmdKanbanMigrate(args[1:])
	default:
		kanbanExit(2, "kanban_unknown_subcommand", "unknown kanban subcommand: "+args[0])
	}
}

func cmdKanbanMigrate(args []string) {
	if len(args) != 1 {
		kanbanExit(2, "kanban_migrate_args", "usage: chitin-kernel kanban migrate <board>")
	}
	board := args[0]

	destPath, err := boardconfig.ResolveField(board, "chitin_db_path")
	if err != nil {
		mapBoardconfigErrToExit(err)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		kanbanExit(2, "user_home", err.Error())
	}
	srcPath := filepath.Join(home, ".hermes", "kanban", "boards", board, "kanban.db")
	if _, err := os.Stat(srcPath); err != nil {
		kanbanExit(2, "source_missing", "source kanban.db missing: "+srcPath)
	}

	if err := kanban.Migrate(srcPath, destPath); err != nil {
		kanbanExit(1, "migrate_failed", err.Error())
	}

	src, err := sql.Open("sqlite", "file:"+srcPath+"?mode=ro")
	if err != nil {
		kanbanExit(1, "open_src_for_verify", err.Error())
	}
	defer src.Close()
	dest, err := sql.Open("sqlite", destPath)
	if err != nil {
		kanbanExit(1, "open_dest_for_verify", err.Error())
	}
	defer dest.Close()

	if err := kanban.VerifyCounts(src, dest); err != nil {
		kanbanExit(1, "verify_failed", err.Error())
	}

	counts, _ := kanban.RowCounts(dest)
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Printf("%s: %d\n", k, counts[k])
	}
	fmt.Printf("ok: %s\n", destPath)
}

func mapBoardconfigErrToExit(err error) {
	if errors.Is(err, boardconfig.ErrNoBoardsInitialized) {
		kanbanExit(3, "no_boards_initialized", err.Error())
	}
	var slugErr boardconfig.InvalidSlugError
	if errors.As(err, &slugErr) {
		kanbanExit(2, "invalid_slug", err.Error())
	}
	var boardErr boardconfig.UnknownBoardError
	if errors.As(err, &boardErr) {
		kanbanExit(3, "unknown_board", err.Error())
	}
	var missingFieldErr boardconfig.MissingFieldError
	if errors.As(err, &missingFieldErr) {
		kanbanExit(2, "missing_field", err.Error())
	}
	var missingConfigErr boardconfig.MissingConfigError
	if errors.As(err, &missingConfigErr) {
		kanbanExit(2, "missing_config", err.Error())
	}
	var unknownFieldErr boardconfig.UnknownFieldError
	if errors.As(err, &unknownFieldErr) {
		kanbanExit(2, "unknown_field", err.Error())
	}
	kanbanExit(2, "boardconfig_error", err.Error())
}

func kanbanExit(code int, kind, msg string) {
	out, _ := json.Marshal(map[string]string{
		"error":   kind,
		"message": msg,
	})
	fmt.Fprintln(os.Stderr, string(out))
	os.Exit(code)
}
