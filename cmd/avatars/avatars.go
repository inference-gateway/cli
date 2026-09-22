package avatarscmd

import (
	"encoding/json"
	"fmt"
	"strings"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	avatars "github.com/inference-gateway/cli/internal/avatars"
)

// NewCommand constructs the avatars command tree.
func NewCommand(renderer *output.Renderer) *cobra.Command {
	avatarsCmd := &cobra.Command{
		Use:   "avatars",
		Short: "Manage the avatar library for TextToVideo",
		Long: `List and delete the avatars TextToVideo renders lip-synced clips from.

An avatar is a folder under ~/.infer/avatars/<name>/ holding one or more
portrait images (.png, .jpg, .jpeg, .webp) of the same person, e.g. shots
from different angles. Lip-sync models receive the first image in sort order.
To add an avatar, create the folder and copy the images into it.`,
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
	avatarsCmd.AddCommand(listCmd, deleteCmd)
	return avatarsCmd
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
