package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

type modelCatalog struct {
	Claude      []modelInfo `json:"claude"`
	Gemini      []modelInfo `json:"gemini"`
	Vertex      []modelInfo `json:"vertex"`
	GeminiCLI   []modelInfo `json:"gemini-cli"`
	AIStudio    []modelInfo `json:"aistudio"`
	CodexFree   []modelInfo `json:"codex-free"`
	CodexTeam   []modelInfo `json:"codex-team"`
	CodexPlus   []modelInfo `json:"codex-plus"`
	CodexPro    []modelInfo `json:"codex-pro"`
	Qwen        []modelInfo `json:"qwen"`
	IFlow       []modelInfo `json:"iflow"`
	Kimi        []modelInfo `json:"kimi"`
	Antigravity []modelInfo `json:"antigravity"`
}

type modelInfo struct {
	ID string `json:"id"`
}

func main() {
	path := "internal/registry/models/models.json"
	if len(os.Args) > 1 && strings.TrimSpace(os.Args[1]) != "" {
		path = os.Args[1]
	}

	data, err := os.ReadFile(path)
	if err != nil {
		fail("read %s: %v", path, err)
	}

	var catalog modelCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		fail("decode %s: %v", path, err)
	}

	sections := []struct {
		name   string
		models []modelInfo
	}{
		{name: "claude", models: catalog.Claude},
		{name: "gemini", models: catalog.Gemini},
		{name: "vertex", models: catalog.Vertex},
		{name: "gemini-cli", models: catalog.GeminiCLI},
		{name: "aistudio", models: catalog.AIStudio},
		{name: "codex-free", models: catalog.CodexFree},
		{name: "codex-team", models: catalog.CodexTeam},
		{name: "codex-plus", models: catalog.CodexPlus},
		{name: "codex-pro", models: catalog.CodexPro},
		{name: "kimi", models: catalog.Kimi},
		{name: "antigravity", models: catalog.Antigravity},
	}

	for _, section := range sections {
		if len(section.models) == 0 {
			fail("validate %s: %s section is empty", path, section.name)
		}

		seen := make(map[string]struct{}, len(section.models))
		for i, model := range section.models {
			id := strings.TrimSpace(model.ID)
			if id == "" {
				fail("validate %s: %s[%d] has empty id", path, section.name, i)
			}
			if _, exists := seen[id]; exists {
				fail("validate %s: %s contains duplicate model id %q", path, section.name, id)
			}
			seen[id] = struct{}{}
		}
	}

	if len(catalog.Qwen) > 0 {
		seen := make(map[string]struct{}, len(catalog.Qwen))
		for i, model := range catalog.Qwen {
			id := strings.TrimSpace(model.ID)
			if id == "" {
				fail("validate %s: qwen[%d] has empty id", path, i)
			}
			if _, exists := seen[id]; exists {
				fail("validate %s: qwen contains duplicate model id %q", path, id)
			}
			seen[id] = struct{}{}
		}
	}

	fmt.Printf("validated models catalog: %s\n", path)
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
