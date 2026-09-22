package avatarscmd

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	cobra "github.com/spf13/cobra"

	sdk "github.com/inference-gateway/sdk"

	output "github.com/inference-gateway/cli/cmd/output"
	runtime "github.com/inference-gateway/cli/cmd/runtime"
	agentdomain "github.com/inference-gateway/cli/internal/agent/domain"
	avatars "github.com/inference-gateway/cli/internal/avatars"
	container "github.com/inference-gateway/cli/internal/container"
)

// qualities are the image-edit quality levels the gateway accepts.
var qualities = []string{
	string(sdk.CreateImageEditMultipartBodyQualityAuto),
	string(sdk.CreateImageEditMultipartBodyQualityLow),
	string(sdk.CreateImageEditMultipartBodyQualityMedium),
	string(sdk.CreateImageEditMultipartBodyQualityHigh),
	string(sdk.CreateImageEditMultipartBodyQualityStandard),
}

// NewCommand constructs the avatars command tree.
func NewCommand(state *runtime.State, renderer *output.Renderer) *cobra.Command {
	avatarsCmd := &cobra.Command{
		Use:   "avatars",
		Short: "Manage the avatar library for TextToVideo",
		Long: `Create, list and delete the avatars TextToVideo renders lip-synced clips from.

An avatar is a folder under ~/.infer/avatars/<name>/ holding one or more
portrait images (.png, .jpg, .jpeg, .webp) of the same person, e.g. shots
from different angles. Lip-sync models receive the first image in sort order.`,
	}
	createCmd := &cobra.Command{
		Use:   "create <name> --from <photo>",
		Short: "Create an avatar from a photo, generating extra angles",
		Long: `Create ~/.infer/avatars/<name>/ from a front-facing photo. The photo is
copied in as the primary image (01-front) and each --angles view is generated
from it through the gateway's image edit API with tools.image_edit.model,
in parallel. Pass --angles "" to only copy the photo.

The photo is sent to the provider behind tools.image_edit.model (OpenAI by
default) for every generated angle.

  infer avatars create presenter --from ~/Pictures/me.jpg
  infer avatars create presenter --from me.jpg --angles three-quarter-left,left-profile
  infer avatars create presenter --from me.jpg --quality medium --size 1024x1024`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return createAvatar(cmd, state, renderer, args[0])
		},
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List avatars",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("format")
			return listAvatars(renderer, format)
		},
	}
	deleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete an avatar and all of its images",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := avatars.Delete(avatars.Dir(), args[0]); err != nil {
				return err
			}
			fmt.Printf("%s Deleted avatar %s\n", renderer.StatusIcon(true), args[0])
			return nil
		},
	}
	listCmd.Flags().StringP("format", "f", "text", "Output format (text|json)")
	createCmd.Flags().String("from", "", "Front-facing photo to build the avatar from (.png, .jpg, .jpeg, .webp)")
	createCmd.Flags().StringSlice("angles", avatars.DefaultAngles,
		"Views to generate: three-quarter-left, three-quarter-right, left-profile, right-profile (\"\" for none)")
	createCmd.Flags().String("quality", string(sdk.CreateImageEditMultipartBodyQualityHigh), "Generated image quality ("+strings.Join(qualities, "|")+")")
	createCmd.Flags().String("size", string(sdk.ImageSize1024X1536), "Generated image size as WIDTHxHEIGHT, or auto")
	_ = createCmd.MarkFlagRequired("from")
	avatarsCmd.AddCommand(createCmd, listCmd, deleteCmd)
	return avatarsCmd
}

func createAvatar(cmd *cobra.Command, state *runtime.State, renderer *output.Renderer, name string) error {
	photo, _ := cmd.Flags().GetString("from")
	quality, _ := cmd.Flags().GetString("quality")
	size, _ := cmd.Flags().GetString("size")
	rawAngles, _ := cmd.Flags().GetStringSlice("angles")
	angles := slices.DeleteFunc(slices.Clone(rawAngles), func(a string) bool { return strings.TrimSpace(a) == "" })

	if !slices.Contains(qualities, quality) {
		return fmt.Errorf("invalid --quality %q (choose from %s)", quality, strings.Join(qualities, ", "))
	}

	var edit avatars.EditFunc
	if len(angles) > 0 {
		cfg := state.Config()
		model := cfg.Tools.ImageEdit.Model
		if !cfg.Tools.ImageEdit.Enabled || model == "" {
			return fmt.Errorf("generating angles uses the ImageEdit model: set tools.image_edit.enabled and tools.image_edit.model, or pass --angles \"\" to only copy the photo")
		}
		// ponytail: the gateway starts on the first edit, after avatars.Create
		// has validated the name, photo and angles, so a typo never spins one up.
		imageService := sync.OnceValues(func() (agentdomain.ImageService, error) {
			fmt.Printf("Generating %d angle(s) with %s (%s quality, %s)...\n", len(angles), model, quality, size)
			services := container.NewServiceContainer(cfg)
			if err := services.GetGatewayManager().EnsureStarted(); err != nil {
				return nil, fmt.Errorf("failed to start inference gateway: %w", err)
			}
			return services.GetImageService(), nil
		})
		edit = func(ctx context.Context, prompt, imagePath string) (string, error) {
			images, err := imageService()
			if err != nil {
				return "", err
			}
			return images.EditImage(ctx, model, prompt, imagePath, quality, size, "")
		}
	}

	avatar, err := avatars.Create(cmd.Context(), avatars.Dir(), name, photo, angles, edit)
	if err != nil {
		return err
	}
	for _, image := range avatar.Images {
		fmt.Printf("  %s\n", image)
	}
	fmt.Printf("%s Created avatar %s (%d images)\n", renderer.StatusIcon(true), avatar.Name, len(avatar.Images))
	return nil
}

func listAvatars(renderer *output.Renderer, format string) error {
	dir := avatars.Dir()
	list, err := avatars.List(dir)
	if err != nil {
		return err
	}

	if format == "json" {
		data, err := json.MarshalIndent(append([]avatars.Avatar{}, list...), "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal avatars: %w", err)
		}
		fmt.Println(string(data))
		return nil
	}

	if len(list) == 0 {
		fmt.Printf("No avatars in %s.\n", dir)
		fmt.Println("Add one by creating a folder there with one or more portrait images.")
		return nil
	}

	fmt.Println(renderer.Title("Avatars"))
	fmt.Println(renderer.Field("Directory", dir))
	tbl := renderer.NewListTable("Name", "Images")
	for _, a := range list {
		tbl.Row(a.Name, strings.Join(a.Images, ", "))
	}
	fmt.Println(tbl.Render())
	return nil
}
