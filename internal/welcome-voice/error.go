package welcomevoice

import "errors"

var (
	ErrExtensionNotFound   = errors.New("extension not found")
	ErrVoiceTooLarge       = errors.New("voice too large")
	ErrAttachmentsNotFound = errors.New("attachments not found")
)
