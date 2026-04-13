package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gookit/color"
	"github.com/zpatronus/zpatcode/config"
	"github.com/zpatronus/zpatcode/llm_client"
)

type ToolUse struct {
	ID      string
	Name    string
	Command string
}

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("err: %v\n", err)
		os.Exit(1)
	}
	client := llm_client.New(cfg)
	color.Cyan.Println("zpat >> AI agent (Ctrl+C to quit)")
	scanner := bufio.NewScanner(os.Stdin)

	history := []llm_client.Message{
		{Role: "system", Content: systemPrompt()},
	}

	for {
		color.Cyan.Print("zpat >> ")
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace((scanner.Text()))

		if query == "" || query == "q" || query == "exit" {
			break
		}

		history = append(history, llm_client.Message{Role: "user", Content: query})

		for turn := 0; turn < cfg.InteractionMaxTurn; turn++ {
			req := llm_client.Request{Messages: history}
			result := <-client.Chat(context.Background(), req)
			if result.Err != nil {
				color.Red.Printf("Error: %v\n", result.Err)
				break
			}

			history = append(history, llm_client.Message{Role: "assistant", Content: result.Response})

			tools := parseToolUses(result.Response)

			if len(tools) == 0 {
				fmt.Println(result.Response)
				break
			}

			fmt.Printf("\n[%d tool call(s)]\n", len(tools))
			results := executeToolCalls(cfg, tools)

			var parts []string
			for _, r := range results {
				parts = append(parts, fmt.Sprintf("Tool: %s\nOutput:\n%s", r["tool_use_id"], r["content"]))
			}
			history = append(history, llm_client.Message{
				Role:    "user",
				Content: strings.Join(parts, "\n\n---\n\n"),
			})
		}
	}
}

func systemPrompt() string {
	cwd, err := os.Getwd()
	if err == nil {
		return fmt.Sprintf("You are an agent at %s. Use bash to interact with the world (inspect, change, etc). Act first, then report clearly.", cwd)
	}
	return fmt.Sprintf("You are an agent. Use bash to interact with the world (inspect, change, etc). Act first, then report clearly.")
}

func parseToolUses(content string) []ToolUse {
	var tools []ToolUse
	lines := strings.Split(content, "\n")
	inBlock := false
	blockID := 0
	var cmd strings.Builder
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "<tool_use>") || strings.HasPrefix(trimmed, "```bash") || strings.HasPrefix(trimmed, "```sh") {
			inBlock = true
			blockID++
			cmd.Reset()
			continue
		}
		if strings.HasPrefix(trimmed, "</tool_use>") || strings.HasPrefix(trimmed, "```") {
			if inBlock && cmd.Len() > 0 {
				tools = append(tools, ToolUse{
					ID:      fmt.Sprintf("tool_%d", blockID),
					Name:    "bash",
					Command: strings.TrimSpace(cmd.String()),
				})
				inBlock = false
				continue
			}
		}
		if inBlock {
			cmd.WriteString(line)
			cmd.WriteString("\n")
		}
	}
	return tools
}

func executeToolCalls(cfg *config.Config, tools []ToolUse) []map[string]any {
	var results []map[string]any
	for _, tool := range tools {
		if !confirmRun(tool.Command) {
			color.Red.Println("Skipped.")
			results = append(results, map[string]any{
				"type":        "tool_result",
				"tool_use_id": tool.ID,
				"content":     "User denied execution",
			})
			continue
		}
		output := runBash(cfg, tool.Command)
		gray := color.Gray
		if len(output) > 50 {
			gray.Printf("Result preview: %s ...\n", output[:50])
		} else {
			gray.Printf("Result preview: %s\n", output)
		}
		results = append(results, map[string]any{
			"type":        "tool_result",
			"tool_use_id": tool.ID,
			"content":     output,
		})
	}
	return results
}

func confirmRun(command string) bool {
	color.Yellow.Printf("$ %s\n", command)
	color.Yellow.Print("Run this command? [Y/n]: ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer == "n" || answer == "no" {
		return false
	}
	return true
}

func runBash(cfg *config.Config, command string) string {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.ToolUseTimeout)*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Sprintf("Error: Timeout (%ds)", cfg.ToolUseTimeout)
		}
		errMsg := fmt.Sprintf("Error: %s", err.Error())
		if len(output) > 0 {
			errMsg += "\n" + string(output)
		}
		result := strings.TrimSpace(errMsg)
		if len(result) > cfg.MaxToolOutputLength {
			return result[:cfg.MaxToolOutputLength] + "\n...(too long, truncated)"
		}
		return result
	}

	result := strings.TrimSpace(string(output))
	if result == "" {
		return "(no output)"
	}
	if len(result) > cfg.MaxToolOutputLength {
		return result[:cfg.MaxToolOutputLength] + "\n...(too long, truncated)"
	}
	return result
}
