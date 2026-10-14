package antigravity

import (
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
	"time"
)

type snapshot struct {
	home        string
	diagnostics source.SourceDiagnostics
	projectMap  map[string]*projectRecord
	sessionMap  map[string]*sessionRecord
	messageMap  map[string]*messageRecord
	seenSteps   map[string]bool
	ordered     []*messageRecord
}

type projectRecord struct {
	ID       string
	Name     string
	Worktree string
}

type sessionRecord struct {
	ID          string
	Title       string
	ProjectID   string
	ProjectName string
	Directory   string
	Created     time.Time
	Updated     time.Time
	Messages    []*messageRecord
}

type messageRecord struct {
	Entry          stats.MessageEntry
	TextParts      []stats.MessagePart
	ReasoningParts []stats.MessagePart
	ToolParts      []stats.ToolPart
	projectID      string
}

func newMessage(session *sessionRecord, project *projectRecord, id, role string, timestamp time.Time) *messageRecord {
	projectID := "unknown"
	if project != nil {
		projectID = project.ID
	}
	return &messageRecord{
		Entry: stats.MessageEntry{
			SourceID:    antigravitySourceID,
			ID:          id,
			SessionID:   session.ID,
			Role:        role,
			TimeCreated: timestamp.UTC(),
		},
		projectID: projectID,
	}
}

func register(snap *snapshot, session *sessionRecord, msg *messageRecord) {
	if msg == nil {
		return
	}
	snap.messageMap[msg.Entry.ID] = msg
	snap.ordered = append(snap.ordered, msg)
	session.Messages = append(session.Messages, msg)
	updateSessionTimes(session, msg.Entry.TimeCreated)
}

func (s *snapshot) ensureProject(id, name, worktree string) *projectRecord {
	if id == "" {
		id = "unknown"
	}
	if name == "" {
		name = id
	}
	project := s.projectMap[id]
	if project == nil {
		project = &projectRecord{ID: id, Name: name, Worktree: worktree}
		s.projectMap[id] = project
	}
	if project.Worktree == "" && worktree != "" {
		project.Worktree = worktree
	}
	return project
}

func (s *snapshot) ensureSession(id string, project *projectRecord, directory string) *sessionRecord {
	if id == "" {
		id = "unknown-session"
	}
	if project == nil {
		project = s.ensureProject("unknown", "unknown", directory)
	}
	session := s.sessionMap[id]
	if session == nil {
		session = &sessionRecord{
			ID: id, ProjectID: project.ID, ProjectName: project.Name, Directory: directory,
		}
		s.sessionMap[id] = session
	}
	if session.Directory == "" && directory != "" {
		session.Directory = directory
	}
	return session
}

func updateSessionTimes(session *sessionRecord, timestamp time.Time) {
	if session == nil || timestamp.IsZero() {
		return
	}
	if session.Created.IsZero() || timestamp.Before(session.Created) {
		session.Created = timestamp.UTC()
	}
	if session.Updated.IsZero() || timestamp.After(session.Updated) {
		session.Updated = timestamp.UTC()
	}
}

func positive(value int64) int64 {
	if value > 0 {
		return value
	}
	return 0
}

func (m *messageRecord) detail() *stats.MessageDetail {
	if m == nil {
		return nil
	}
	text := append([]stats.MessagePart{}, m.TextParts...)
	reasoning := append([]stats.MessagePart{}, m.ReasoningParts...)
	tools := append([]stats.ToolPart{}, m.ToolParts...)
	if text == nil {
		text = []stats.MessagePart{}
	}
	if reasoning == nil {
		reasoning = []stats.MessagePart{}
	}
	if tools == nil {
		tools = []stats.ToolPart{}
	}
	entry := m.Entry
	entry.Tokens = cloneTokens(entry.Tokens)
	entry.CostProvenance = cloneProvenance(entry.CostProvenance)
	return &stats.MessageDetail{
		MessageEntry: entry,
		Content: stats.MessageContent{
			TextParts: text, ReasoningParts: reasoning, ToolParts: tools,
		},
	}
}
