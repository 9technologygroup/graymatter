package session

import bolt "go.etcd.io/bbolt"

// Resume loads the latest checkpoint for agentID and returns it, ready to
// reconstruct conversation context for the next agent run.
//
// Typical usage:
//
//	cp, err := session.Resume(store.DB(), "my-agent")
//	if err == nil {
//	    messages = append(cp.Messages, newUserMessage)
//	    state    = cp.State
//	}
func Resume(db *bolt.DB, agentID string) (*Checkpoint, error) {
	return Latest(db, agentID)
}
