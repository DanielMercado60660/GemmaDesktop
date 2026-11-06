package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strconv"
	"strings"
	"unicode/utf8"
)

const maxNativeStreamBytes = 32 << 20
const maxNativeToolArguments = 1 << 20

// scanSSE assembles data lines, not TCP reads. It handles UTF-8 and JSON split
// across arbitrary reads, CRLF, comments and multiline events. A partial event
// at EOF is an incomplete stream, never a final complete tool call.
func scanSSE(ctx context.Context, reader io.Reader, accept func([]byte) error) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), maxStreamLine)
	var data bytes.Buffer
	total, events := 0, 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := scanner.Bytes()
		total += len(line) + 1
		if total > maxNativeStreamBytes {
			return errors.New("native stream exceeds size limit")
		}
		if len(line) == 0 {
			if data.Len() > 0 {
				events++
				if events > 65536 {
					return errors.New("native stream exceeds event limit")
				}
				payload := bytes.TrimSuffix(data.Bytes(), []byte{'\n'})
				if !utf8.Valid(payload) {
					return errors.New("native stream contains invalid UTF-8")
				}
				if err := accept(payload); err != nil {
					return err
				}
				data.Reset()
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := bytes.Cut(line, []byte{':'})
		if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		if bytes.Equal(field, []byte("data")) {
			if data.Len()+len(value)+1 > maxStreamLine {
				return errors.New("native SSE event exceeds size limit")
			}
			data.Write(value)
			data.WriteByte('\n')
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read native stream: %w", err)
	}
	if data.Len() != 0 {
		return ErrIncompleteStream
	}
	return nil
}

type toolFragments struct {
	id        string
	name      strings.Builder
	arguments strings.Builder
}

type nativeStream struct {
	provider      ProviderKind
	model         string
	calls         map[int]*toolFragments
	finished      bool
	done          bool
	reason        string
	repetition    bool
	metrics       Metrics
	countsPresent bool
	usageDetails  UsageDetails
}

func (s *nativeStream) accept(data []byte, emit func(ChatChunk) error) error {
	if s.done {
		return errors.New("native server sent data after [DONE]")
	}
	if bytes.Equal(bytes.TrimSpace(data), []byte("[DONE]")) {
		if !s.finished {
			return ErrIncompleteStream
		}
		s.done = true
		return nil
	}
	var envelope struct {
		Model   string            `json:"model"`
		Choices []json.RawMessage `json:"choices"`
		Usage   json.RawMessage   `json:"usage"`
		Error   json.RawMessage   `json:"error"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode native stream envelope: %w", err)
	}
	if envelope.Model != "" && envelope.Model != s.model {
		return errors.New("native response model differs from selected model")
	}
	if len(envelope.Choices) > 1 {
		return errors.New("native stream returned multiple choices")
	}
	var header struct {
		Index         *int            `json:"index"`
		FinishReason  *string         `json:"finish_reason"`
		FinishDetails json.RawMessage `json:"finish_details"`
		Delta         json.RawMessage `json:"delta"`
	}
	wasFinished := s.finished
	if len(envelope.Choices) == 1 {
		if err := json.Unmarshal(envelope.Choices[0], &header); err != nil {
			return fmt.Errorf("decode native choice: %w", err)
		}
		if header.Index == nil || *header.Index != 0 {
			return errors.New("native stream requires choice index zero")
		}
		if header.FinishReason != nil {
			if s.finished {
				return errors.New("native stream repeated its finish reason")
			}
			s.finished, s.reason = true, *header.FinishReason
		}
	}
	// Observe counts before validating the final delta. A malformed tool call
	// does not erase the tokens the provider reports consuming.
	if err := s.usage(envelope.Usage); err != nil {
		return err
	}
	if len(envelope.Error) > 0 && string(envelope.Error) != "null" {
		return fmt.Errorf("native generation failed: %.8192s", envelope.Error)
	}
	if len(envelope.Choices) == 0 {
		if !s.finished || len(envelope.Usage) == 0 || string(envelope.Usage) == "null" {
			return errors.New("unexpected native stream event without a choice or terminal usage")
		}
		return nil
	}
	if header.FinishReason != nil {
		switch s.reason {
		case "stop", "tool_calls", "length":
		default:
			return fmt.Errorf("unsupported native finish reason %q", s.reason)
		}
	}
	if len(header.FinishDetails) > 0 && string(header.FinishDetails) != "null" {
		var details struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(header.FinishDetails, &details); err != nil {
			return fmt.Errorf("decode native finish details: %w", err)
		}
		if details.Type == "repetition_loop" {
			s.repetition = true
		} else if details.Type != "" {
			return fmt.Errorf("unsupported native finish details %q", details.Type)
		}
	}
	var delta struct {
		Role      string `json:"role"`
		Content   string `json:"content"`
		Reasoning string `json:"reasoning_content"`
		Tools     []struct {
			Index    *int   `json:"index"`
			ID       string `json:"id"`
			Type     string `json:"type"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	}
	if len(header.Delta) != 0 {
		if err := json.Unmarshal(header.Delta, &delta); err != nil {
			return fmt.Errorf("decode native delta: %w", err)
		}
	}
	if wasFinished {
		return errors.New("native server sent a choice after finish")
	}
	if delta.Role != "" && delta.Role != "assistant" {
		return errors.New("native response has non-assistant role")
	}
	for _, piece := range delta.Tools {
		if piece.Index == nil || *piece.Index < 0 || *piece.Index >= 64 {
			return errors.New("native tool delta requires an index between 0 and 63")
		}
		if piece.Type != "" && piece.Type != "function" {
			return errors.New("native tool delta has unsupported type")
		}
		call := s.calls[*piece.Index]
		if call == nil {
			call = &toolFragments{}
			s.calls[*piece.Index] = call
		}
		if piece.ID != "" {
			if len(piece.ID) > 512 || strings.ContainsAny(piece.ID, "\r\n\x00") || call.id != "" && piece.ID != call.id {
				return errors.New("native tool delta has an invalid or changing ID")
			}
			call.id = piece.ID
		}
		if call.name.Len()+len(piece.Function.Name) > 128 || call.arguments.Len()+len(piece.Function.Arguments) > maxNativeToolArguments {
			return errors.New("native tool call exceeds size limit")
		}
		call.name.WriteString(piece.Function.Name)
		call.arguments.WriteString(piece.Function.Arguments)
	}
	if emit != nil && (delta.Content != "" || delta.Reasoning != "") {
		return emit(ChatChunk{Model: s.model, Message: Message{Role: "assistant", Content: delta.Content, Thinking: delta.Reasoning}})
	}
	return nil
}

func (s *nativeStream) completeCalls() ([]ToolCall, error) {
	if s.reason == "length" {
		// Preserve the incomplete text/usage receipt, but never grant tool
		// authority to a response stopped by its generation limit.
		return nil, nil
	}
	if len(s.calls) > 0 && s.reason != "tool_calls" || len(s.calls) == 0 && s.reason == "tool_calls" {
		return nil, errors.New("native tool calls do not match the finish reason")
	}
	seen := map[string]bool{}
	calls := make([]ToolCall, 0, len(s.calls))
	for index := 0; index < len(s.calls); index++ {
		fragment := s.calls[index]
		if fragment == nil || fragment.id == "" || fragment.name.Len() == 0 {
			return nil, errors.New("incomplete native tool call identity")
		}
		if seen[fragment.id] {
			return nil, fmt.Errorf("duplicate native tool call ID %q", fragment.id)
		}
		seen[fragment.id] = true
		var arguments map[string]any
		decoder := json.NewDecoder(strings.NewReader(fragment.arguments.String()))
		decoder.UseNumber()
		if err := decoder.Decode(&arguments); err != nil || arguments == nil {
			return nil, errors.New("native tool arguments must be a complete JSON object")
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			return nil, errors.New("native tool arguments contain trailing JSON")
		}
		if _, err := exactHostNumbers(arguments); err != nil {
			return nil, err
		}
		calls = append(calls, ToolCall{ID: fragment.id, Type: "function", Function: ToolFunction{Index: index, Name: fragment.name.String(), Arguments: arguments}})
	}
	return calls, nil
}

// Host tool schemas currently consume float64 values. Reject any numeric value
// whose JSON round trip changes its mathematical decimal value before granting
// tool authority (including nested objects/arrays and integers above 2^53).
func exactHostNumbers(value any) (any, error) {
	switch v := value.(type) {
	case json.Number:
		if len(v) > 128 {
			return nil, errors.New("native tool number exceeds precision limit")
		}
		f, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return nil, errors.New("native tool number cannot be represented by host schema")
		}
		encoded, err := json.Marshal(f)
		if err != nil {
			return nil, err
		}
		// Avoid constructing arbitrarily large rationals for underflow/zero.
		if f == 0 {
			mantissa := strings.Split(strings.ToLower(string(v)), "e")[0]
			if strings.Trim(mantissa, "-0.") != "" {
				return nil, errors.New("native tool number loses precision in host schema")
			}
			return f, nil
		}
		original, ok := new(big.Rat).SetString(string(v))
		roundtrip, roundOK := new(big.Rat).SetString(string(encoded))
		if !ok || !roundOK || original.Cmp(roundtrip) != 0 {
			return nil, errors.New("native tool number loses precision in host schema")
		}
		return f, nil
	case map[string]any:
		for key, item := range v {
			converted, err := exactHostNumbers(item)
			if err != nil {
				return nil, err
			}
			v[key] = converted
		}
	case []any:
		for index, item := range v {
			converted, err := exactHostNumbers(item)
			if err != nil {
				return nil, err
			}
			v[index] = converted
		}
	}
	return value, nil
}
