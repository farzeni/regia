package claude

import (
	"encoding/json"
	"strings"
)

// FullOutput is a result's whole output, read again from the transcript (the
// Result keeps only a tail): a command's stdout then its stderr, or the
// result's text when the tool recorded no streams.
func FullOutput(path string, r *Result) (string, error) {
	line, err := RawLine(path, r.Off)
	if err != nil {
		return "", err
	}
	var e struct {
		Message struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
		Use json.RawMessage `json:"toolUseResult"`
	}
	if err := json.Unmarshal(line, &e); err != nil {
		return "", err
	}
	var streams struct {
		Stdout *string `json:"stdout"`
		Stderr string  `json:"stderr"`
	}
	if len(e.Use) > 0 && e.Use[0] == '{' && json.Unmarshal(e.Use, &streams) == nil && streams.Stdout != nil {
		out := strings.TrimRight(*streams.Stdout, "\n")
		if s := strings.TrimRight(streams.Stderr, "\n"); s != "" {
			out = strings.TrimLeft(out+"\n"+s, "\n")
		}
		return out, nil
	}
	var blocks []rawBlock
	if json.Unmarshal(e.Message.Content, &blocks) != nil {
		return "", nil
	}
	for _, b := range blocks {
		if b.Type == "tool_result" && b.ToolUseID == r.ID {
			return resultText(b.Content), nil
		}
	}
	return "", nil
}

// resultText is a tool_result's content as text: a string, or its text parts.
func resultText(c json.RawMessage) string {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s
	}
	var parts []struct{ Type, Text string }
	_ = json.Unmarshal(c, &parts)
	var ts []string
	for _, p := range parts {
		if p.Type == "text" {
			ts = append(ts, p.Text)
		}
	}
	return strings.Join(ts, "\n")
}
