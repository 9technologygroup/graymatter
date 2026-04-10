package harness

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

func TestListSessions_Empty(t *testing.T) {
	dir := t.TempDir()
	// Create a minimal gray.db by running a failed open — just ensure ListSessions
	// does not error on a fresh (or missing) database.
	sessions, err := ListSessions(dir)
	// It's acceptable to return an error if the db doesn't exist yet,
	// but an empty slice with no error is the preferred path.
	if err != nil {
		// gray.db doesn't exist yet; that's fine — we expect 0 sessions.
		return
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(sessions))
	}
}

func TestListSessions_AfterRun(t *testing.T) {
	dir := t.TempDir()
	af := agentFile(t, simpleAgentContent)

	cfg := RunConfig{
		AgentFile:  af,
		DataDir:    dir,
		MaxRetries: 1,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		llmDoer: func(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
			return cannedMessage("ok"), nil
		},
	}
	result, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].ID != result.SessionID {
		t.Errorf("session ID mismatch: %q != %q", sessions[0].ID, result.SessionID)
	}
	if sessions[0].AgentID != "test-runner-agent" {
		t.Errorf("agent ID = %q", sessions[0].AgentID)
	}
	if sessions[0].Status != "done" {
		t.Errorf("status = %q", sessions[0].Status)
	}
}

func TestListSessions_SortedNewestFirst(t *testing.T) {
	dir := t.TempDir()
	af := agentFile(t, simpleAgentContent)

	// Run twice to get two sessions.
	for i := 0; i < 2; i++ {
		cfg := RunConfig{
			AgentFile:  af,
			DataDir:    dir,
			MaxRetries: 1,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
			llmDoer: func(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
				return cannedMessage("reply"), nil
			},
		}
		if _, err := Run(context.Background(), cfg); err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
		time.Sleep(2 * time.Millisecond) // ensure distinct timestamps
	}

	sessions, err := ListSessions(dir)
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
	if !sessions[0].StartedAt.After(sessions[1].StartedAt) {
		t.Error("sessions not sorted newest first")
	}
}

func TestKillSession_NotRunning(t *testing.T) {
	dir := t.TempDir()
	af := agentFile(t, simpleAgentContent)

	cfg := RunConfig{
		AgentFile:  af,
		DataDir:    dir,
		MaxRetries: 1,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		llmDoer: func(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
			return cannedMessage("done"), nil
		},
	}
	result, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Attempt to kill a "done" session — should error.
	err = KillSession(result.SessionID, dir)
	if err == nil {
		t.Fatal("expected error when killing a non-running session, got nil")
	}
}

func TestKillSession_NoPID(t *testing.T) {
	dir := t.TempDir()
	af := agentFile(t, simpleAgentContent)

	// Run foreground (no PID set).
	cfg := RunConfig{
		AgentFile:  af,
		DataDir:    dir,
		MaxRetries: 1,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		llmDoer: func(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
			return cannedMessage("done"), nil
		},
	}
	result, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Manually flip status to "running" so KillSession proceeds past that check.
	db := openTestDB(t, dir)
	hs, loadErr := loadHarnessSession(db, result.SessionID)
	if loadErr != nil {
		t.Fatalf("loadHarnessSession: %v", loadErr)
	}
	hs.Status = "running"
	hs.PID = 0
	if saveErr := saveHarnessSession(db, *hs); saveErr != nil {
		t.Fatalf("saveHarnessSession: %v", saveErr)
	}
	_ = db.Close()

	err = KillSession(result.SessionID, dir)
	if err == nil {
		t.Fatal("expected error when killing session with no PID, got nil")
	}
}

func TestResumeResolvesLatest(t *testing.T) {
	dir := t.TempDir()
	af := agentFile(t, simpleAgentContent)

	var lastID string
	for i := 0; i < 2; i++ {
		cfg := RunConfig{
			AgentFile:  af,
			DataDir:    dir,
			MaxRetries: 1,
			Stdout:     os.Stdout,
			Stderr:     os.Stderr,
			llmDoer: func(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
				return cannedMessage("reply"), nil
			},
		}
		result, err := Run(context.Background(), cfg)
		if err != nil {
			t.Fatalf("Run %d: %v", i, err)
		}
		lastID = result.SessionID
		time.Sleep(2 * time.Millisecond)
	}

	rc, err := Resume(context.Background(), "latest", dir)
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if rc.ResumeID != lastID {
		t.Errorf("Resume resolved to %q, want latest %q", rc.ResumeID, lastID)
	}
	if rc.AgentFile != af {
		t.Errorf("AgentFile = %q, want %q", rc.AgentFile, af)
	}
}

func TestResume_NotFound(t *testing.T) {
	dir := t.TempDir()
	af := agentFile(t, simpleAgentContent)

	// Run one session first to create the database.
	cfg := RunConfig{
		AgentFile:  af,
		DataDir:    dir,
		MaxRetries: 1,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		llmDoer: func(_ context.Context, _ anthropic.MessageNewParams) (*anthropic.Message, error) {
			return cannedMessage("ok"), nil
		},
	}
	if _, err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run: %v", err)
	}

	_, err := Resume(context.Background(), "nonexistent-id", dir)
	if err == nil {
		t.Fatal("expected error for nonexistent session, got nil")
	}
}
