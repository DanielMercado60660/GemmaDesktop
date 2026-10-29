package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"

	"gemmadesktop.local/app/internal/engine"
)

type generationControls struct {
	thinking        string
	maxOutputTokens int
	temperature     float64
	toolProtocol    string
}

const defaultAgentTemperature = 0.2

// Missing legacy fields have explicit defaults. Invalid supplied fields never
// fall back silently, and automatic thinking is represented by an absent wire
// option rather than a claim about the model's capabilities.
func agentGenerationControls(config Doc) (generationControls, error) {
	controls := generationControls{thinking: "auto", maxOutputTokens: engine.DefaultMaxOutputTokens, temperature: defaultAgentTemperature, toolProtocol: "native"}
	if value, present := config["thinking"]; present {
		thinking, ok := value.(string)
		if !ok || (thinking != "auto" && thinking != "on" && thinking != "off") {
			return controls, errors.New("thinking must be auto, on, or off")
		}
		controls.thinking = thinking
	}
	if value, present := config["maxOutputTokens"]; present {
		number, ok := finiteNumber(value)
		if !ok || math.Trunc(number) != number || number < engine.MinMaxOutputTokens || number > engine.MaxMaxOutputTokens {
			return controls, outputTokensError()
		}
		controls.maxOutputTokens = int(number)
	}
	if value, present := config["temperature"]; present {
		temperature, ok := finiteNumber(value)
		if !ok || temperature < 0 || temperature > 2 {
			return controls, errors.New("temperature must be a finite number from 0 to 2")
		}
		controls.temperature = temperature
	}
	if value, present := config["toolProtocol"]; present {
		protocol, ok := value.(string)
		if !ok || (protocol != "native" && protocol != engine.ToolProtocolStructuredJSON) {
			return controls, errors.New("toolProtocol must be native or structured_json")
		}
		controls.toolProtocol = protocol
	}
	return controls, nil
}

func finiteNumber(value any) (float64, bool) {
	var number float64
	switch v := value.(type) {
	case int:
		number = float64(v)
	case int64:
		number = float64(v)
	case float64:
		number = v
	case json.Number:
		var err error
		number, err = v.Float64()
		if err != nil {
			return 0, false
		}
	default:
		return 0, false
	}
	return number, !math.IsNaN(number) && !math.IsInf(number, 0)
}

func outputTokensError() error {
	return fmt.Errorf("maxOutputTokens must be an integer from %d to %d", engine.MinMaxOutputTokens, engine.MaxMaxOutputTokens)
}

func (c generationControls) apply(req *engine.Request) {
	req.Think = nil
	if c.thinking == "on" {
		req.Think = true
	} else if c.thinking == "off" {
		req.Think = false
	}
	req.MaxOutputTokens = c.maxOutputTokens
	req.Temperature = c.temperature
	req.ToolProtocol = c.toolProtocol
}
