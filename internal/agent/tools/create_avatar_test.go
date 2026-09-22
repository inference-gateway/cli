package tools

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	assert "github.com/stretchr/testify/assert"
	require "github.com/stretchr/testify/require"

	agentdomainmocks "github.com/inference-gateway/cli/tests/mocks/agentdomain"

	config "github.com/inference-gateway/cli/config"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	avatars "github.com/inference-gateway/cli/internal/avatars"
)

// newTestAvatarTool isolates HOME (the avatar library and artifacts dir) and
// the working directory, and stubs EditImage to write a generated PNG.
func newTestAvatarTool(t *testing.T) (*CreateAvatarTool, *agentdomainmocks.FakeImageService, string) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	workDir := t.TempDir()
	t.Chdir(workDir)

	cfg := config.DefaultConfig()
	cfg.Prompts = *config.DefaultPromptsConfig()
	cfg.TextToVideo.Enabled = true
	cfg.TextToVideo.CreateAvatar = true
	cfg.Tools.ImageEdit.Enabled = true
	cfg.Tools.ImageEdit.Model = "openai/gpt-image-2"

	images := &agentdomainmocks.FakeImageService{}
	scratch := t.TempDir()
	images.EditImageStub = func(_ context.Context, _, _, _, _, _, _ string) (string, error) {
		out, err := os.CreateTemp(scratch, "edit-*.png")
		if err != nil {
			return "", err
		}
		defer func() { _ = out.Close() }()
		_, err = out.Write(minimalPNG())
		return out.Name(), err
	}
	return NewCreateAvatarTool(cfg, images), images, workDir
}

func TestCreateAvatarTool_Validate(t *testing.T) {
	tool, _, workDir := newTestAvatarTool(t)
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "me.png"), minimalPNG(), 0o600))

	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{"minimal", map[string]any{"name": "presenter", "photo": "me.png"}, ""},
		{"all knobs", map[string]any{"name": "p", "photo": "me.png", "angles": []any{"left-profile"}, "quality": "low", "size": "1024x1024"}, ""},
		{"no angles", map[string]any{"name": "p", "photo": "me.png", "angles": []any{}}, ""},
		{"missing name", map[string]any{"photo": "me.png"}, "name is required"},
		{"missing photo", map[string]any{"name": "p"}, "photo is required"},
		{"absolute photo", map[string]any{"name": "p", "photo": filepath.Join(workDir, "me.png")}, "invalid photo path"},
		{"photo traversal", map[string]any{"name": "p", "photo": "../me.png"}, "invalid photo path"},
		{"nested photo", map[string]any{"name": "p", "photo": "sub/me.png"}, "invalid photo path"},
		{"unsupported photo", map[string]any{"name": "p", "photo": "me.gif"}, "must be a .png"},
		{"unknown angle", map[string]any{"name": "p", "photo": "me.png", "angles": []any{"upside-down"}}, "unknown angle"},
		{"angles not an array", map[string]any{"name": "p", "photo": "me.png", "angles": "left-profile"}, "must be an array"},
		{"bad quality", map[string]any{"name": "p", "photo": "me.png", "quality": "ultra"}, "quality must be one of"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tool.Validate(tt.args)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestCreateAvatarTool_Execute(t *testing.T) {
	t.Run("builds the avatar from a session artifact with the default angles", func(t *testing.T) {
		tool, images, _ := newTestAvatarTool(t)
		ctx := context.WithValue(context.Background(), agentdomain.SessionIDKey, "sess-1")
		artifacts := tool.config.SessionArtifactsDir("sess-1")
		require.NoError(t, os.MkdirAll(artifacts, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(artifacts, "portrait.png"), minimalPNG(), 0o600))

		res, err := tool.Execute(ctx, map[string]any{"name": "presenter", "photo": "portrait.png"})
		require.NoError(t, err)
		require.True(t, res.Success, res.Error)

		data := res.Data.(map[string]any)
		assert.Equal(t, "presenter", data["name"])
		assert.Equal(t, []string{"01-front.png", "02-three-quarter-left.png", "03-three-quarter-right.png"}, data["images"])
		assert.DirExists(t, filepath.Join(avatars.Dir(), "presenter"))

		require.Equal(t, 2, images.EditImageCallCount())
		_, model, _, imagePath, quality, size, mask := images.EditImageArgsForCall(0)
		assert.Equal(t, "openai/gpt-image-2", model)
		assert.Equal(t, filepath.Join(avatars.Dir(), "presenter", "01-front.png"), imagePath)
		assert.Equal(t, "high", quality)
		assert.Equal(t, "1024x1536", size)
		assert.Empty(t, mask)
		assert.Contains(t, tool.FormatForLLM(res), `pass avatar "presenter" to TextToVideo`)
	})

	t.Run("no angles stores the photo without calling the provider", func(t *testing.T) {
		tool, images, workDir := newTestAvatarTool(t)
		tool.config.Tools.ImageEdit.Enabled = false // not needed without angles
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "me.png"), minimalPNG(), 0o600))

		res, err := tool.Execute(context.Background(), map[string]any{"name": "presenter", "photo": "me.png", "angles": []any{}})
		require.NoError(t, err)
		require.True(t, res.Success, res.Error)
		assert.Equal(t, []string{"01-front.png"}, res.Data.(map[string]any)["images"])
		assert.Equal(t, 0, images.EditImageCallCount())
	})

	t.Run("an existing avatar fails before any provider call", func(t *testing.T) {
		tool, images, workDir := newTestAvatarTool(t)
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "me.png"), minimalPNG(), 0o600))
		writeAvatar(t, "presenter", "01-front.png")

		res, err := tool.Execute(context.Background(), map[string]any{"name": "presenter", "photo": "me.png"})
		require.NoError(t, err)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "already exists")
		assert.Equal(t, 0, images.EditImageCallCount())
	})

	t.Run("angles with image edit disabled fail clearly", func(t *testing.T) {
		tool, images, workDir := newTestAvatarTool(t)
		tool.config.Tools.ImageEdit.Enabled = false
		require.NoError(t, os.WriteFile(filepath.Join(workDir, "me.png"), minimalPNG(), 0o600))

		res, err := tool.Execute(context.Background(), map[string]any{"name": "presenter", "photo": "me.png"})
		require.NoError(t, err)
		assert.False(t, res.Success)
		assert.Contains(t, res.Error, "tools.image_edit")
		assert.Equal(t, 0, images.EditImageCallCount())
		assert.NoDirExists(t, filepath.Join(avatars.Dir(), "presenter"))
	})

	t.Run("a missing photo or a directory is not found", func(t *testing.T) {
		tool, _, workDir := newTestAvatarTool(t)
		require.NoError(t, os.Mkdir(filepath.Join(workDir, "dir.png"), 0o755))

		for _, photo := range []string{"nope.png", "dir.png"} {
			res, err := tool.Execute(context.Background(), map[string]any{"name": "presenter", "photo": photo})
			require.NoError(t, err)
			assert.False(t, res.Success)
			assert.Contains(t, res.Error, "not found")
		}
	})
}

func TestCreateAvatarTool_RegistryGating(t *testing.T) {
	for _, tt := range []struct {
		name                string
		video, createAvatar bool
		want                bool
	}{
		{"both off", false, false, false},
		{"text_to_video only", true, false, false},
		{"create_avatar without text_to_video", false, true, false},
		{"both on", true, true, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.TextToVideo.Enabled = tt.video
			cfg.TextToVideo.CreateAvatar = tt.createAvatar
			registry := NewRegistry(cfg, &agentdomainmocks.FakeImageService{}, nil, nil, nil, &agentdomainmocks.FakeVideoService{}, nil, nil, nil, nil, nil)

			if tt.want {
				assert.Contains(t, registry.ListAvailableTools(), "CreateAvatar")
				return
			}
			assert.NotContains(t, registry.ListAvailableTools(), "CreateAvatar")
			_, err := registry.GetTool("CreateAvatar")
			assert.Error(t, err)
		})
	}
}

func TestCreateAvatarTool_Definition(t *testing.T) {
	tool, _, _ := newTestAvatarTool(t)
	def := tool.Definition()

	assert.Equal(t, "CreateAvatar", def.Function.Name)
	require.NotNil(t, def.Function.Description)
	assert.Contains(t, *def.Function.Description, "never overwrites")
	properties := (*def.Function.Parameters)["properties"].(map[string]any)
	for _, name := range []string{"name", "photo", "angles", "quality", "size"} {
		assert.Contains(t, properties, name)
	}
	assert.Equal(t, "Avatar creation failed", tool.FormatPreview(nil))
}
