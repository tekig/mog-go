package app

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/bwmarrin/discordgo"
	boommessage "github.com/tekig/mog-go/internal/boom-message"
	welcomevoice "github.com/tekig/mog-go/internal/welcome-voice"
)

type App struct {
	runners  []func(context.Context) error
	shutdown []func() error

	logger *log.Logger
}

func New() (*App, error) {
	logger := func() *log.Logger {
		return log.New(os.Stderr, "", log.LstdFlags)
	}

	app := &App{
		logger: logger(),
	}
	app.logger.SetPrefix("[App]: ")

	config, err := NewConfig()
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	client, err := discordgo.New("Bot " + config.Token)
	if err != nil {
		return nil, fmt.Errorf("discord: %w", err)
	}
	app.shutdown = append(app.shutdown, func() error {
		return fmt.Errorf("close discord: %w", client.Close())
	})

	client.Identify.Intents = discordgo.MakeIntent(
		discordgo.IntentGuildMessages |
			discordgo.IntentGuildVoiceStates |
			discordgo.IntentGuildMessageReactions,
	)

	if err := client.Open(); err != nil {
		return nil, fmt.Errorf("client open: %w", err)
	}

	welcome, err := welcomevoice.New(config.WelcomeVoice, client, logger())
	if err != nil {
		return nil, fmt.Errorf("welcome voice: %w", err)
	}
	app.runners = append(app.runners, welcome.Run)

	boom, err := boommessage.New(config.BoomMessage, client, logger())
	if err != nil {
		return nil, fmt.Errorf("boom message: %w", err)
	}
	app.runners = append(app.runners, boom.Run)

	return app, nil
}

func (a *App) Run(ctx context.Context) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	wg.Add(len(a.runners))
	for _, r := range a.runners {
		r := r
		go func() {
			defer cancel()
			defer wg.Done()

			if err := r(ctx); err != nil {
				mu.Lock()
				defer mu.Unlock()

				errs = append(errs, err)
			}
		}()
	}

	a.logger.Print("running...")
	// Bug(t): fall of one runner cannot be determined
	wg.Wait()

	for _, s := range a.shutdown {
		if err := s(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}
