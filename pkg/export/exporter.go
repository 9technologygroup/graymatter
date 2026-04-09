// Package export writes GrayMatter memories to human-readable files.
// Three formats are supported: plain markdown, Obsidian vault, and JSON.
package export

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/angelnicolasc/graymatter/pkg/memory"
)

// Format names the output format for an export run.
type Format string

const (
	FormatMarkdown Format = "markdown"
	FormatObsidian Format = "obsidian"
	FormatJSON     Format = "json"
)

// Exporter writes a slice of facts to an output directory.
type Exporter interface {
	Export(facts []memory.Fact, outDir string) error
}

// New returns an Exporter for the given format.
func New(f Format) (Exporter, error) {
	switch f {
	case FormatMarkdown:
		return &MarkdownExporter{}, nil
	case FormatObsidian:
		return &ObsidianExporter{}, nil
	case FormatJSON:
		return &JSONExporter{}, nil
	default:
		return nil, fmt.Errorf("unknown export format: %q (valid: markdown, obsidian, json)", f)
	}
}

// JSONExporter writes all facts as a single pretty-printed JSON array.
type JSONExporter struct{}

func (e *JSONExporter) Export(facts []memory.Fact, outDir string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(outDir, "memories.json"), data, 0o644)
}
