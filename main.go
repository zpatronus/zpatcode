package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/chzyer/readline"
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

	promptStr := color.Cyan.Sprint("zpat >> ")
	rl, err := readline.NewEx(&readline.Config{
		Prompt:          promptStr,
		HistoryFile:     "/tmp/zpatcode_history",
		InterruptPrompt: "^C",
		EOFPrompt:       "exit",
	})
	if err != nil {
		fmt.Printf("Error initializing readline: %v\n", err)
		os.Exit(1)
	}
	defer rl.Close()

	history := []llm_client.Message{
		{Role: "system", Content: systemPrompt()},
	}

	for {
		query, err := rl.Readline()
		if err != nil {
			break
		}
		query = strings.TrimSpace(query)
		if query == "" {
			continue
		}
		if query == "q" || query == "exit" {
			break
		}
		history = append(history, llm_client.Message{Role: "user", Content: query})

		for turn := 0; turn < cfg.InteractionMaxTurn; turn++ {
			// loop until no more tool use
			req := llm_client.Request{Messages: history}
			result := <-client.Chat(context.Background(), req)
			if result.Err != nil {
				color.Red.Printf("Error: %v\n", result.Err)
				break
			}
			history = append(history, llm_client.Message{Role: "assistant", Content: result.Response})
			// color.Gray.Println("%s", result.Response)
			tools := parseToolUses(result.Response)
			if len(tools) == 0 {
				fmt.Println(result.Response)
				break
			}
			fmt.Printf("\n[%d tool call(s)]\n", len(tools))
			results := executeToolCalls(cfg, tools, rl)
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
		if strings.HasPrefix(trimmed, "<tool_use>") || strings.HasPrefix(trimmed, "```bash") || strings.HasPrefix(trimmed, "```sh") || strings.HasPrefix(trimmed, "<bash>") {
			inBlock = true
			blockID++
			cmd.Reset()
			continue
		}
		if strings.HasPrefix(trimmed, "</tool_use>") || strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "</bash>") {
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

func executeToolCalls(cfg *config.Config, tools []ToolUse, rl *readline.Instance) []map[string]any {
	var results []map[string]any
	for _, tool := range tools {
		userApproval, reason := confirmRun(tool.Command, rl)
		if !userApproval {
			color.Red.Println("Skipped.")
			results = append(results, map[string]any{
				"type":        "tool_result",
				"tool_use_id": tool.ID,
				"content":     reason,
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

func confirmRun(command string, rl *readline.Instance) (bool, string) {
	color.Yellow.Printf("$ %s\n", command)
	rl.SetPrompt(color.Yellow.Sprint("Run this command? [Enter=yes; n=no, or type rejection reason]: "))
	rl.Refresh()
	answer, err := rl.Readline()
	rl.SetPrompt(color.Cyan.Sprint("zpat >> "))
	if err != nil {
		return false, "Agent error. Cannot get user approval"
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "n" {
		return false, "User denied execution"
	}
	if answer == "" {
		return true, ""
	}
	return false, answer
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
