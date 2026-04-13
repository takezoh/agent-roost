package transcript

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type Entry struct {
	Text string
}

type Snapshot struct {
	Title                string
	LastPrompt           string
	LastAssistantMessage string
	StatusLine           string
}

type Parser struct {
	title                string
	lastPrompt           string
	lastAssistantMessage string
	model                string
	totalTokens          int
	contextWindow        int
	currentTurnID        string
}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Reset() {
	*p = Parser{}
}

func (p *Parser) Snapshot() Snapshot {
	return Snapshot{
		Title:                p.title,
		LastPrompt:           p.lastPrompt,
		LastAssistantMessage: p.lastAssistantMessage,
		StatusLine:           p.statusLine(),
	}
}

func (p *Parser) ParseLines(raw []byte) []Entry {
	if len(raw) == 0 {
		return nil
	}
	var out []Entry
	for _, line := range bytes.Split(raw, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		entry, ok := p.parseLine(line)
		if ok {
			out = append(out, entry)
		}
	}
	return out
}

func (p *Parser) parseLine(line []byte) (Entry, bool) {
	var item rolloutLine
	if err := json.Unmarshal(line, &item); err != nil {
		return Entry{}, false
	}
	switch item.Type {
	case "session_meta":
		p.parseSessionMeta(item.Payload)
		return Entry{}, false
	case "turn_context":
		p.parseTurnContext(item.Payload)
		return p.renderTurnContext(item.Payload)
	case "event_msg":
		return p.parseEvent(item.Payload)
	case "response_item":
		return p.parseResponseItem(item.Payload)
	case "compacted":
		return p.renderCompacted(item.Payload)
	default:
		return Entry{}, false
	}
}

func (p *Parser) parseSessionMeta(payload json.RawMessage) {
	var meta struct {
		ModelProvider string `json:"model_provider"`
	}
	_ = json.Unmarshal(payload, &meta)
}

func (p *Parser) parseTurnContext(payload json.RawMessage) {
	var ctx struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(payload, &ctx)
	if ctx.Model != "" {
		p.model = ctx.Model
	}
}

func (p *Parser) renderTurnContext(payload json.RawMessage) (Entry, bool) {
	var ctx struct {
		Model            string `json:"model"`
		Cwd              string `json:"cwd"`
		ApprovalPolicy   string `json:"approval_policy"`
		CollabMode       any    `json:"collaboration_mode"`
		ReasoningEffort  any    `json:"effort"`
		RealtimeActive   any    `json:"realtime_active"`
		SandboxPolicyRaw any    `json:"sandbox_policy"`
	}
	_ = json.Unmarshal(payload, &ctx)
	var parts []string
	if ctx.Model != "" {
		parts = append(parts, "model="+ctx.Model)
	}
	if ctx.ApprovalPolicy != "" {
		parts = append(parts, "approval="+ctx.ApprovalPolicy)
	}
	if ctx.Cwd != "" {
		parts = append(parts, "cwd="+ctx.Cwd)
	}
	if len(parts) == 0 {
		return Entry{}, false
	}
	return Entry{Text: "[turn] " + strings.Join(parts, " ")}, true
}

func (p *Parser) parseEvent(payload json.RawMessage) (Entry, bool) {
	var probe struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return Entry{}, false
	}
	switch probe.Type {
	case "user_message":
		return p.parseUserMessage(payload)
	case "agent_message":
		return p.parseAgentMessage(payload)
	case "thread_name_updated":
		return p.parseThreadNameUpdated(payload)
	case "token_count":
		return p.parseTokenCount(payload)
	case "turn_started":
		return p.parseTurnStarted(payload)
	case "turn_complete":
		return p.parseTurnComplete(payload)
	case "turn_aborted":
		return p.parseTurnAborted(payload)
	case "thread_rolled_back":
		return p.parseThreadRolledBack(payload)
	case "agent_reasoning":
		return p.parseAgentReasoning(payload)
	default:
		return Entry{Text: "[event] " + probe.Type}, true
	}
}

func (p *Parser) parseUserMessage(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(payload, &ev)
	text := strings.TrimSpace(stripUserMessagePrefix(ev.Message))
	if text == "" {
		return Entry{}, false
	}
	if p.title == "" {
		p.title = text
	}
	p.lastPrompt = text
	return Entry{Text: "[context] " + text}, true
}

func (p *Parser) parseAgentMessage(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(payload, &ev)
	text := strings.TrimSpace(ev.Message)
	if text == "" {
		return Entry{}, false
	}
	p.lastAssistantMessage = text
	return Entry{Text: text}, true
}

func (p *Parser) parseThreadNameUpdated(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		ThreadName string `json:"thread_name"`
	}
	_ = json.Unmarshal(payload, &ev)
	name := strings.TrimSpace(ev.ThreadName)
	if name == "" {
		return Entry{}, false
	}
	p.title = name
	return Entry{Text: "[title] " + name}, true
}

func (p *Parser) parseTokenCount(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		Info *struct {
			TotalTokenUsage *struct {
				TotalTokens int `json:"total_tokens"`
			} `json:"total_token_usage"`
			ModelContextWindow int `json:"model_context_window"`
		} `json:"info"`
	}
	_ = json.Unmarshal(payload, &ev)
	if ev.Info == nil {
		return Entry{}, false
	}
	if ev.Info.TotalTokenUsage != nil && ev.Info.TotalTokenUsage.TotalTokens > 0 {
		p.totalTokens = ev.Info.TotalTokenUsage.TotalTokens
	}
	if ev.Info.ModelContextWindow > 0 {
		p.contextWindow = ev.Info.ModelContextWindow
	}
	return Entry{Text: "[tokens] " + p.statusLine()}, true
}

func (p *Parser) parseTurnStarted(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		TurnID string `json:"turn_id"`
	}
	_ = json.Unmarshal(payload, &ev)
	p.currentTurnID = ev.TurnID
	label := "[turn] started"
	if ev.TurnID != "" {
		label += " " + ev.TurnID
	}
	return Entry{Text: appendPromptContext(label, p.lastPrompt)}, true
}

func (p *Parser) parseTurnComplete(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		TurnID           string `json:"turn_id"`
		LastAgentMessage string `json:"last_agent_message"`
	}
	_ = json.Unmarshal(payload, &ev)
	if msg := strings.TrimSpace(ev.LastAgentMessage); msg != "" {
		p.lastAssistantMessage = msg
	}
	label := "[turn] complete"
	if ev.TurnID != "" {
		label += " " + ev.TurnID
	}
	if ev.TurnID != "" && ev.TurnID == p.currentTurnID {
		p.currentTurnID = ""
	}
	return Entry{Text: appendPromptContext(label, p.lastPrompt)}, true
}

func (p *Parser) parseTurnAborted(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		TurnID string `json:"turn_id"`
		Reason string `json:"reason"`
	}
	_ = json.Unmarshal(payload, &ev)
	var parts []string
	if ev.TurnID != "" {
		parts = append(parts, ev.TurnID)
	}
	if ev.Reason != "" {
		parts = append(parts, "reason="+ev.Reason)
	}
	label := "[turn] aborted"
	if len(parts) > 0 {
		label += " " + strings.Join(parts, " ")
	}
	if ev.TurnID != "" && ev.TurnID == p.currentTurnID {
		p.currentTurnID = ""
	}
	return Entry{Text: appendPromptContext(label, p.lastPrompt)}, true
}

func (p *Parser) parseThreadRolledBack(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		NumTurns int `json:"num_turns"`
	}
	_ = json.Unmarshal(payload, &ev)
	if ev.NumTurns <= 0 {
		return Entry{Text: "[rollback]"}, true
	}
	return Entry{Text: appendPromptContext(fmt.Sprintf("[rollback] %d turn(s)", ev.NumTurns), p.lastPrompt)}, true
}

func (p *Parser) parseAgentReasoning(payload json.RawMessage) (Entry, bool) {
	var ev struct {
		Text string `json:"text"`
	}
	_ = json.Unmarshal(payload, &ev)
	text := strings.TrimSpace(ev.Text)
	if text == "" {
		return Entry{}, false
	}
	return Entry{Text: "[reasoning] " + text}, true
}

func (p *Parser) parseResponseItem(payload json.RawMessage) (Entry, bool) {
	var probe struct {
		Type string `json:"type"`
		Role string `json:"role"`
	}
	if err := json.Unmarshal(payload, &probe); err != nil {
		return Entry{}, false
	}
	switch probe.Type {
	case "message":
		return p.parseMessageItem(payload, probe.Role)
	case "function_call", "custom_tool_call", "web_search_call", "local_shell_call":
		return p.renderToolCall(payload, probe.Type)
	case "function_call_output", "custom_tool_call_output", "local_shell_call_output":
		return p.renderToolResult(payload, probe.Type)
	default:
		return Entry{}, false
	}
}

func (p *Parser) parseMessageItem(payload json.RawMessage, role string) (Entry, bool) {
	var item struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(payload, &item); err != nil {
		return Entry{}, false
	}
	var parts []string
	for _, content := range item.Content {
		if content.Type == "output_text" || content.Type == "input_text" {
			text := strings.TrimSpace(content.Text)
			if text != "" {
				parts = append(parts, text)
			}
		}
	}
	if len(parts) == 0 {
		return Entry{}, false
	}
	text := strings.Join(parts, "\n")
	switch role {
	case "assistant":
		p.lastAssistantMessage = text
		return Entry{}, false
	case "user":
		return Entry{}, false
	default:
		return Entry{Text: text}, true
	}
}

func (p *Parser) renderCompacted(payload json.RawMessage) (Entry, bool) {
	var item struct {
		Message string `json:"message"`
	}
	_ = json.Unmarshal(payload, &item)
	text := strings.TrimSpace(item.Message)
	if text == "" {
		return Entry{Text: "[compacted]"}, true
	}
	return Entry{Text: "[compacted] " + text}, true
}

func (p *Parser) statusLine() string {
	var parts []string
	if p.model != "" {
		parts = append(parts, p.model)
	}
	if p.totalTokens > 0 {
		parts = append(parts, comma(p.totalTokens)+" tok")
	}
	if p.contextWindow > 0 {
		parts = append(parts, comma(p.contextWindow)+" ctx")
	}
	return strings.Join(parts, " | ")
}

func RenderEntries(entries []Entry) string {
	if len(entries) == 0 {
		return ""
	}
	lines := make([]string, 0, len(entries))
	for _, entry := range entries {
		text := strings.TrimSpace(entry.Text)
		if text != "" {
			lines = append(lines, text)
		}
	}
	return strings.Join(lines, "\n")
}

type rolloutLine struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

func (p *Parser) renderToolCall(payload json.RawMessage, fallback string) (Entry, bool) {
	var item struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		CallID    string `json:"call_id"`
		Arguments string `json:"arguments"`
		Action    struct {
			Type    string `json:"type"`
			Query   string `json:"query"`
			Command string `json:"command"`
		} `json:"action"`
		Command string `json:"command"`
		Query   string `json:"query"`
	}
	_ = json.Unmarshal(payload, &item)
	name := firstNonEmpty(item.Name, item.Type, fallback)
	detail := firstNonEmpty(
		previewText(item.Command),
		previewText(item.Query),
		previewText(item.Action.Command),
		previewText(item.Action.Query),
		previewText(item.Arguments),
	)
	if detail == "" {
		return Entry{Text: appendPromptContext("▸ "+name, p.lastPrompt)}, true
	}
	return Entry{Text: appendPromptContext("▸ "+name+" "+detail, p.lastPrompt)}, true
}

func (p *Parser) renderToolResult(payload json.RawMessage, fallback string) (Entry, bool) {
	var item struct {
		Type   string `json:"type"`
		CallID string `json:"call_id"`
		Output string `json:"output"`
	}
	_ = json.Unmarshal(payload, &item)
	name := firstNonEmpty(item.Type, fallback)
	detail := previewText(item.Output)
	if detail == "" {
		return Entry{Text: appendPromptContext("← "+name, p.lastPrompt)}, true
	}
	return Entry{Text: appendPromptContext("← "+name+" "+detail, p.lastPrompt)}, true
}

func appendPromptContext(label, prompt string) string {
	prompt = previewText(prompt)
	if prompt == "" {
		return label
	}
	return label + ` <- "` + prompt + `"`
}

func stripUserMessagePrefix(text string) string {
	const prefix = "## My request for Codex:"
	text = strings.TrimSpace(text)
	if idx := strings.Index(text, prefix); idx >= 0 {
		return strings.TrimSpace(text[idx+len(prefix):])
	}
	return text
}

func comma(n int) string {
	s := strconv.Itoa(n)
	if len(s) <= 3 {
		return s
	}
	sign := ""
	if s[0] == '-' {
		sign, s = "-", s[1:]
	}
	var out []byte
	pre := len(s) % 3
	if pre == 0 {
		pre = 3
	}
	out = append(out, s[:pre]...)
	for i := pre; i < len(s); i += 3 {
		out = append(out, ',')
		out = append(out, s[i:i+3]...)
	}
	return sign + string(out)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func previewText(text string) string {
	text = strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	const max = 80
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max-3]) + "..."
}
