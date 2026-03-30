package config

import (
	"testing"

	"gopkg.in/yaml.v3"
)

// FuzzAgentConfigUnmarshal fuzzes the YAML unmarshaling of AgentConfig
// to find panics or unexpected crashes from malformed input.
func FuzzAgentConfigUnmarshal(f *testing.F) {
	// Seed corpus with valid and edge-case YAML
	f.Add([]byte(`
description: test agent
nodes:
  - name: start
    type: llm
    system: hello
    prompt: world
    output_model:
      response: str
flow:
  - from: START
    to: start
  - from: start
    to: END
`))

	f.Add([]byte(`
description: drill suite
type: drill_suite
suite_config:
  template: ubuntu
  service:
    start_command: "echo hello"
    port: 8080
    ready_check:
      endpoint: /health
      timeout_seconds: 30
nodes: []
flow: []
`))

	f.Add([]byte(`
description: legacy test config
type: test
test_config:
  timeout: 60
  step_timeout: 30
nodes: []
flow: []
`))

	f.Add([]byte(`
description: conditional flow
nodes:
  - name: check
    type: llm
    system: classify
    prompt: input
    output_model:
      result: str
flow:
  - from: START
    to: check
  - from: check
    edges:
      - to: END
        condition: "lambda x: x['result'] == 'yes'"
      - to: START
        condition: "lambda x: x['result'] == 'no'"
`))

	f.Add([]byte(`{}`))
	f.Add([]byte(``))
	f.Add([]byte(`null`))
	f.Add([]byte(`[]`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var cfg AgentConfig
		// We don't care about the error — we're looking for panics
		_ = yaml.Unmarshal(data, &cfg)
	})
}

// FuzzAppConfigUnmarshal fuzzes the YAML unmarshaling of AppConfig.
func FuzzAppConfigUnmarshal(f *testing.F) {
	f.Add([]byte(`
default_provider: openai
default_model: gpt-4
web_capable_tools:
  - web_fetch
  - web_search
`))

	f.Add([]byte(`
chat:
  max_turns: 50
  temperature: 0.7
scheduler:
  enabled: true
  interval: 300
`))

	f.Add([]byte(``))
	f.Add([]byte(`null`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var cfg AppConfig
		_ = yaml.Unmarshal(data, &cfg)
	})
}
