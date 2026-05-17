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
		driversExit(2, "drivers_args", "usage: chitin-kernel drivers list [--json] [--cwd <dir>] [--policy-file <path>]")
	}
	switch args[0] {
	case "list":
		cmdDriversList(args[1:])
	default:
		driversExit(2, "drivers_args", "usage: chitin-kernel drivers list [--json] [--cwd <dir>] [--policy-file <path>]")
	}
}

func cmdDriversList(args []string) {
	fs := flag.NewFlagSet("drivers list", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	asJSON := fs.Bool("json", false, "emit JSON")
	cwd := fs.String("cwd", ".", "working directory used to resolve chitin.yaml")
	policyFile := fs.String("policy-file", "", "explicit path to chitin.yaml")
	if err := fs.Parse(args); err != nil {
		driversExit(2, "drivers_args", err.Error())
	}

	policy, err := loadDriversPolicy(*cwd, *policyFile)
	if err != nil {
		driversExit(2, "drivers_load", err.Error())
	}

	if *asJSON {
		out, err := json.Marshal(map[string]any{"drivers": policy.Drivers})
		if err != nil {
			driversExit(2, "drivers_json", err.Error())
		}
		fmt.Println(string(out))
		return
	}

	for _, driver := range policy.Drivers {
		fmt.Println(driver.ID)
	}
}

func loadDriversPolicy(cwd, policyFile string) (gov.Policy, error) {
	if policyFile != "" {
		return gov.LoadPolicyFile(policyFile)
	}
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return gov.Policy{}, err
	}
	policy, _, err := gov.LoadWithInheritance(absCwd)
	return policy, err
}

func driversExit(code int, kind, msg string) {
	out, _ := json.Marshal(map[string]string{
		"error":   kind,
		"message": msg,
	})
	fmt.Fprintln(os.Stderr, string(out))
	os.Exit(code)
}
