package domain

import "context"

// FileService handles file operations
type FileService interface {
	ListProjectFiles() ([]string, error)
	ReadFile(path string) (string, error)
	ReadFileLines(path string, startLine, endLine int) (string, error)
	ValidateFile(path string) error
	GetFileInfo(path string) (FileInfo, error)
}

// ImageService handles image operations including loading and encoding
type ImageService interface {
	// ReadImageFromFile reads an image from a file path and returns it as a base64 attachment
	ReadImageFromFile(filePath string) (*ImageAttachment, error)
	// ReadImageFromBinary reads an image from binary data and returns it as a base64 attachment
	ReadImageFromBinary(imageData []byte, filename string) (*ImageAttachment, error)
	// ReadImageFromURL fetches an image from a URL and returns it as a base64 attachment
	ReadImageFromURL(imageURL string) (*ImageAttachment, error)
	// CreateDataURL creates a data URL from an image attachment
	CreateDataURL(attachment *ImageAttachment) string
	// IsImageFile checks if a file is a supported image format
	IsImageFile(filePath string) bool
	// IsImageURL checks if a string is a valid image URL
	IsImageURL(urlStr string) bool
	// IsImageModel reports whether the model generates images rather than text
	IsImageModel(model string) bool
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

// FileInfo contains file metadata
type FileInfo struct {
	Path  string
	Size  int64
	IsDir bool
}
