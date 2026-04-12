package main

import (
	"context"
	"fmt"
	"log"

	"github.com/zpatronus/zpatcode/config"
	"github.com/zpatronus/zpatcode/llm_client"
)

func main() {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		fmt.Printf("err: %v\n", err)

	}
	// fmt.Printf("%+v\n", cfg)

	client := llm_client.New(cfg)
	req := llm_client.Request{
		Messages: []llm_client.Message{
			{Role: "user", Content: "hello"},
		},
	}
	result := <-client.Chat(context.Background(), req)
	if result.Err != nil {
		log.Fatal(result.Err)
	}

	fmt.Printf("Provider: %s\nModel: %s\nResponse: %s\n", result.LLMProviderName, result.ModelDisplayName, result.Response)
}
