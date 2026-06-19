package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	devssh "github.com/scaryrawr/devssh"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)
	go func() {
		<-sigChan
		cancel()
	}()

	args := devssh.ParseArgs()
	if args.Logs {
		devssh.ListRecentLogFiles()
		return nil
	}

	cfg, err := devssh.LoadAppConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = devssh.AppConfig{}
	}

	if args.Host == "" {
		picked, err := devssh.SelectHost()
		if err != nil {
			return fmt.Errorf("select host: %w", err)
		}
		args.Host = picked
	}

	return devssh.Run(ctx, args.SessionOptions(cfg))
}
