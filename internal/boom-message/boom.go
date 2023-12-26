package boommessage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"reflect"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

const (
	messageName = "messages.json"
)

type BoomMessage struct {
	config     Config
	client     *discordgo.Session
	logger     *log.Logger
	mu         sync.Mutex
	repository map[string]time.Time // map[ID]birth

	shutdown []func() error
}

func New(config Config, client *discordgo.Session, logger *log.Logger) (*BoomMessage, error) {
	logger.SetPrefix("[Boom Message]: ")

	b := &BoomMessage{
		config:     config,
		client:     client,
		logger:     logger,
		repository: make(map[string]time.Time),
	}

	if err := os.MkdirAll(config.MessageDir, os.ModePerm); err != nil {
		return nil, fmt.Errorf("mkdir message dir: %w", err)
	}

	if err := b.loadMessage(); err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}

	cancelMessageCreate := b.client.AddHandler(b.onMessageCreate)
	b.shutdown = append(b.shutdown, func() error {
		cancelMessageCreate()
		return nil
	})
	cancelReactionRemoveAll := b.client.AddHandler(b.onReactionRemoveAll)
	b.shutdown = append(b.shutdown, func() error {
		cancelReactionRemoveAll()
		return nil
	})
	cancelReactionRemove := b.client.AddHandler(b.onReactionRemove)
	b.shutdown = append(b.shutdown, func() error {
		cancelReactionRemove()
		return nil
	})
	cancelMessageDeleteBulk := b.client.AddHandler(b.onMessageDeleteBulk)
	b.shutdown = append(b.shutdown, func() error {
		cancelMessageDeleteBulk()
		return nil
	})
	cancelMessageDelete := b.client.AddHandler(b.onMessageDelete)
	b.shutdown = append(b.shutdown, func() error {
		cancelMessageDelete()
		return nil
	})
	cancelReactionAdd := b.client.AddHandler(b.onReactionAdd)
	b.shutdown = append(b.shutdown, func() error {
		cancelReactionAdd()
		return nil
	})

	return b, nil
}

func (b *BoomMessage) Run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer cancel()
		defer wg.Done()
		b.runTicker(ctx)
	}()

	wg.Add(1)
	go func() {
		defer cancel()
		defer wg.Done()
		b.runBoom(ctx)
	}()

	wg.Wait()

	var errs []error
	for _, s := range b.shutdown {
		if err := s(); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func (b *BoomMessage) loadMessage() error {
	prevRepo, err := b.readRepository()
	if errors.Is(err, os.ErrNotExist) {
		prevRepo = make(map[string]time.Time)
	} else if err != nil {
		return fmt.Errorf("read repository: %w", err)
	}

	var (
		beforeID string
		nextRepo = make(map[string]time.Time)
	)
	for {
		messages, err := b.client.ChannelMessages(b.config.ChannelID, 100, beforeID, "", "")
		if err != nil {
			return fmt.Errorf("channel messages: %w", err)
		}
		if len(messages) == 0 {
			break
		}
		beforeID = messages[len(messages)-1].ID

		for _, m := range messages {
			if len(m.Reactions) > 0 {
				continue
			}

			birth, ok := prevRepo[m.ID]
			if !ok {
				birth = time.Now()
			}

			nextRepo[m.ID] = birth
		}
	}

	b.mu.Lock()
	b.repository = nextRepo
	b.mu.Unlock()

	return nil
}

func (b *BoomMessage) readRepository() (map[string]time.Time, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	f, err := os.Open(path.Join(b.config.MessageDir, messageName))
	if err != nil {
		return nil, fmt.Errorf("load message: %w", err)
	}
	defer f.Close()

	var messages = make(map[string]time.Time)
	if err := json.NewDecoder(f).Decode(&messages); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	return messages, nil
}

func (b *BoomMessage) runTicker(ctx context.Context) {
	tick := time.NewTicker(b.config.SaveAfter.Duration)
	defer tick.Stop()

	var prev = make(map[string]time.Time)
	equal := func() bool {
		b.mu.Lock()
		defer b.mu.Unlock()

		if !reflect.DeepEqual(prev, b.repository) {
			return false
		}

		prev = make(map[string]time.Time)
		for k, v := range b.repository {
			prev[k] = v
		}

		return true
	}

	for {
		select {
		case <-tick.C:
			if equal() {
				continue
			}

			if err := b.writeRepository(); err != nil {
				b.logger.Printf("tick write repository: %s", err.Error())
			}
		case <-ctx.Done():
			if err := b.writeRepository(); err != nil {
				b.logger.Printf("context write repository: %s", err.Error())
			}
		}
	}
}

func (b *BoomMessage) runBoom(ctx context.Context) {
	laterBirth := func() (string, time.Time) {
		b.mu.Lock()
		defer b.mu.Unlock()

		var (
			laterTime = time.Now()
			laterID   string
		)
		for id, t := range b.repository {
			if t.Before(laterTime) {
				laterID = id
				laterTime = t
			}
		}

		return laterID, laterTime
	}

	for {
		messageID, birth := laterBirth()

		d := time.Until(birth)
		d = d + b.config.DeadAfter.Duration

		timer := time.After(d)

		select {
		case <-timer:
			b.mu.Lock()
			_, ok := b.repository[messageID]
			b.mu.Unlock()
			if !ok {
				continue
			}

			if err := b.client.ChannelMessageDelete(b.config.ChannelID, messageID); err != nil {
				b.logger.Printf("run boom: channel message delete: %s", err.Error())
				continue
			}

			b.mu.Lock()
			delete(b.repository, messageID)
			b.mu.Unlock()
		case <-ctx.Done():
			return
		}
	}
}

func (b *BoomMessage) writeRepository() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	f, err := os.OpenFile(path.Join(b.config.MessageDir, messageName), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return fmt.Errorf("load message: %w", err)
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(b.repository); err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	return nil
}

func (b *BoomMessage) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.ChannelID != b.config.ChannelID {
		return
	}

	b.mu.Lock()
	b.repository[m.Message.ID] = time.Now()
	b.mu.Unlock()
}

func (b *BoomMessage) onReactionAdd(s *discordgo.Session, r *discordgo.MessageReactionAdd) {
	if r.ChannelID != b.config.ChannelID {
		return
	}

	b.mu.Lock()
	delete(b.repository, r.MessageID)
	b.mu.Unlock()
}

func (b *BoomMessage) onReactionRemoveAll(s *discordgo.Session, r *discordgo.MessageReactionRemoveAll) {
	if r.ChannelID != b.config.ChannelID {
		return
	}

	b.mu.Lock()
	b.repository[r.ChannelID] = time.Now()
	b.mu.Unlock()
}

func (b *BoomMessage) onReactionRemove(s *discordgo.Session, r *discordgo.MessageReactionRemove) {
	if r.ChannelID != b.config.ChannelID {
		return
	}

	m, err := b.client.ChannelMessage(r.ChannelID, r.MessageID)
	if err != nil {
		b.logger.Printf("on reaction remove: channel message: %s", err.Error())
		return
	}

	if len(m.Reactions) > 0 {
		return
	}

	b.mu.Lock()
	b.repository[r.ChannelID] = time.Now()
	b.mu.Unlock()
}

func (b *BoomMessage) onMessageDeleteBulk(s *discordgo.Session, m *discordgo.MessageDeleteBulk) {
	if m.ChannelID != b.config.ChannelID {
		return
	}

	b.mu.Lock()
	delete(b.repository, m.ChannelID)
	b.mu.Unlock()
}

func (b *BoomMessage) onMessageDelete(s *discordgo.Session, m *discordgo.MessageDelete) {
	if m.ChannelID != b.config.ChannelID {
		return
	}

	b.mu.Lock()
	delete(b.repository, m.ChannelID)
	b.mu.Unlock()
}
