package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/chitinhq/chitin/go/execution-kernel/internal/gov"
)

func cmdDrivers(args []string) {
	if len(args) < 1 {
		exitErr("drivers_no_subcommand", "usage: chitin-kernel drivers list [--json] [--cwd <path>] [--policy-file <path>]")
	}
	switch args[0] {
	case "list":
		cmdDriversList(args[1:])
	default:
		exitErr("drivers_unknown_subcommand", args[0])
	}
}

func cmdDriversList(args []string) {
	fs := flag.NewFlagSet("drivers list", flag.ExitOnError)
	cwd := fs.String("cwd", ".", "cwd to load policy from")
	policyFile := fs.String("policy-file", os.Getenv("CHITIN_POLICY_FILE"), "explicit chitin.yaml path; overrides the cwd-walk-upward lookup")
	jsonOut := fs.Bool("json", false, "emit structured JSON")
	fs.Parse(args)

	absCwd, err := filepath.Abs(*cwd)
	if err != nil {
		exitErr("drivers_abs_cwd", err.Error())
	}

	var (
		policy  gov.Policy
		sources []string
	)
	if *policyFile != "" {
		policy, err = gov.LoadPolicyFile(*policyFile)
		sources = []string{*policyFile}
	} else {
		policy, sources, err = gov.LoadWithInheritance(absCwd)
	}
	if err != nil {
		exitErr("drivers_load_policy", err.Error())
	}

	if *jsonOut {
		out, _ := json.Marshal(map[string]any{
			"policy_id": policy.ID,
			"sources":   sources,
			"drivers":   policy.Drivers,
			"count":     len(policy.Drivers),
		})
		fmt.Println(string(out))
		return
	}

	for _, driver := range policy.Drivers {
		fmt.Println(driver.ID)
	}
}
