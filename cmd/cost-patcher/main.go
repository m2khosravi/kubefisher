package main

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/m2khosravi/kubefisher/internal/costpatcher"
)

func main() {
	cfg, log := parseFlags()
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := costpatcher.NewApp(log, cfg).Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}
