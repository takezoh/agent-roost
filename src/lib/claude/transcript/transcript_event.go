package transcript

import (
	"encoding/json"
	"fmt"
	"strings"
)

// parseLine turns a single JSONL line into zero or more Entry values.
// Unknown event types and invalid JSON produce no entries. UUID and
// ParentUUID are stamped onto every emitted entry from the line wrapper
// so callers (notably transcript.Tracker) can reconstruct the active
// conversation chain and ignore rewound branches.
func (p *Parser) parseLine(line []byte) []Entry { //nolint:funlen
	if len(line) == 0 {
		return nil
	}
	var head struct {
		Type       string `json:"type"`
		UUID       string `json:"uuid"`
		ParentUUID string `json:"parentUuid"`
	}
	if json.Unmarshal(line, &head) != nil {
		return nil
	}
	var entries []Entry
	switch head.Type {
	case "user":
		var u struct {
			Message       *jsonMessage    `json:"message"`
			ToolUseResult json.RawMessage `json:"toolUseResult"`
		}
		if json.Unmarshal(line, &u) != nil {
			return nil
		}
		entries = p.parseUserEntry(u.Message, u.ToolUseResult)
	case "assistant":
		var a struct {
			Message *jsonMessage `json:"message"`
		}
		if json.Unmarshal(line, &a) != nil {
			return nil
		}
		entries = p.parseAssistantEntry(a.Message)
	case "system":
		entries = parseSystemEntry(line)
	case "attachment":
		entries = parseAttachmentEntry(line)
	case "file-history-snapshot":
		entries = parseFileSnapshotEntry(line)
	case "custom-title":
		entries = parseCustomTitleEntry(line)
	case "agent-name":
		entries = parseAgentNameEntry(line)
	case "last-prompt":
		entries = parseLastPromptEntry(line)
	default:
		return nil
	}
	if head.UUID != "" {
		if len(entries) == 0 {
			// The line had a uuid but produced no displayable Entry
			// (e.g. an assistant turn whose only block is `thinking`
			// when ShowThinking is false). Emit a no-op chain stub so
			// downstream consumers like transcript.Tracker can keep
			// their parentUuid chain intact across such turns. Renderers
			// ignore KindUnknown entries with empty text.
			entries = []Entry{{Kind: KindUnknown}}
		}
		for i := range entries {
			entries[i].UUID = head.UUID
			entries[i].ParentUUID = head.ParentUUID
		}
	}
	return entries
}

// maybeInlineSubagent appends the subagent transcript directly under a
// Task/Agent tool_result Entry. No-op when the loader is unset, when the
// tool is not a subagent launcher, or when the result lacks an agentId.
func (p *Parser) maybeInlineSubagent(entries []Entry, e Entry) []Entry {
	if p.loader == nil {
		return entries
	}
	if e.ToolName != "Task" && e.ToolName != "Agent" {
		return entries
	}
	ar, ok := e.ToolResult.(AgentResult)
	if !ok || ar.AgentID == "" {
		return entries
	}
	sub := p.loader.Load(ar.AgentID, 0)
	if len(sub) == 0 {
		return entries
	}
	return append(entries, sub...)
}

func parseSystemEntry(line []byte) []Entry {
	var v struct {
		Subtype string `json:"subtype"`
		Level   string `json:"level"`
		Content string `json:"content"`
	}
	if json.Unmarshal(line, &v) != nil {
		return nil
	}
	text := v.Subtype
	if text == "" {
		text = "system"
	}
	if v.Level != "" && v.Level != "info" {
		text = v.Level + ":" + text
	}
	if snippet := firstLine(strings.TrimSpace(v.Content)); snippet != "" {
		text = text + "  " + truncate(snippet, 120)
	}
	return []Entry{{Kind: KindSystem, Text: text}}
}

func parseAttachmentEntry(line []byte) []Entry {
	var v struct {
		Attachment struct {
			Type         string   `json:"type"`
			AddedNames   []string `json:"addedNames"`
			RemovedNames []string `json:"removedNames"`
		} `json:"attachment"`
	}
	if json.Unmarshal(line, &v) != nil {
		return nil
	}
	if len(v.Attachment.AddedNames) == 0 && len(v.Attachment.RemovedNames) == 0 {
		return nil
	}
	var parts []string
	if n := len(v.Attachment.AddedNames); n > 0 {
		parts = append(parts, summarizeNameList("+", v.Attachment.AddedNames))
	}
	if n := len(v.Attachment.RemovedNames); n > 0 {
		parts = append(parts, summarizeNameList("-", v.Attachment.RemovedNames))
	}
	text := strings.Join(parts, "  ")
	if v.Attachment.Type != "" {
		text = v.Attachment.Type + "  " + text
	}
	return []Entry{{Kind: KindAttachment, Text: text}}
}

func summarizeNameList(prefix string, names []string) string {
	const maxShow = 3
	show := names
	suffix := ""
	if len(show) > maxShow {
		show = show[:maxShow]
		suffix = fmt.Sprintf(" +%d more", len(names)-maxShow)
	}
	return prefix + strings.Join(show, ",") + suffix
}

func parseFileSnapshotEntry(line []byte) []Entry {
	var v struct {
		Snapshot struct {
			TrackedFileBackups []struct {
				BackupFileName string `json:"backupFileName"`
			} `json:"trackedFileBackups"`
		} `json:"snapshot"`
	}
	if json.Unmarshal(line, &v) != nil {
		return nil
	}
	// Keep the entry quiet by default (Render returns ""); future PRs
	// can surface the count in session metadata instead.
	n := len(v.Snapshot.TrackedFileBackups)
	return []Entry{{Kind: KindFileSnapshot, Text: fmt.Sprintf("%d tracked", n)}}
}

func parseCustomTitleEntry(line []byte) []Entry {
	var v struct {
		CustomTitle string `json:"customTitle"`
	}
	if json.Unmarshal(line, &v) != nil || v.CustomTitle == "" {
		return nil
	}
	return []Entry{{Kind: KindCustomTitle, Text: v.CustomTitle}}
}

func parseAgentNameEntry(line []byte) []Entry {
	var v struct {
		AgentName string `json:"agentName"`
	}
	if json.Unmarshal(line, &v) != nil || v.AgentName == "" {
		return nil
	}
	return []Entry{{Kind: KindAgentName, Text: v.AgentName}}
}

func parseLastPromptEntry(line []byte) []Entry {
	var v struct {
		LastPrompt string `json:"lastPrompt"`
	}
	if json.Unmarshal(line, &v) != nil {
		return nil
	}
	return []Entry{{Kind: KindLastPrompt, Text: v.LastPrompt}}
}

type jsonMessage struct {
	Content json.RawMessage `json:"content"`
}

func (p *Parser) parseUserEntry(msg *jsonMessage, topToolUseResult json.RawMessage) []Entry {
	if msg == nil {
		return nil
	}
	var s string
	if json.Unmarshal(msg.Content, &s) == nil {
		s = stripSystemTags(strings.TrimSpace(s))
		if s == "" {
			return nil
		}
		return []Entry{{Kind: KindUser, Text: s}}
	}
	var blocks []userBlock
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return nil
	}
	var entries []Entry
	toolResultConsumed := false
	for _, b := range blocks {
		switch b.Type {
		case "text":
			if t := strings.TrimSpace(b.Text); t != "" {
				// Block-text user content always originates from Claude
				// itself (skill bootstrap, interrupt markers, command
				// output echoes) — never from the human at the CLI.
				// Mark Synthetic so transcript.Tracker excludes it from
				// the lastPrompt chain.
				entries = append(entries, Entry{Kind: KindUser, Text: t, Synthetic: true})
			}
		case "tool_result":
			e := p.buildToolResultEntry(b, topToolUseResult, &toolResultConsumed)
			entries = append(entries, e)
			entries = p.maybeInlineSubagent(entries, e)
		}
	}
	return entries
}

type userBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
	ToolUseID string          `json:"tool_use_id"`
}

func (p *Parser) buildToolResultEntry(b userBlock, topToolUseResult json.RawMessage, consumed *bool) Entry {
	e := Entry{
		Kind:      KindToolResult,
		ToolUseID: b.ToolUseID,
		IsError:   b.IsError,
	}
	if name, ok := p.toolUseNames[b.ToolUseID]; ok {
		e.ToolName = name
		if !*consumed && len(topToolUseResult) > 0 {
			if tr := ParseToolUseResult(name, topToolUseResult); tr != nil {
				e.ToolResult = tr
				*consumed = true
			}
		}
	}
	if e.ToolResult == nil {
		e.ToolResult = GenericResult{Chars: contentLen(b.Content)}
	}
	return e
}

func (p *Parser) parseAssistantEntry(msg *jsonMessage) []Entry {
	if msg == nil {
		return nil
	}
	var blocks []struct {
		Type     string          `json:"type"`
		Text     string          `json:"text"`
		Thinking string          `json:"thinking"`
		Name     string          `json:"name"`
		ID       string          `json:"id"`
		Input    json.RawMessage `json:"input"`
	}
	if json.Unmarshal(msg.Content, &blocks) != nil {
		return nil
	}
	var entries []Entry
	for _, b := range blocks {
		switch b.Type {
		case "text":
			t := strings.TrimSpace(b.Text)
			if t != "" {
				entries = append(entries, Entry{Kind: KindAssistantText, Text: t})
			}
		case "thinking":
			if !p.opts.ShowThinking {
				continue
			}
			// Claude now stores the body in "thinking"; older logs used "text".
			t := strings.TrimSpace(b.Thinking)
			if t == "" {
				t = strings.TrimSpace(b.Text)
			}
			if t != "" {
				entries = append(entries, Entry{Kind: KindAssistantThinking, Text: t})
			}
		case "tool_use":
			if b.ID != "" && b.Name != "" {
				p.toolUseNames[b.ID] = b.Name
			}
			entries = append(entries, Entry{
				Kind:      KindToolUse,
				ToolName:  b.Name,
				ToolUseID: b.ID,
				ToolInput: ParseToolInput(b.Name, b.Input),
			})
		}
	}
	return entries
}

func contentLen(raw json.RawMessage) int {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return len(s)
	}
	var blocks []struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &blocks) == nil {
		n := 0
		for _, b := range blocks {
			n += len(b.Text)
		}
		return n
	}
	return 0
}

var systemTagPrefixes = []string{
	"<local-command-caveat>",
	"<command-name>",
	"<system-reminder>",
	"<bash-input>",
	"<bash-stdout>",
	"<bash-stderr>",
}

func stripSystemTags(s string) string {
	for _, prefix := range systemTagPrefixes {
		if strings.HasPrefix(s, prefix) {
			return ""
		}
	}
	return s
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}
