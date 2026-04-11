package main

import (
	"fmt"
	"os"

	"goodkind.io/mwan/internal/agent"
	"goodkind.io/mwan/internal/config"
	"goodkind.io/mwan/internal/cutover"
	"goodkind.io/mwan/internal/cutover2"
	"goodkind.io/mwan/internal/watchdog"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: mwan <agent|watchdog|cutover|cutover2> [flags]")
		os.Exit(1)
	}
	sub := os.Args[1]
	os.Args = append([]string{os.Args[0]}, os.Args[2:]...)

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mwan: %v\n", err)
		os.Exit(1)
	}

	var runErr error
	switch sub {
	case "agent":
		agent.Run(cfg)
	case "watchdog":
		if len(os.Args) > 1 && os.Args[1] == "failover" {
			os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
			runErr = watchdog.FailoverRun(cfg)
		} else {
			runErr = watchdog.Run(cfg)
		}
	case "cutover":
		dryRun := false
		for _, arg := range os.Args[1:] {
			if arg == "--dry-run" {
				dryRun = true
			}
		}
		runErr = cutover.Run(cfg, dryRun)
	case "cutover2":
		runErr = cutover2.Run(cfg)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", sub)
		os.Exit(1)
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "mwan %s: %v\n", sub, runErr)
		os.Exit(1)
	}
}
