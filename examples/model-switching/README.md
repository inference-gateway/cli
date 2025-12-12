# Model Switching Example

This example demonstrates the `/model` command for temporarily switching models within a single conversation.

## What is `/model`?

The `/model` command allows you to execute a specific prompt with a different model while maintaining your
conversation context. After the prompt completes, the CLI automatically restores your original model.

**Syntax**: `/model <model-name> <prompt>`

## Use Case: Cost-Effective Development with Vision Analysis

This example shows a practical workflow where you:

1. Use a cost-effective model (`deepseek/deepseek-chat`) as your default for all coding tasks
2. Temporarily switch to vision model (`anthropic/claude-opus-4-5-20251101`) only when you need to analyze screenshots
3. Automatically return to the cheap model for implementation
4. Switch back to vision model for verification, then return to cheap model

This approach maximizes cost savings:

- **DeepSeek Chat (default)**: Stay on the cheap model ($0.28/$0.42 per MTok) for 95% of your work
- **Opus 4.5 (temporary)**: Use vision capabilities only when needed ($5.00/$25.00 per MTok)

## Prerequisites

Before running this example, ensure you have:

1. **Anthropic API Key** with sufficient credits
   - Required for Claude Opus 4.5 (vision model)
   - Get your API key at: <https://console.anthropic.com/>
   - Pricing: $5.00 per million input tokens, $25.00 per million output tokens
   - Minimum recommended credit: $5 for testing

2. **DeepSeek API Key** with sufficient credits
   - Required for DeepSeek Chat (default model)
   - Get your API key at: <https://platform.deepseek.com/>
   - Pricing: $0.28 per million input tokens, $0.42 per million output tokens
   - Minimum recommended credit: $1 for testing

3. **Inference Gateway CLI** installed
   - Install from [releases](https://github.com/inference-gateway/cli/releases)
   - Or build from source: `task build`

4. **Docker** (optional, only needed for the frontend demo)
   - Docker Desktop for Mac/Windows
   - Docker Engine for Linux

**Note**: This example uses two different providers. Make sure both API keys are active and have sufficient
credits before starting.

## Quick Start

1. **Set your API keys** as environment variables:

   ```bash
   export ANTHROPIC_API_KEY=your-anthropic-key-here
   export DEEPSEEK_API_KEY=your-deepseek-key-here
   ```

2. **Start the frontend** (optional):

   ```bash
   docker compose up -d
   ```

   Opens shoe store demo at <http://localhost:3000>

3. **Run the CLI**:

   ```bash
   infer chat
   ```

4. **Set DeepSeek as your default model**:

   ```text
   /switch
   ```

   Then select `deepseek/deepseek-chat` from the interactive menu.

5. **Open the demo store**: Navigate to <http://localhost:3000>, take a screenshot, and paste it into the CLI.

## Example Workflow

### Step 1: Analyze Screenshot with Opus 4.5

Paste your screenshot into the CLI, then type:

```text
/model anthropic/claude-opus-4-5-20251101 [Image #1] Please analyze this shoe store and describe all
the issues you see. Be specific and comprehensive.
```

**What happens**:

- CLI temporarily switches to `opus-4-5-20251101` (vision-capable model)
- Sends your detailed prompt with the screenshot to Opus 4.5
- Opus visually analyzes the ecommerce store and identifies all issues it sees
- **Automatically restores your default model** (deepseek-chat)

### Step 2: Implement Fixes with DeepSeek Chat

Your default model (`deepseek-chat`) is automatically restored. Now implement the fixes:

```text
Based on the analysis above, please fix all the identified issues in frontend/index.html
```

**What happens**:

- CLI automatically restored `deepseek-chat` after Opus 4.5 completed
- DeepSeek has full conversation context including Opus 4.5's screenshot analysis
- Implements the fixes at the cheap rate ($0.28/$0.42 per MTok)
- You stay on DeepSeek for all subsequent prompts

### Step 3: Continue with DeepSeek

Your default model (`deepseek-chat`) remains active. Continue enhancing:

```text
Now add hover effects, search functionality, and make the category links work
```

### Step 3.5: Compact Conversation (Optional)

Before verification, reduce context size to save costs:

```text
/compact
```

**What happens**:

- CLI summarizes the conversation history into a concise format
- Reduces input tokens for the next Opus 4.5 call
- Preserves key decisions and context while removing verbose details
- Saves ~40-95% on input token costs for verification

### Step 4: Verify Fixes with Opus 4.5

After DeepSeek completes the enhancements, take a new screenshot and verify the fixes:

```text
/model anthropic/claude-opus-4-5-20251101 [Image #2] Please review this updated shoe store and verify
if all the previous issues have been properly fixed. What improvements do you see?
```

**What happens**:

- CLI temporarily switches back to `opus-4-5-20251101` for vision-based verification
- Opus analyzes the new screenshot and compares against the original issues
- Provides visual confirmation of what was fixed and identifies any remaining problems
- **Automatically restores your default model** (deepseek-chat)
- You can continue iterating with DeepSeek based on Opus's feedback

## Cost Optimization Strategy

**Best Practice**: Use `deepseek/deepseek-chat` as your default model and only switch to Opus 4.5 when you need vision capabilities.

| Task Type | Model Choice | Cost per MTok |
| --------- | ------------ | ------------- |
| **Default for everything** | `deepseek/deepseek-chat` | $0.28/$0.42 |
| **Only for screenshots** | `/model anthropic/claude-opus-4-5-20251101` (temporary) | $5.00/$25.00 |

**Cost Comparison** (with `/compact` before verification):

- **Opus 4.5 initial analysis** (5K input + 2K output): ~$0.075
- **DeepSeek implementation** (8K input + 3K output): ~$0.003
- **DeepSeek enhancements** (5K input + 2K output): ~$0.002
- **Opus 4.5 verification** (1.5K input after compact + 1K output): ~$0.033
- **Total session cost**: ~$0.113 (97% of cost is the vision analysis!)

**Without `/compact`**: ~$0.13 (saves ~$0.017 per verification)
**Without `/model` command** (using Opus 4.5 for everything): ~$0.60+

## Key Features

- **Automatic model restoration**: No need to manually switch back
- **Full context preservation**: Temporary model sees entire conversation
- **Vision model support**: Use Opus 4.5 for image analysis
- **Cost transparency**: Pricing shown in autocomplete
- **Streaming indicator**: Shows which model is currently responding
- **Error handling**: Validates model names before switching

## Tips

- **Stay on DeepSeek by default** for all coding tasks
- **Use `/model` only for vision analysis** when you need to analyze screenshots
- **Use `/compact` before expensive model calls** to reduce input tokens by 40-95%
- **CLI auto-restores your model** after the temporary switch
- **Paste screenshots directly** into the chat (Cmd+Shift+4 on Mac, Win+Shift+S/Ctrl+Shift+S on Windows/Linux)

## Cleanup

Stop the frontend container:

```bash
docker compose down
```

## Related Examples

- **`examples/basic/`**: Basic CLI usage
- **`examples/shortcuts/`**: Custom shortcuts system
- **`examples/mcp/`**: MCP server integration

## Learn More

- See the main README for full list of available models
- Check `/help model` in the CLI for command syntax
- Use `/cost` to see session cost breakdown by model
