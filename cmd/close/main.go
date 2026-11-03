package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/voska/close/internal/cli"
)

var version = "dev"
var commit = "unknown"
var date = "unknown"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Execute(ctx, os.Args[1:], cli.Dependencies{}, cli.Build{Version: version, Commit: commit, Date: date})
	stop()
	os.Exit(code)
}
