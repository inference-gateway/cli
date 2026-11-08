package compute

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"strconv"
	"time"

	huh "charm.land/huh/v2"
	cobra "github.com/spf13/cobra"

	runtime "github.com/inference-gateway/cli/cmd/runtime"
	config "github.com/inference-gateway/cli/config"
	computedomain "github.com/inference-gateway/cli/internal/compute/domain"
	runpod "github.com/inference-gateway/cli/internal/compute/infrastructure/runpod"
	utils "github.com/inference-gateway/cli/internal/platform/utils"
)

const (
	gpuDefaultImage  = "ghcr.io/ggml-org/llama.cpp:server-cuda"
	gpuDefaultModel  = "ggml-org/gemma-4-E4B-it-GGUF:Q4_0"
	gpuDefaultDiskGB = 50
	gpuLlamaPort     = 8080
)

const gpusLong = `Provision, inspect and destroy on-demand GPU instances running a llama.cpp
server - pay only for the hours used. The instance is exposed through the
standard llamacpp provider env vars (LLAMACPP_API_URL / LLAMACPP_API_KEY),
so connecting to it is indistinguishable from any other llamacpp backend.

The RunPod API key is management-plane only (create/list/destroy calls). It is
read from RUNPOD_API_KEY in the environment or ~/.infer/auth.yaml, and asked
for and saved to auth.yaml on first use. Defaults and the hourly cap live under
gpus in ~/.infer/compute.yaml.`

// newGPUsCommand is `infer compute gpus`, renting GPU instances that serve a
// model. use is its name, so the same tree also serves the old `infer gpu`.
func newGPUsCommand(state *runtime.State, use string) *cobra.Command {
	command := &cobra.Command{
		Use:   use,
		Short: "Rent an on-demand cloud GPU running llama.cpp (RunPod)",
		Long:  gpusLong,
	}

	provisionCommand := &cobra.Command{
		Use:   "provision",
		Short: "Provision a GPU instance running llama.cpp (interactive)",
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
		Use:   "status <instance-id>",
		Short: "Show an instance's status, cost and endpoint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return gpuStatus(state, cmd, args[0])
		},
	}
	statusCommand.Flags().Bool("wait", false, "Block until llama.cpp answers, with a progress heartbeat")

	destroyCommand := &cobra.Command{
		Use:   "destroy <instance-id>",
		Short: "Destroy an instance (billing stops)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return gpuDestroy(state, cmd, args[0])
		},
	}
	destroyCommand.Flags().BoolP("yes", "y", false, "Skip the confirmation prompt")

	command.AddCommand(provisionCommand, listCommand, statusCommand, destroyCommand)
	return command
}

// NewGPUAlias keeps `infer gpu` working as a hidden, deprecated copy of
// `infer compute gpus`.
func NewGPUAlias(state *runtime.State) *cobra.Command {
	command := newGPUsCommand(state, "gpu")
	command.Deprecated = "use `infer compute gpus` instead"
	command.Hidden = true
	return command
}

// gpuDriver builds the RunPod driver from the key in the environment or
// auth.yaml, else the legacy config.yaml key, else one it asks for and saves.
func gpuDriver(state *runtime.State, warn io.Writer) (*runpod.RunPod, error) {
	key, err := runPodAPIKey(state.Config(), warn)
	if err != nil {
		return nil, err
	}
	if key == "" {
		if key, err = promptAndSaveAPIKey(); err != nil {
			return nil, err
		}
	}
	return runpod.NewRunPod(key), nil
}

// runPodAPIKey is the RunPod key the environment or auth.yaml sets, else the
// legacy provisioner.runpod.api_key of config.yaml with a warning to move it.
func runPodAPIKey(cfg *config.Config, warn io.Writer) (string, error) {
	keys, err := config.Credentials([]string{config.RunPodAPIKeyName})
	if err != nil {
		_, _ = fmt.Fprintf(warn, "warning: %v\n", err)
	}
	if key := keys[config.RunPodAPIKeyName]; key != "" {
		return key, nil
	}
	if legacy := cfg.Provisioner.RunPod.APIKey; legacy != "" {
		_, _ = fmt.Fprintf(warn, "warning: provisioner.runpod.api_key in config.yaml is deprecated, move it to %s in ~/.infer/auth.yaml\n", config.RunPodAPIKeyName)
		return legacy, nil
	}
	return "", nil
}

func promptAndSaveAPIKey() (string, error) {
	var key string
	if err := huh.NewInput().
		Title("RunPod API key").
		Description("Management-plane only (create/list/destroy). Saved as " + config.RunPodAPIKeyName + " in ~/.infer/auth.yaml.").
		EchoMode(huh.EchoModePassword).
		Value(&key).Run(); err != nil {
		return "", err
	}
	if key == "" {
		return "", fmt.Errorf("a RunPod API key is required (create one at runpod.io/console/user/settings)")
	}
	if err := config.SetAuthKey(config.RunPodAPIKeyName, key); err != nil {
		return "", fmt.Errorf("failed to save API key: %w", err)
	}
	fmt.Printf("API key saved to %s\n", config.AuthFilePath())
	return key, nil
}

// gpuSettings are the GPU settings in effect, warning when they still come
// from the legacy provisioner block of config.yaml.
func gpuSettings(cfg *config.Config, warn io.Writer) config.GPUConfig {
	settings, legacy := cfg.GPUSettings()
	if legacy {
		_, _ = fmt.Fprintln(warn, "warning: provisioner in config.yaml is deprecated, move it to gpus in ~/.infer/compute.yaml")
	}
	return settings
}

func gpuProvision(state *runtime.State, cmd *cobra.Command) error {
	settings := gpuSettings(state.Config(), cmd.ErrOrStderr())
	drv, err := gpuDriver(state, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	ctx := cmd.Context()

	gpuType, _ := cmd.Flags().GetString("gpu-type")
	model, _ := cmd.Flags().GetString("model")
	yes, _ := cmd.Flags().GetBool("yes")
	gpuType = cmp.Or(gpuType, settings.GPUType)
	model = cmp.Or(model, settings.Model, gpuDefaultModel)

	gpuType, model, cloud, pricePerHr, err := gpuResolveChoices(ctx, drv, settings.CloudType, gpuType, model, yes)
	if err != nil {
		return err
	}
	if err := computedomain.CheckHourlyCap(gpuType, pricePerHr, settings.MaxHourly); err != nil {
		return err
	}

	if !yes {
		ok := false
		if err := huh.NewConfirm().
			Title(fmt.Sprintf("Provision %s (%s) at ~$%.2f/hr serving %s?", gpuType, cloud, pricePerHr, model)).
			Value(&ok).Run(); err != nil || !ok {
			return fmt.Errorf("provisioning cancelled")
		}
	}

	token, err := utils.RandomToken()
	if err != nil {
		return err
	}
	image := cmp.Or(settings.Image, gpuDefaultImage)
	diskGB := cmp.Or(settings.DiskGB, gpuDefaultDiskGB)

	instance, err := drv.Provision(ctx, computedomain.ProvisionRequest{
		Name:      computedomain.InstanceNamePrefix + token[:8],
		GPUTypeID: gpuType,
		Image:     image,
		StartCmd: []string{
			"-hf", model,
			"--host", "0.0.0.0",
			"--port", strconv.Itoa(gpuLlamaPort),
			"--api-key", token,
			"--jinja",
		},
		Cloud:  cloud,
		DiskGB: diskGB,
		Ports:  []string{fmt.Sprintf("%d/http", gpuLlamaPort)},
	})
	if err != nil {
		return fmt.Errorf("failed to provision instance: %w", err)
	}
	fmt.Printf("Instance %s created (~$%.2f/hr)\n", instance.ID, instance.HourlyCost)
	fmt.Printf("Spec: %s (%s), %d GB disk, image %s, model %s\n", gpuType, cloud, diskGB, image, model)
	fmt.Println("Waiting until the model answers...")

	instance, err = drv.WaitReady(ctx, instance.ID, gpuLlamaPort, gpuHeartbeat)
	fmt.Println()
	if err != nil {
		return fmt.Errorf("instance %s did not become ready (destroy it with `infer compute gpus destroy %s`): %w", instance.ID, instance.ID, err)
	}

	fmt.Println("\nReady. Point the gateway at it:")
	fmt.Printf("  LLAMACPP_API_URL=%s/v1\n", drv.URL(instance.ID, gpuLlamaPort))
	fmt.Printf("  LLAMACPP_API_KEY=%s\n", token)
	fmt.Printf("\nWhen done: infer compute gpus destroy %s\n", instance.ID)
	return nil
}

func gpuAskChoices(types []computedomain.GPUType, model string, yes bool) (string, string, error) {
	var gpuType string
	fields := []huh.Field{
		huh.NewSelect[string]().Title("GPU type (cheapest first)").Options(gpuTypeOptions(types)...).Height(15).Value(&gpuType),
	}
	if !yes {
		fields = append(fields, huh.NewInput().Title("Model (Hugging Face GGUF, <repo>:<quant>)").Value(&model))
	}
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return "", "", err
	}
	return gpuType, model, nil
}

func gpuTypeOptions(types []computedomain.GPUType) []huh.Option[string] {
	opts := make([]huh.Option[string], 0, len(types))
	for _, t := range types {
		price, _ := t.HourlyPrice()
		opts = append(opts, huh.NewOption(fmt.Sprintf("%-45s %3d GB  $%.2f/hr", t.DisplayName, t.MemoryGB, price), t.ID))
	}
	return opts
}

// gpuResolveChoices fills in GPU type, model, cloud and price from flags,
// config and the interactive picker (with live pricing, cheapest first).
func gpuResolveChoices(ctx context.Context, drv *runpod.RunPod, configuredCloud, gpuType, model string, yes bool) (string, string, computedomain.Cloud, float64, error) {
	types, err := drv.GPUTypes(ctx)
	if err != nil {
		return "", "", "", 0, fmt.Errorf("failed to list GPU types: %w", err)
	}
	if gpuType == "" {
		if gpuType, model, err = gpuAskChoices(types, model, yes); err != nil {
			return "", "", "", 0, err
		}
	}
	cloud, pricePerHr := computedomain.CloudCommunity, 0.0
	for _, t := range types {
		if t.ID == gpuType {
			pricePerHr, cloud = t.HourlyPrice()
		}
	}
	if configuredCloud != "" {
		cloud = computedomain.Cloud(configuredCloud)
	}
	return gpuType, model, cloud, pricePerHr, nil
}

func gpuList(state *runtime.State, cmd *cobra.Command) error {
	drv, err := gpuDriver(state, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	instances, err := drv.List(cmd.Context())
	if err != nil {
		return err
	}
	if len(instances) == 0 {
		fmt.Println("No infer-provisioned instances.")
		return nil
	}
	now := time.Now()
	fmt.Printf("%-16s %-16s %-10s %-10s %s\n", "ID", "NAME", "STATUS", "$/HR", "UPTIME")
	for _, instance := range instances {
		fmt.Printf("%-16s %-16s %-10s %-10.2f %s\n", instance.ID, instance.Name, instance.Status, instance.HourlyCost, uptime(instance, now))
	}
	return nil
}

func gpuHeartbeat(msg string) { fmt.Printf("\r\033[K• %s", msg) }

func gpuStatus(state *runtime.State, cmd *cobra.Command, id string) error {
	drv, err := gpuDriver(state, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	instance, err := drv.Status(cmd.Context(), id)
	if err != nil {
		return err
	}
	if wait, _ := cmd.Flags().GetBool("wait"); wait {
		instance, err = drv.WaitReady(cmd.Context(), instance.ID, gpuLlamaPort, gpuHeartbeat)
		fmt.Println()
		if err != nil {
			return err
		}
		fmt.Println("Ready.")
	}
	now := time.Now()
	fmt.Printf("ID:      %s\nName:    %s\nStatus:  %s\nImage:   %s\nCost:    $%.2f/hr\nUptime:  %s\nURL:     %s/v1\n",
		instance.ID, instance.Name, instance.Status, instance.Image, instance.HourlyCost, uptime(instance, now), drv.URL(instance.ID, gpuLlamaPort))
	if spent := instance.SpentSoFar(now); spent > 0 {
		fmt.Printf("Est. total: $%.2f\n", spent)
	}
	return nil
}

func gpuDestroy(state *runtime.State, cmd *cobra.Command, id string) error {
	drv, err := gpuDriver(state, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	if yes, _ := cmd.Flags().GetBool("yes"); !yes {
		ok := false
		if err := huh.NewConfirm().Title(fmt.Sprintf("Destroy instance %s?", id)).Value(&ok).Run(); err != nil || !ok {
			return fmt.Errorf("destroy cancelled")
		}
	}
	if err := drv.Destroy(cmd.Context(), id); err != nil {
		return err
	}
	fmt.Printf("Instance %s destroyed, billing stopped.\n", id)
	return nil
}

func uptime(instance computedomain.GPUInstance, now time.Time) string {
	if up := instance.Uptime(now); up > 0 {
		return up.Round(time.Minute).String()
	}
	return "-"
}
