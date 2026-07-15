// Command armada is the operator CLI for the fleet-management platform. It is
// the primary interface — there is no web dashboard. Run it from your VPS (or
// anywhere with reach to the control plane) to register, inspect, and monitor
// managed devices.
//
// Configuration is read from the environment and can be overridden per command:
//
//	ARMADA_SERVER_URL     control-plane base URL (default http://localhost:8080)
//	ARMADA_OPERATOR_TOKEN operator bearer token (if the server requires one)
//	ARMADA_TENANT_ID      tenant to operate within (required)
//
// Usage:
//
//	armada systems register --name web-1 --fqdn web1.example.com --region eu
//	armada systems list [--region eu] [--health offline]
//	armada systems get <id>
//	armada systems inventory <id>
//	armada enroll <system-id> [--ttl 15m]
//	armada monitor [--interval 5s] [--region eu]
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "systems":
		err = runSystems(ctx, os.Args[2:])
	case "enroll":
		err = runEnroll(ctx, os.Args[2:])
	case "install-command":
		err = runInstallCommand(ctx, os.Args[2:])
	case "monitor":
		err = runMonitor(ctx, os.Args[2:])
	case "version", "--version", "-v":
		fmt.Printf("armada %s\n", version)
	case "help", "--help", "-h":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `armada — fleet management CLI

Commands:
  systems register   Register a new managed device
  systems list       List devices (with filters)
  systems get        Show one device in detail
  systems inventory  Show a device's latest hardware/OS inventory
  enroll             Issue a one-time enrollment token for a device's agent
  install-command    Print the one-line curl|sh (and PowerShell) to bind a device
  monitor            Live health + metrics view of the fleet
  version            Print the CLI version

Global configuration (env or per-command flags):
  --server   ARMADA_SERVER_URL      control-plane URL (default http://localhost:8080)
  --token    ARMADA_OPERATOR_TOKEN  operator bearer token
  --tenant   ARMADA_TENANT_ID       tenant id (required)

Run 'armada <command> -h' for command-specific flags.
`)
}
