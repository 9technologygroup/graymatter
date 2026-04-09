package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/angelnicolasc/graymatter/pkg/session"
)

func (s *Server) handleMemorySearch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	agentID, ok := getString(args, "agent_id")
	if !ok || agentID == "" {
		return toolError("agent_id is required")
	}
	query, ok := getString(args, "query")
	if !ok || query == "" {
		return toolError("query is required")
	}
	topK := getInt(args, "top_k", s.mem.Config().TopK)

	facts, err := s.mem.Recall(agentID, query)
	if err != nil {
		return toolError(fmt.Sprintf("recall error: %v", err))
	}

	if topK < len(facts) {
		facts = facts[:topK]
	}

	if len(facts) == 0 {
		return toolText(fmt.Sprintf("No memories found for agent %q matching %q.", agentID, query))
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d relevant memories for agent %q:\n\n", len(facts), agentID))
	for i, f := range facts {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, f))
	}
	return toolText(sb.String())
}

func (s *Server) handleMemoryAdd(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	agentID, ok := getString(args, "agent_id")
	if !ok || agentID == "" {
		return toolError("agent_id is required")
	}
	text, ok := getString(args, "text")
	if !ok || text == "" {
		return toolError("text is required")
	}

	if err := s.mem.Remember(agentID, text); err != nil {
		return toolError(fmt.Sprintf("remember error: %v", err))
	}

	return toolText(fmt.Sprintf("Memory stored for agent %q.", agentID))
}

func (s *Server) handleCheckpointSave(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	agentID, ok := getString(args, "agent_id")
	if !ok || agentID == "" {
		return toolError("agent_id is required")
	}

	var state map[string]any
	if stateStr, ok := getString(args, "state"); ok && stateStr != "" {
		if err := json.Unmarshal([]byte(stateStr), &state); err != nil {
			return toolError(fmt.Sprintf("state must be valid JSON: %v", err))
		}
	}

	store := s.mem.Store()
	if store == nil {
		return toolError("memory store not initialised")
	}

	cp := session.Checkpoint{
		AgentID:   agentID,
		CreatedAt: time.Now().UTC(),
		State:     state,
		Metadata:  map[string]string{"source": "mcp"},
	}
	saved, err := session.Save(store.DB(), cp)
	if err != nil {
		return toolError(fmt.Sprintf("checkpoint save error: %v", err))
	}

	return toolText(fmt.Sprintf("Checkpoint %q saved for agent %q.", saved.ID, agentID))
}

func (s *Server) handleCheckpointResume(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	agentID, ok := getString(args, "agent_id")
	if !ok || agentID == "" {
		return toolError("agent_id is required")
	}

	store := s.mem.Store()
	if store == nil {
		return toolError("memory store not initialised")
	}

	cp, err := session.Resume(store.DB(), agentID)
	if err != nil {
		return toolError(fmt.Sprintf("no checkpoint found for agent %q: %v", agentID, err))
	}

	stateJSON, _ := json.MarshalIndent(cp.State, "", "  ")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Checkpoint %q restored for agent %q.\n", cp.ID, agentID))
	sb.WriteString(fmt.Sprintf("Created: %s\n", cp.CreatedAt.Format(time.RFC3339)))
	if len(stateJSON) > 2 {
		sb.WriteString(fmt.Sprintf("State:\n%s\n", string(stateJSON)))
	}
	if len(cp.Messages) > 0 {
		sb.WriteString(fmt.Sprintf("Messages: %d turns\n", len(cp.Messages)))
	}
	return toolText(sb.String())
}
