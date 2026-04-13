package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chzyer/readline"
	"github.com/gookit/color"
	"github.com/zpatronus/zpatcode/config"
	"github.com/zpatronus/zpatcode/llm_client"
	"github.com/zpatronus/zpatcode/tooluse"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("err: %v\n", err)
		os.Exit(1)
	}
	client := llm_client.New(cfg)
	workDir, err := tooluse.GetAbsWorkDir()
	if err != nil {
		fmt.Printf(color.Red.Sprintf("Error determining working directory: %v\n", err))
		os.Exit(1)
	}
	color.Cyan.Println("zpatcode AI agent (Ctrl+C to quit)")
	color.Gray.Printf("Working directory: %s\n", workDir)

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
			tools := tooluse.ParseToolUses(result.Response)
			if len(tools) == 0 {
				fmt.Println(result.Response)
				break
			}
			fmt.Printf("\n[%d tool call(s)]\n", len(tools))
			results := tooluse.ExecuteToolCalls(cfg, tools, rl)
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
