package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/boardconfig"
)

func cmdBoardConfig(args []string) {
	fs := flag.NewFlagSet("board-config", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: chitin-kernel board-config <slug> <field>")
	}
	if err := fs.Parse(args); err != nil {
		boardConfigExit(2, "board_config_args", err.Error())
	}
	if fs.NArg() != 2 {
		boardConfigExit(2, "board_config_args", "usage: chitin-kernel board-config <slug> <field>")
	}

	value, err := boardconfig.ResolveField(fs.Arg(0), fs.Arg(1))
	if err != nil {
		switch {
		case errors.Is(err, boardconfig.ErrNoBoardsInitialized):
			boardConfigExit(3, "no_boards_initialized", err.Error())
		default:
			var slugErr boardconfig.InvalidSlugError
			if errors.As(err, &slugErr) {
				boardConfigExit(2, "invalid_slug", err.Error())
			}
			var boardErr boardconfig.UnknownBoardError
			if errors.As(err, &boardErr) {
				boardConfigExit(3, "unknown_board", err.Error())
			}
			var missingFieldErr boardconfig.MissingFieldError
			if errors.As(err, &missingFieldErr) {
				boardConfigExit(2, "missing_field", err.Error())
			}
			var missingConfigErr boardconfig.MissingConfigError
			if errors.As(err, &missingConfigErr) {
				boardConfigExit(2, "missing_config", err.Error())
			}
			var unknownFieldErr boardconfig.UnknownFieldError
			if errors.As(err, &unknownFieldErr) {
				boardConfigExit(2, "unknown_field", err.Error())
			}
			boardConfigExit(2, "board_config_error", err.Error())
		}
	}

	fmt.Println(value)
}

func boardConfigExit(code int, kind, msg string) {
	out, _ := json.Marshal(map[string]string{
		"error":   kind,
		"message": msg,
	})
	fmt.Fprintln(os.Stderr, string(out))
	os.Exit(code)
}
