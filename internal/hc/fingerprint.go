package hc

import (
	"crypto/sha256"
	"encoding/json"
)

// snapshotFingerprint hashes everything the phone renders so polls that
// produce identical output do not re-broadcast.
func snapshotFingerprint(s Snapshot) string {
	type agentKey struct {
		PaneID      string `json:"p"`
		Status      string `json:"st"`
		TabLabel    string `json:"tl"`
		WorkspaceID string `json:"w"`
		Agent       string `json:"a"`
		Name        string `json:"n"`
		Cwd         string `json:"c"`
		Focused     bool   `json:"f"`
	}
	agents := make([]agentKey, 0, len(s.Agents))
	for _, a := range s.Agents {
		if a == nil {
			continue
		}
		agents = append(agents, agentKey{
			PaneID: a.PaneID, Status: a.Status, TabLabel: a.TabLabel,
			WorkspaceID: a.WorkspaceID, Agent: a.Agent, Name: a.Name,
			Cwd: a.Cwd, Focused: a.Focused,
		})
	}
	encoded, err := json.Marshal(struct {
		Available bool       `json:"a"`
		Message   string     `json:"m"`
		ServerID  string     `json:"s"`
		Spaces    []Space    `json:"sp"`
		Agents    []agentKey `json:"ag"`
	}{
		Available: s.Available, Message: s.Message, ServerID: s.ServerID,
		Spaces: s.Spaces, Agents: agents,
	})
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(encoded)
	return string(sum[:])
}
