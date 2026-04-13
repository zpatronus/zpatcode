package tooluse

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/chzyer/readline"
	"github.com/gookit/color"
	"github.com/zpatronus/zpatcode/config"
)

type ToolUse struct {
	ID      string
	Name    string
	Command string
}

func ParseToolUses(content string) []ToolUse {
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

func ExecuteToolCalls(cfg *config.Config, tools []ToolUse, rl *readline.Instance) []map[string]any {
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

func GetAbsWorkDir() (string, error) {
	workDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("Error getting current working directory: %v", err)
	}
	workDir, err = filepath.EvalSymlinks(workDir)
	if err != nil {
		return "", fmt.Errorf("Error evaluating symlinks for working directory: %v", err)
	}
	workDir, err = filepath.Abs(workDir)
	if err != nil {
		return "", fmt.Errorf("Error getting absolute path of working directory: %v", err)
	}
	return workDir, nil
}

func GetSafePath(path string) (string, error) {
	workDir, err := GetAbsWorkDir()
	if err != nil {
		return "", err
	}
	workDir = filepath.Clean(workDir)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("Error getting absolute path: %v", err)
	}
	absPath, err = filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("Error evaluating symlinks for path: %v", err)
	}

	// safe only if absPath is under workDir or is the same as workDir
	rel, err := filepath.Rel(workDir, absPath)
	if err != nil {
		return "", fmt.Errorf("Error getting relative path: %v", err)
	}
	if strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("Unsafe path: %s is outside of working directory", absPath)
	}
	if !strings.HasPrefix(absPath, workDir) {
		return "", fmt.Errorf("Unsafe path: %s is outside of working directory", absPath)
	}
	return absPath, nil
}

func runRead(path string) ([]byte, error) {
	safePath, err := GetSafePath(path)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(safePath)
	if err != nil {
		return nil, fmt.Errorf("Error reading file: %v", err)
	}
	return content, nil
}

func runReadUnsafePath(path string) ([]byte, error) {
	return os.ReadFile(path)
}
