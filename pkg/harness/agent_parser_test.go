package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseAgentFile_Basic(t *testing.T) {
	content := `---
name: test-agent
description: A test agent
model: claude-opus-4-6
---

## System Prompt
You are helpful.

## Task
Do something useful.

## Inputs
- task: The task to perform
`
	path := writeTemp(t, content)
	def, err := ParseAgentFile(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Name != "test-agent" {
		t.Errorf("Name = %q, want %q", def.Name, "test-agent")
	}
	if def.Description != "A test agent" {
		t.Errorf("Description = %q, want %q", def.Description, "A test agent")
	}
	if def.Model != "claude-opus-4-6" {
		t.Errorf("Model = %q, want %q", def.Model, "claude-opus-4-6")
	}
	if def.SystemPrompt != "You are helpful." {
		t.Errorf("SystemPrompt = %q, want %q", def.SystemPrompt, "You are helpful.")
	}
	if def.Task != "Do something useful." {
		t.Errorf("Task = %q, want %q", def.Task, "Do something useful.")
	}
	if len(def.InputVars) != 1 || def.InputVars[0].Name != "task" {
		t.Errorf("InputVars = %+v, want [{Name:task ...}]", def.InputVars)
	}
}

func TestParseAgentFile_DefaultModel(t *testing.T) {
	content := `---
name: no-model-agent
---

## Task
Do it.
`
	path := writeTemp(t, content)
	def, err := ParseAgentFile(path, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.Model != defaultModel {
		t.Errorf("Model = %q, want default %q", def.Model, defaultModel)
	}
}

func TestParseAgentFile_VarSubstitution(t *testing.T) {
	content := `---
name: subst-agent
---

## System Prompt
Hello, {{name}}.

## Task
Follow up with {{contact}} about {{topic}}.
`
	path := writeTemp(t, content)
	vars := map[string]string{
		"name":    "World",
		"contact": "Maria",
		"topic":   "the demo",
	}
	def, err := ParseAgentFile(path, vars)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if def.SystemPrompt != "Hello, World." {
		t.Errorf("SystemPrompt = %q, want %q", def.SystemPrompt, "Hello, World.")
	}
	if def.Task != "Follow up with Maria about the demo." {
		t.Errorf("Task = %q, want %q", def.Task, "Follow up with Maria about the demo.")
	}
}

func TestParseAgentFile_MissingName(t *testing.T) {
	content := `---
description: no name here
---

## Task
Do it.
`
	path := writeTemp(t, content)
	_, err := ParseAgentFile(path, nil)
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
}

func TestParseAgentFile_NoFrontmatter(t *testing.T) {
	content := `## System Prompt
You are helpful.
`
	path := writeTemp(t, content)
	_, err := ParseAgentFile(path, nil)
	// Missing name → should error
	if err == nil {
		t.Fatal("expected error for missing name without frontmatter, got nil")
	}
}

func TestParseAgentFile_FileNotFound(t *testing.T) {
	_, err := ParseAgentFile(filepath.Join(t.TempDir(), "nonexistent.md"), nil)
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
}

func TestParseInputsSection(t *testing.T) {
	section := `- task: The main task
- agent: The agent name
- format: Output format`
	vars := parseInputsSection(section)
	if len(vars) != 3 {
		t.Fatalf("len = %d, want 3", len(vars))
	}
	if vars[0].Name != "task" || vars[0].Description != "The main task" {
		t.Errorf("vars[0] = %+v", vars[0])
	}
	if vars[1].Name != "agent" {
		t.Errorf("vars[1].Name = %q", vars[1].Name)
	}
}

func TestSubstituteVars_NoVars(t *testing.T) {
	got := substituteVars("hello {{world}}", nil)
	if got != "hello {{world}}" {
		t.Errorf("got %q, want unchanged", got)
	}
}

func TestSubstituteVars_Unknown(t *testing.T) {
	got := substituteVars("hello {{unknown}}", map[string]string{"other": "x"})
	if got != "hello {{unknown}}" {
		t.Errorf("got %q, want placeholder left as-is", got)
	}
}

// writeTemp creates a temporary .md file with content and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}
