package modelscmd

import (
	"fmt"
	"io"

	cobra "github.com/spf13/cobra"

	output "github.com/inference-gateway/cli/cmd/output"
	models "github.com/inference-gateway/cli/internal/models"
	huggingface "github.com/inference-gateway/cli/internal/platform/huggingface"
)

const (
	defaultSearchLimit = 20
	bytesPerMegabyte   = 1 << 20
)

// NewCommand constructs the models command tree.
func NewCommand(renderer *output.Renderer) *cobra.Command {
	modelsCmd := &cobra.Command{
		Use:   "models",
		Short: "Search Hugging Face and manage downloaded model files",
		Long: `Search the Hugging Face Hub and download model repositories to this machine.

A downloaded repository is a folder under ~/.infer/models/huggingface/<owner>/<name>/
holding its files as the Hub lays them out. Only public repositories can be
downloaded.`,
	}
	searchCmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search the Hugging Face Hub for models, most downloaded first",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			limit, _ := cmd.Flags().GetInt("limit")
			format, _ := cmd.Flags().GetString("format")
			found, err := huggingface.NewClient().Search(cmd.Context(), args[0], limit)
			if err != nil {
				return err
			}
			return printSearch(cmd.OutOrStdout(), renderer, format, found)
		},
	}
	filesCmd := &cobra.Command{
		Use:   "files <owner/name>",
		Short: "List the files of a model on the Hub with their sizes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			format, _ := cmd.Flags().GetString("format")
			files, err := huggingface.NewClient().Files(cmd.Context(), huggingface.Repo{ID: args[0]})
			if err != nil {
				return err
			}
			return printFiles(cmd.OutOrStdout(), renderer, format, files)
		},
	}
	downloadCmd := &cobra.Command{
		Use:   "download <owner/name> [file...]",
		Short: "Download a model, or only the files named, from the Hub",
		Long: `Download a model repository into ~/.infer/models/huggingface/<owner>/<name>/.
Name files to fetch only those, which matters for a repository that holds
many variants of one model. A file that is already there is kept.

  infer models download openai/whisper-tiny
  infer models download TMElyralab/MuseTalk musetalkV15/unet.pth musetalkV15/musetalk.json`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			w := cmd.OutOrStdout()
			progress := func(line string) { fmt.Fprintln(w, line) }
			model, err := models.Download(cmd.Context(), huggingface.NewClient(), models.Dir(), args[0], args[1:], progress)
			if err != nil {
				return err
			}
			fmt.Fprintf(w, "%s Downloaded %s (%d files, %s) to %s\n", renderer.StatusIcon(true), model.ID, model.Files, megabytes(model.Bytes), model.Path)
			return nil
		},
	}
	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List downloaded models",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			format, _ := cmd.Flags().GetString("format")
			return printLibrary(cmd.OutOrStdout(), renderer, format)
		},
	}
	deleteCmd := &cobra.Command{
		Use:   "delete <owner/name>",
		Short: "Delete a downloaded model and all of its files",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := models.Delete(models.Dir(), args[0]); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s Deleted model %s\n", renderer.StatusIcon(true), args[0])
			return nil
		},
	}
	searchCmd.Flags().Int("limit", defaultSearchLimit, "Most results to return")
	output.AddFormatFlag(searchCmd, "text", "json")
	output.AddFormatFlag(filesCmd, "text", "json")
	output.AddFormatFlag(listCmd, "text", "json")
	modelsCmd.AddCommand(searchCmd, filesCmd, downloadCmd, listCmd, deleteCmd)
	return modelsCmd
}

func megabytes(bytes int64) string {
	return fmt.Sprintf("%.1f MB", float64(bytes)/bytesPerMegabyte)
}

func printSearch(w io.Writer, renderer *output.Renderer, format string, found []huggingface.Model) error {
	if format == "json" {
		return output.PrintJSON(w, append([]huggingface.Model{}, found...))
	}
	if len(found) == 0 {
		fmt.Fprintln(w, "No models found.")
		return nil
	}
	tbl := renderer.NewListTable("Model", "Task", "Downloads", "Likes")
	for _, m := range found {
		tbl.Row(m.ID, m.PipelineTag, fmt.Sprint(m.Downloads), fmt.Sprint(m.Likes))
	}
	fmt.Fprintln(w, tbl.Render())
	return nil
}

func printFiles(w io.Writer, renderer *output.Renderer, format string, files []huggingface.File) error {
	if format == "json" {
		return output.PrintJSON(w, append([]huggingface.File{}, files...))
	}
	tbl := renderer.NewListTable("File", "Size")
	for _, f := range files {
		tbl.Row(f.Path, megabytes(f.Size))
	}
	fmt.Fprintln(w, tbl.Render())
	return nil
}

func printLibrary(w io.Writer, renderer *output.Renderer, format string) error {
	dir := models.Dir()
	library, err := models.List(dir)
	if err != nil {
		return err
	}
	if format == "json" {
		return output.PrintJSON(w, append([]models.Downloaded{}, library...))
	}
	if len(library) == 0 {
		fmt.Fprintf(w, "No models in %s.\n", dir)
		return nil
	}
	fmt.Fprintln(w, renderer.Title("Models"))
	fmt.Fprintln(w, renderer.Field("Directory", dir))
	tbl := renderer.NewListTable("Model", "Files", "Size")
	for _, m := range library {
		tbl.Row(m.ID, fmt.Sprint(m.Files), megabytes(m.Bytes))
	}
	fmt.Fprintln(w, tbl.Render())
	return nil
}
