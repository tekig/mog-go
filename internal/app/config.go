package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path"

	boommessage "github.com/tekig/mog-go/internal/boom-message"
	welcomevoice "github.com/tekig/mog-go/internal/welcome-voice"
)

type Config struct {
	Token        string              `json:"token,omitempty"`
	Store        string              `json:"store,omitempty"`
	WelcomeVoice welcomevoice.Config `json:"welcome_voice,omitempty"`
	BoomMessage  boommessage.Config  `json:"boom_message,omitempty"`
}

func NewConfig() (*Config, error) {
	const defaultPaht = "cfg/config.json"
	p := os.Getenv("CONFIG")
	if p == "" {
		p = defaultPaht
	}

	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("open: %w", err)
	}

	var config Config
	if err := json.NewDecoder(f).Decode(&config); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	if config.WelcomeVoice.VoiceDir == "" {
		config.WelcomeVoice.VoiceDir = path.Join(config.Store, "welcome-voice")
	}

	if config.BoomMessage.MessageDir == "" {
		config.BoomMessage.MessageDir = path.Join(config.Store, "boom-message")
	}

	return &config, nil
}
