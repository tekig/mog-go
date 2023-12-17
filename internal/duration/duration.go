package duration

import (
	"strings"
	"time"
)

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalJSON(data []byte) error {
	raw := string(data)

	v, err := time.ParseDuration(strings.Trim(raw, "\""))
	if err != nil {
		return err
	}

	d.Duration = v

	return nil
}
