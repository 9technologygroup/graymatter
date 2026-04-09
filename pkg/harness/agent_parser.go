// Package harness provides local managed orchestration for GrayMatter agents.
// It implements the run/resume/kill lifecycle for agent processes backed by
// persistent bbolt checkpoints and the GrayMatter memory store.
package harness

import (
	"bytes"
	"fmt"
	"os"
	"strings"
)

const defaultModel = "claude-opus-4-6"

// InputVar describes a declared input variable in an agent file.
type InputVar struct {
	Name        string
	Description string
}

// AgentDef is the parsed representation of a SKILL.md-format agent file.
// All fields have variable substitution applied ({{varname}} replaced).
type AgentDef struct {
	Name         string
	Description  string
	Model        string // default: "claude-opus-4-6"
	SystemPrompt string
	Task         string
	InputVars    []InputVar
}

// ParseAgentFile reads the agent Markdown file at path, substitutes vars,
// and returns the parsed AgentDef. Returns an error if "name" is missing.
func ParseAgentFile(path string, vars map[string]string) (*AgentDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read agent file %q: %w", path, err)
	}
	return parseAgentBytes(data, vars)
}

// parseAgentBytes is the testable core of ParseAgentFile.
func parseAgentBytes(data []byte, vars map[string]string) (*AgentDef, error) {
	yamlBytes, body, err := parseFrontmatter(data)
	if err != nil {
		return nil, fmt.Errorf("parse frontmatter: %w", err)
	}

	fm := parseFrontmatterMap(yamlBytes)

	def := &AgentDef{
		Name:        fm["name"],
		Description: fm["description"],
		Model:       fm["model"],
	}
	if def.Name == "" {
		return nil, fmt.Errorf("agent file missing required field: name")
	}
	if def.Model == "" {
		def.Model = defaultModel
	}

	def.SystemPrompt = substituteVars(strings.TrimSpace(extractSection(body, "System Prompt")), vars)
	def.Task = substituteVars(strings.TrimSpace(extractSection(body, "Task")), vars)
	def.InputVars = parseInputsSection(extractSection(body, "Inputs"))

	return def, nil
}

// parseFrontmatter splits a Markdown file into YAML frontmatter and body.
// The frontmatter is delimited by "---" lines. No external YAML library is used.
func parseFrontmatter(content []byte) (yamlBytes []byte, body []byte, err error) {
	// No frontmatter — return the full content as body.
	if !bytes.HasPrefix(content, []byte("---")) {
		return nil, content, nil
	}

	// Skip the opening "---" line.
	rest := content[3:]
	if len(rest) > 0 && rest[0] == '\r' {
		rest = rest[1:]
	}
	if len(rest) > 0 && rest[0] == '\n' {
		rest = rest[1:]
	}

	// Find the closing "---" on its own line.
	idx := bytes.Index(rest, []byte("\n---"))
	if idx < 0 {
		return nil, content, fmt.Errorf("frontmatter opening --- found but closing --- not found")
	}
	yamlBytes = rest[:idx]
	body = rest[idx+4:] // skip "\n---"
	// Skip the newline (and optional \r) that follows the closing ---.
	if len(body) > 0 && body[0] == '\r' {
		body = body[1:]
	}
	if len(body) > 0 && body[0] == '\n' {
		body = body[1:]
	}
	return yamlBytes, body, nil
}

// parseFrontmatterMap parses simple "key: value" YAML lines.
// No nesting, no lists — exactly what SKILL.md frontmatter requires.
func parseFrontmatterMap(yamlBytes []byte) map[string]string {
	result := make(map[string]string)
	for _, line := range strings.Split(string(yamlBytes), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		result[strings.TrimSpace(key)] = strings.TrimSpace(val)
	}
	return result
}

// extractSection returns the text content of the "## heading" section in body.
// Returns an empty string if the heading is absent.
func extractSection(body []byte, heading string) string {
	target := "## " + heading
	lines := strings.Split(string(body), "\n")
	var capturing bool
	var buf strings.Builder
	for _, line := range lines {
		if strings.TrimSpace(line) == target {
			capturing = true
			continue
		}
		if capturing {
			if strings.HasPrefix(strings.TrimSpace(line), "## ") {
				break
			}
			buf.WriteString(line)
			buf.WriteByte('\n')
		}
	}
	return strings.TrimSpace(buf.String())
}

// parseInputsSection parses the bullet list under "## Inputs":
//
//	- varname: Description text
func parseInputsSection(section string) []InputVar {
	var vars []InputVar
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		line = line[2:]
		name, desc, _ := strings.Cut(line, ":")
		vars = append(vars, InputVar{
			Name:        strings.TrimSpace(name),
			Description: strings.TrimSpace(desc),
		})
	}
	return vars
}

// substituteVars replaces {{varname}} placeholders in text with values from vars.
// Unknown placeholders are left as-is.
func substituteVars(text string, vars map[string]string) string {
	if len(vars) == 0 {
		return text
	}
	pairs := make([]string, 0, len(vars)*2)
	for k, v := range vars {
		pairs = append(pairs, "{{"+k+"}}", v)
	}
	return strings.NewReplacer(pairs...).Replace(text)
}
