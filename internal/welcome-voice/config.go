package welcomevoice

import (
	"github.com/tekig/mog-go/internal/duration"
)

type Config struct {
	ChannelID     string            `json:"channel_id,omitempty"`
	VoiceDir      string            `json:"voice_dir,omitempty"`
	VoiceDuration duration.Duration `json:"voice_duration,omitempty"`
	Emoji         string            `json:"emoji,omitempty"`
}
