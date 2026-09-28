package download

import "testing"

func TestFilenameFromURL(t *testing.T) {
	tests := []struct {
		name        string
		url         string
		contentType string
		want        string
	}{
		{
			name:        "URL with extension keeps it",
			url:         "https://example.com/photo.png",
			contentType: "",
			want:        "photo.png",
		},
		{
			name:        "extensionless URL with image/png gets .png",
			url:         "https://github.com/user-attachments/assets/6a8fffe9-d795-499f-8ef5-e470a97a14af",
			contentType: "image/png",
			want:        "6a8fffe9-d795-499f-8ef5-e470a97a14af.png",
		},
		{
			name:        "extensionless URL with image/jpeg gets extension from mime.ExtensionsByType",
			url:         "https://example.com/photo",
			contentType: "image/jpeg",
			want:        "photo.jfif",
		},
		{
			name:        "extensionless URL with unknown type gets .bin from mime.ExtensionsByType",
			url:         "https://example.com/file",
			contentType: "application/octet-stream",
			want:        "file.bin",
		},
		{
			name:        "extensionless URL with empty content type gets .dat",
			url:         "https://example.com/file",
			contentType: "",
			want:        "file.dat",
		},
		{
			name:        "URL with query string stripped",
			url:         "https://example.com/file.png?w=800",
			contentType: "",
			want:        "file.png",
		},
		{
			name:        "URL with fragment stripped",
			url:         "https://example.com/file.png#section",
			contentType: "",
			want:        "file.png",
		},
		{
			name:        "content type with charset parameter",
			url:         "https://example.com/photo",
			contentType: "image/png; charset=utf-8",
			want:        "photo.png",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilenameFromURL(tt.url, tt.contentType)
			if got != tt.want {
				t.Errorf("FilenameFromURL(%q, %q) = %q, want %q", tt.url, tt.contentType, got, tt.want)
			}
		})
	}
}
