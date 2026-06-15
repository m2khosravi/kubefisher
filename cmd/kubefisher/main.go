package main

import (
	"log/slog"
	"os"

	"github.com/m2khosravi/kubefisher/internal/cli/kubefisher"
)

func main() {
	if err := kubefisher.Execute(); err != nil {
		slog.Error("kubefisher", "err", err)
		os.Exit(1)
	}
}
