package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/tekig/mog-go/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	if err := run(ctx); err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	app, err := app.New()
	if err != nil {
		return fmt.Errorf("app: %w", err)
	}

	if err := app.Run(ctx); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	return nil
}
