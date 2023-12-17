package boommessage

import "github.com/tekig/mog-go/internal/duration"

type Config struct {
	ChannelID  string            `json:"channel_id,omitempty"`
	DeadAfter  duration.Duration `json:"dead_after,omitempty"`
	SaveAfter  duration.Duration `json:"save_after,omitempty"`
	MessageDir string            `json:"message_dir,omitempty"`
}
