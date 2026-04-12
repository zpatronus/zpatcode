package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	LLMProviders        map[string]LLMProviderConfig `yaml:"llm_providers"`
	LLMMaxRetries       int                          `yaml:"llm_max_retries"`
	LLMTimeoutSec       int                          `yaml:"llm_timeout_sec"`
	InteractionMaxTurn  int                          `yaml:"interaction_max_turn"`
	ToolUseTimeout      int                          `yaml:"tool_use_timeout"`
	MaxToolOutputLength int                          `yaml:"max_tool_output_length"`
}

type LLMProviderConfig struct {
	Tokens  []string          `yaml:"tokens"`
	BaseURL string            `yaml:"base_url"`
	Models  map[string]string `yaml:"models"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
