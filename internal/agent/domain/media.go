package domain

import "context"

// FileService handles file operations
type FileService interface {
	ListProjectFiles() ([]string, error)
	ReadFile(path string) (string, error)
	ValidateFile(path string) error
}

// ImageService handles image operations including loading and encoding
type ImageService interface {
	// ReadImageFromFile reads an image from a file path and returns it as a base64 attachment
	ReadImageFromFile(filePath string) (*ImageAttachment, error)
	// ReadImageFromBinary reads an image from binary data and returns it as a base64 attachment
	ReadImageFromBinary(imageData []byte, filename string) (*ImageAttachment, error)
	// ReadImageFromURL fetches an image from a URL and returns it as a base64 attachment
	ReadImageFromURL(imageURL string) (*ImageAttachment, error)
	// IsImageFile checks if a file is a supported image format
	IsImageFile(filePath string) bool
	// GenerateImage generates an image from prompt using model ("provider/name")
	// and returns the path of the saved file. A blank quality or size leaves the
	// provider's own default
	GenerateImage(ctx context.Context, model, prompt, quality, size string) (string, error)
	// EditImage edits the image at imagePath using prompt and model
	// ("provider/name") and returns the path of the saved file. A blank quality
	// or size leaves the provider's own default. A non-empty maskPath points to
	// a PNG whose transparent (alpha=0) areas mark the editable region; all
	// other pixels are preserved exactly.
	EditImage(ctx context.Context, model, prompt, imagePath, quality, size, maskPath string) (string, error)
	// CreateImageVariation creates a variation of the image at imagePath using
	// model ("provider/name") and returns the path of the saved file. A blank
	// size leaves the provider's own default
	CreateImageVariation(ctx context.Context, model, imagePath, size string) (string, error)
}

// SpeechService synthesizes speech through the gateway's Audio API, writing
// the audio to outPath; a non-empty voiceSamplePath is forwarded as a
// reference sample for zero-shot voice cloning.
type SpeechService interface {
	Synthesize(ctx context.Context, text, voiceSamplePath, outPath string) error
}

// MusicService composes a music clip from a text prompt through the gateway's
// Music API, writing the audio to outPath. A non-nil seconds caps the clip
// length in seconds; a non-nil instrumental requests a vocal-free clip. An
// omitted knob leaves the choice to the configured provider.
type MusicService interface {
	Compose(ctx context.Context, prompt, outPath string, seconds *float32, instrumental *bool) error
}

// SoundEffectService generates a short sound effect or ambience clip from a
// text prompt through the gateway's SFX API, writing WAV audio to outPath.
// A non-nil seconds caps the clip length in seconds; a non-nil loop requests
// a clip that loops seamlessly. An omitted knob leaves the choice to the
// configured provider.
type SoundEffectService interface {
	Generate(ctx context.Context, prompt, outPath string, seconds *float32, loop *bool) error
}

// VideoRequest is one video render: a text prompt (required unless the audio
// drives the render), the optional seconds and size passthroughs, the
// optional avatar portrait and driving audio for lip-synced talking clips,
// and the optional reference images of the subject (e.g. an avatar from
// several angles) that keep them consistent in a prompt render. The gateway
// rejects reference images together with a portrait.
type VideoRequest struct {
	Prompt         string
	Seconds        string
	Size           string
	AvatarPath     string
	AudioPath      string
	ReferencePaths []string
}

// IsAvatar reports whether the request is a lip-synced avatar render: the
// audio clip drives it, so it goes to the avatar model.
func (r VideoRequest) IsAvatar() bool {
	return r.AudioPath != ""
}

// VideoService renders a video clip through the gateway's Videos API,
// writing MP4 content to outPath. With an avatar and audio it renders a
// lip-synced talking clip; the render blocks until the gateway job
// completes, fails or the context ends.
type VideoService interface {
	Render(ctx context.Context, request VideoRequest, outPath string) error
}
