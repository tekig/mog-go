package welcomevoice

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/pion/webrtc/v4/pkg/media/oggreader"
)

const (
	voiceExtension = ".ogg"
	voiceMaxSize   = 5 * 1024 * 1024
)

type WelcomeVoice struct {
	config Config
	client *discordgo.Session
	logger *log.Logger
	mu     sync.Mutex

	shutdown []func() error
}

func New(config Config, client *discordgo.Session, logger *log.Logger) (*WelcomeVoice, error) {
	logger.SetPrefix("[Welcome Voice]: ")

	w := &WelcomeVoice{
		config: config,
		client: client,
		logger: logger,
	}

	if err := os.MkdirAll(config.VoiceDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("mkdir voice dir: %w", err)
	}

	cancelConnect := w.client.AddHandler(w.onConnect)
	w.shutdown = append(w.shutdown, func() error {
		cancelConnect()
		return nil
	})
	cancelMessageCreate := w.client.AddHandler(w.onMessageCreate)
	w.shutdown = append(w.shutdown, func() error {
		cancelMessageCreate()
		return nil
	})

	if err := w.loadMessage(); err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}

	return w, nil
}

func (w *WelcomeVoice) Run(ctx context.Context) error {
	<-ctx.Done()

	var errs []error
	for _, s := range w.shutdown {
		if err := s(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (w *WelcomeVoice) loadMessage() error {
	var beforeID string
	for {
		messages, err := w.client.ChannelMessages(w.config.ChannelID, 100, beforeID, "", "")
		if err != nil {
			return fmt.Errorf("channel messages: %w", err)
		}
		if len(messages) == 0 {
			return nil
		}
		beforeID = messages[len(messages)-1].ID

		for _, m := range messages {
			_, err := os.Stat(w.pathSoundData(m.Author.ID))
			if errors.Is(err, os.ErrNotExist) {
				if err := w.prepareSound(m); err != nil {
					w.logger.Printf("load message: prepare: %s", err.Error())

					if err := w.client.ChannelMessageDelete(m.ChannelID, m.ID); err != nil {
						return fmt.Errorf("remove message: %w", err)
					}

					continue
				}
			} else if err != nil {
				return fmt.Errorf("stat file: %w", err)
			}
		}
	}
}

func (w *WelcomeVoice) prepareSound(m *discordgo.Message) error {
	if len(m.Attachments) != 1 {
		return ErrAttachmentsNotFound
	}

	attach := m.Attachments[0]

	if attach.Size > voiceMaxSize {
		return ErrVoiceTooLarge
	}

	path, err := w.downloadSound(attach)
	if err != nil {
		return fmt.Errorf("download sound: %w", err)
	}
	defer os.Remove(path)

	if err := w.convertSound(path, w.pathSoundData(m.Author.ID)); err != nil {
		return fmt.Errorf("conver sound: %w", err)
	}

	if err := w.client.MessageReactionAdd(m.ChannelID, m.ID, w.config.Emoji); err != nil {
		return fmt.Errorf("reaction add: %w", err)
	}

	return nil
}

func (w *WelcomeVoice) downloadSound(attach *discordgo.MessageAttachment) (string, error) {
	exts, err := mime.ExtensionsByType(attach.ContentType)
	if err != nil {
		return "", fmt.Errorf("extension %s: %w", attach.ContentType, err)
	}
	if len(exts) < 1 {
		return "", fmt.Errorf("extension %s: %w", attach.ContentType, ErrExtensionNotFound)
	}

	f, err := os.CreateTemp("", "mog-*"+exts[0])
	if err != nil {
		return "", fmt.Errorf("crate temp: %w", err)
	}
	defer f.Close()

	r, err := http.Get(attach.ProxyURL)
	if err != nil {
		return "", fmt.Errorf("get: %w", err)
	}
	defer r.Body.Close()

	if _, err := io.Copy(f, r.Body); err != nil {
		return "", fmt.Errorf("download: %w", err)
	}

	return f.Name(), nil
}

func (w *WelcomeVoice) convertSound(from, to string) error {
	cmd := exec.Command("ffmpeg", "-y", "-i", from, "-c:a", "libopus", "-page_duration", "20000", to)

	if output, err := cmd.Output(); err != nil {
		return fmt.Errorf("ffmpeg: %s, %w", string(output), err)
	}

	return nil
}

func (w *WelcomeVoice) pathSoundData(userID string) string {
	return path.Join(w.config.VoiceDir, userID+voiceExtension)
}

func (w *WelcomeVoice) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.ChannelID != w.config.ChannelID {
		return
	}

	if err := w.prepareSound(m.Message); err != nil {
		w.logger.Printf("message create: prepare sound: %s", err.Error())
		_ = w.client.ChannelMessageDelete(m.ChannelID, m.ID)
	}
}

func (w *WelcomeVoice) onConnect(_ *discordgo.Session, u *discordgo.VoiceStateUpdate) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.play(u); err != nil {
		w.logger.Printf("on connect: play: %s", err.Error())
	}
}

func (w *WelcomeVoice) play(update *discordgo.VoiceStateUpdate) error {
	if update.BeforeUpdate != nil {
		return nil
	}

	f, err := os.Open(w.pathSoundData(update.UserID))
	if err != nil {
		return fmt.Errorf("open file: %w", err)
	}
	defer f.Close()

	voice, err := w.client.ChannelVoiceJoin(update.GuildID, update.ChannelID, false, true)
	if err != nil {
		return fmt.Errorf("channel voice join: %w", err)
	}
	defer func() { _ = voice.Disconnect() }()

	reader, _, err := oggreader.NewWith(f)
	if err != nil {
		return fmt.Errorf("ogg reader: %w", err)
	}

	t := time.NewTimer(w.config.VoiceDuration.Duration)
	for {
		data, _, err := reader.ParseNextPage()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("parse ogg: %w", err)
		}

		select {
		case voice.OpusSend <- data:
		case <-t.C:
			return nil
		}
	}
}
