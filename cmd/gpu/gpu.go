package gpu

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"

	huh "charm.land/huh/v2"
	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
	provisioner "github.com/inference-gateway/cli/internal/provisioner"
)

const (
	gpuDefaultImage  = "ghcr.io/ggml-org/llama.cpp:server-cuda"
	gpuDefaultModel  = "ggml-org/gemma-4-E4B-it-GGUF:Q4_0"
	gpuDefaultDiskGB = 50
	gpuLlamaPort     = 8080
)

func NewCommand(state *runtime.State) *cobra.Command {
	command := &cobra.Command{
		Use:   "gpu",
		Short: "Rent an on-demand cloud GPU running llama.cpp (RunPod)",
		Long: `Provision, inspect and destroy on-demand GPU instances running a llama.cpp
server - pay only for the hours used. The instance is exposed through the
standard llamacpp provider env vars (LLAMACPP_API_URL / LLAMACPP_API_KEY);
connecting to it is indistinguishable from any other llamacpp backend.

The RunPod API key is management-plane only (create/list/destroy calls). It is
asked for on first provision and stored as provisioner.runpod.api_key in
config.yaml, or supplied via INFER_PROVISIONER_RUNPOD_API_KEY.`,
	}

	provisionCommand := &cobra.Command{
		Use:   "provision",
		Short: "Provision a GPU pod running llama.cpp (interactive)",
		RunE: func(cmd *cobra.Command, args []string) error {
			return gpuProvision(state, cmd)
		},
	}
	provisionCommand.Flags().String("gpu-type", "", "GPU type id (skips the interactive picker)")
	provisionCommand.Flags().String("model", "", "Hugging Face GGUF to serve, as <repo>:<quant>")
	provisionCommand.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")

	listCommand := &cobra.Command{
		Use:   "list",
		Short: "List instances provisioned by infer",
		RunE: func(cmd *cobra.Command, args []string) error {
			return gpuList(state, cmd)
		},
	}

	statusCommand := &cobra.Command{
		Use:   "status <pod-id>",
		Short: "Show state, uptime and cost of a pod",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return gpuStatus(state, cmd, args[0])
		},
	}
	statusCommand.Flags().Bool("wait", false, "Block until llama.cpp answers, with a progress heartbeat")

	destroyCommand := &cobra.Command{
		Use:   "destroy <pod-id>",
		Short: "Destroy a pod (billing stops)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return gpuDestroy(state, cmd, args[0])
		},
	}
	destroyCommand.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")

	command.AddCommand(provisionCommand, listCommand, statusCommand, destroyCommand)
	return command
}

// gpuDriver builds the RunPod driver, prompting for (and persisting) the API
// key on first use via the same write path as `infer config set`.
func gpuDriver(state *runtime.State) (*provisioner.RunPod, error) {
	key := state.Config().Provisioner.RunPod.APIKey
	if key == "" {
		var err error
		if key, err = promptAndSaveAPIKey(); err != nil {
			return nil, err
		}
	}
	return provisioner.NewRunPod(key), nil
}

func promptAndSaveAPIKey() (string, error) {
	var key string
	if err := huh.NewInput().
		Title("RunPod API key").
		Description("Management-plane only (create/list/destroy). Stored as provisioner.runpod.api_key in ~/.infer/config.yaml.").
		EchoMode(huh.EchoModePassword).
		Value(&key).Run(); err != nil {
		return "", err
	}
	if key == "" {
		return "", fmt.Errorf("a RunPod API key is required (create one at runpod.io/console/user/settings)")
	}
	target, path, err := runtime.ConfigWriteTarget(false)
	if err != nil {
		return "", err
	}
	target.Set("provisioner.runpod.api_key", key)
	if err := utils.WriteViperConfigWithIndent(target, 2); err != nil {
		return "", fmt.Errorf("failed to save API key: %w", err)
	}
	fmt.Printf("API key saved to %s\n", path)
	return key, nil
}

func gpuProvision(state *runtime.State, cmd *cobra.Command) error {
	cfg := state.Config()
	drv, err := gpuDriver(state)
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	gpuType, _ := cmd.Flags().GetString("gpu-type")
	model, _ := cmd.Flags().GetString("model")
	yes, _ := cmd.Flags().GetBool("yes")
	if gpuType == "" {
		gpuType = cfg.Provisioner.GPUType
	}
	if model == "" {
		model = cfg.Provisioner.Model
	}
	if model == "" {
		model = gpuDefaultModel
	}

	gpuType, model, cloudType, pricePerHr, err := gpuResolveChoices(cmd.Context(), drv, cfg.Provisioner.CloudType, gpuType, model, yes)
	if err != nil {
		return err
	}
	if maxHourly := cfg.Provisioner.MaxHourly; maxHourly > 0 && pricePerHr > maxHourly {
		return fmt.Errorf("%s costs $%.2f/hr, above provisioner.max_hourly ($%.2f)", gpuType, pricePerHr, maxHourly)
	}

	if !yes {
		ok := false
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Provision %s (%s) at ~$%.2f/hr serving %s?", gpuType, cloudType, pricePerHr, model)).
			Value(&ok).Run(); err != nil || !ok {
			return fmt.Errorf("provisioning cancelled")
		}
	}

	token, err := randomToken()
	if err != nil {
		return err
	}
	image := cfg.Provisioner.Image
	if image == "" {
		image = gpuDefaultImage
	}
	diskGB := cfg.Provisioner.DiskGB
	if diskGB == 0 {
		diskGB = gpuDefaultDiskGB
	}

	pod, err := drv.Provision(ctx, provisioner.ProvisionRequest{
		Name:      provisioner.PodNamePrefix + token[:8],
		GPUTypeID: gpuType,
		Image:     image,
		StartCmd: []string{
			"-hf", model,
			"--host", "0.0.0.0",
			"--port", strconv.Itoa(gpuLlamaPort),
			"--api-key", token,
			"--jinja",
		},
		CloudType: cloudType,
		DiskGB:    diskGB,
		Ports:     []string{fmt.Sprintf("%d/http", gpuLlamaPort)},
	})
	if err != nil {
		return fmt.Errorf("failed to provision pod: %w", err)
	}
	fmt.Printf("Pod %s created (~$%.2f/hr)\n", pod.ID, pod.CostPerHr)
	fmt.Printf("Spec: %s (%s), %d GB disk, image %s, model %s\n", gpuType, cloudType, diskGB, image, model)
	fmt.Println("Waiting until the model answers...")

	pod, err = drv.WaitReady(ctx, pod.ID, gpuLlamaPort, gpuHeartbeat)
	fmt.Println()
	if err != nil {
		return fmt.Errorf("pod %s did not become ready (destroy it with `infer gpu destroy %s`): %w", pod.ID, pod.ID, err)
	}

	fmt.Println("\nReady. Point the gateway at it:")
	fmt.Printf("  LLAMACPP_API_URL=%s/v1\n", pod.ProxyURL(gpuLlamaPort))
	fmt.Printf("  LLAMACPP_API_KEY=%s\n", token)
	fmt.Printf("\nWhen done: infer gpu destroy %s\n", pod.ID)
	return nil
}

func gpuAskChoices(types []provisioner.GPUType, model string, yes bool) (string, string, error) {
	var gpuType string
	fields := []huh.Field{
		// Height caps the list so the title/filter line stays on screen ("/" to filter).
		huh.NewSelect[string]().Title("GPU type (cheapest first)").Options(gpuTypeOptions(types)...).Height(15).Value(&gpuType),
	}
	if !yes {
		fields = append(fields, huh.NewInput().Title("Model (Hugging Face GGUF, <repo>:<quant>)").Value(&model))
	}
	// One group so shift+tab navigates back to change an earlier answer.
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return "", "", err
	}
	return gpuType, model, nil
}

func gpuTypeOptions(types []provisioner.GPUType) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(types))
	for _, t := range types {
		p := t.CommunityPrice
		if p == 0 {
			p = t.SecurePrice
		}
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-45s %3d GB  $%.2f/hr", t.DisplayName, t.MemoryInGb, p), t.ID))
	}
	return opts
}

// gpuResolveChoices fills in GPU type, model, cloud type and price from flags,
// config and the interactive picker (with live pricing, cheapest first).
func gpuResolveChoices(ctx context.Context, drv *provisioner.RunPod, configuredCloudType, gpuType, model string, yes bool) (string, string, string, float64, error) {
	types, err := drv.GPUTypes(ctx)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("failed to list GPU types: %w", err)
	}
	if gpuType == "" {
		if gpuType, model, err = gpuAskChoices(types, model, yes); err != nil {
			return "", "", "", 0, err
		}
	}
	cloudType, pricePerHr := "COMMUNITY", 0.0
	for _, t := range types {
		if t.ID == gpuType {
			pricePerHr = t.CommunityPrice
			if pricePerHr == 0 {
				pricePerHr = t.SecurePrice
				cloudType = "SECURE"
			}
		}
	}
	if configuredCloudType != "" {
		cloudType = configuredCloudType
	}
	return gpuType, model, cloudType, pricePerHr, nil
}

func gpuList(state *runtime.State, cmd *cobra.Command) error {
	drv, err := gpuDriver(state)
	if err != nil {
		return err
	}
	pods, err := drv.List(cmd.Context())
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		fmt.Println("No infer-provisioned instances.")
		return nil
	}
	fmt.Printf("%-16s %-16s %-10s %-10s %s\n", "ID", "NAME", "STATUS", "$/HR", "UPTIME")
	for _, p := range pods {
		fmt.Printf("%-16s %-16s %-10s %-10.2f %s\n", p.ID, p.Name, p.DesiredStatus, p.CostPerHr, podUptime(p))
	}
	return nil
}

func gpuHeartbeat(msg string) { fmt.Printf("\r\033[K• %s", msg) }

func gpuStatus(state *runtime.State, cmd *cobra.Command, podID string) error {
	drv, err := gpuDriver(state)
	if err != nil {
		return err
	}
	pod, err := drv.Status(cmd.Context(), podID)
	if err != nil {
		return err
	}
	if wait, _ := cmd.Flags().GetBool("wait"); wait {
		pod, err = drv.WaitReady(cmd.Context(), pod.ID, gpuLlamaPort, gpuHeartbeat)
		fmt.Println()
		if err != nil {
			return err
		}
		fmt.Println("Ready.")
	}
	fmt.Printf("ID:      %s\nName:    %s\nStatus:  %s\nImage:   %s\nCost:    $%.2f/hr\nUptime:  %s\nURL:     %s/v1\n",
		pod.ID, pod.Name, pod.DesiredStatus, pod.Image, pod.CostPerHr, podUptime(pod), pod.ProxyURL(gpuLlamaPort))
	if up := uptimeHours(pod); up > 0 {
		fmt.Printf("Est. total: $%.2f\n", up*pod.CostPerHr)
	}
	return nil
}

func gpuDestroy(state *runtime.State, cmd *cobra.Command, podID string) error {
	drv, err := gpuDriver(state)
	if err != nil {
		return err
	}
	if yes, _ := cmd.Flags().GetBool("yes"); !yes {
		ok := false
		if err := huh.NewConfirm().Title(fmt.Sprintf("Destroy pod %s?", podID)).Value(&ok).Run(); err != nil || !ok {
			return fmt.Errorf("destroy cancelled")
		}
	}
	if err := drv.Destroy(cmd.Context(), podID); err != nil {
		return err
	}
	fmt.Printf("Pod %s destroyed; billing stopped.\n", podID)
	return nil
}

func podUptime(p provisioner.Pod) string {
	if h := uptimeHours(p); h > 0 {
		return time.Duration(h * float64(time.Hour)).Round(time.Minute).String()
	}
	return "-"
}

func uptimeHours(p provisioner.Pod) float64 {
	if p.DesiredStatus != "RUNNING" || p.LastStartedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, p.LastStartedAt)
	if err != nil {
		return 0
	}
	return time.Since(t).Hours()
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
