package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	_ "time/tzdata"

	"universeatwar/internal/cli"
	"universeatwar/internal/config"
)

var version = "dev"

func main() {
	configuration, err := config.Load(os.Getenv)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	runner := cli.Runner{
		Stdout:          os.Stdout,
		Stderr:          os.Stderr,
		Version:         version,
		DefaultDatabase: configuration.DatabasePath,
		DefaultListen:   configuration.ListenAddress,
		LogLevel:        configuration.LogLevel,
	}
	os.Exit(runner.Run(ctx, os.Args[1:]))
}
