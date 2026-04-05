package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var (
	agentCache    = map[string]*AgentDefinition{}
	workflowCache = map[string]*WorkflowDefinition{}
	schemaCache   = map[string]map[string]interface{}{}
	log           = NewLogger("loader")
)

// FindProjectRoot walks up from the working directory looking for
// agentic.config.yaml, an agentic-spec/ directory, or an agents/ directory.
func FindProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "agentic.config.yaml")); err == nil {
			return dir
		}
		if info, err := os.Stat(filepath.Join(dir, "agentic-spec")); err == nil && info.IsDir() {
			return dir
		}
		if info, err := os.Stat(filepath.Join(dir, "agents")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	cwd, _ := os.Getwd()
	return cwd
}

// LoadAgent reads agentic-spec/agents/<id>/agent.yaml and its optional prompt.md.
func LoadAgent(agentID string) (*AgentDefinition, error) {
	if cached, ok := agentCache[agentID]; ok {
		return cached, nil
	}

	root := FindProjectRoot()
	agentDir := filepath.Join(root, "agentic-spec", "agents", agentID)
	yamlPath := filepath.Join(agentDir, "agent.yaml")

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent %s: %w", agentID, err)
	}

	var def AgentDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("failed to parse agent %s YAML: %w", agentID, err)
	}

	promptPath := filepath.Join(agentDir, "prompt.md")
	if promptData, err := os.ReadFile(promptPath); err == nil {
		def.Prompt = string(promptData)
	}

	agentCache[agentID] = &def
	log.Debug("loaded agent", map[string]interface{}{"agent": agentID})
	return &def, nil
}

// LoadAllAgents reads every agent under agentic-spec/agents/.
func LoadAllAgents() (map[string]*AgentDefinition, error) {
	root := FindProjectRoot()
	agentsDir := filepath.Join(root, "agentic-spec", "agents")

	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to list agents directory: %w", err)
	}

	result := make(map[string]*AgentDefinition)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		def, err := LoadAgent(id)
		if err != nil {
			log.Warn("skipping agent", map[string]interface{}{"agent": id, "error": err.Error()})
			continue
		}
		result[id] = def
	}
	return result, nil
}

// LoadWorkflow reads agentic-spec/workflows/<name>.yaml.
func LoadWorkflow(name string) (*WorkflowDefinition, error) {
	if cached, ok := workflowCache[name]; ok {
		return cached, nil
	}

	root := FindProjectRoot()
	yamlPath := filepath.Join(root, "agentic-spec", "workflows", name+".yaml")

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read workflow %s: %w", name, err)
	}

	def, err := parseWorkflowYAML(data)
	if err != nil {
		return nil, fmt.Errorf("failed to parse workflow %s: %w", name, err)
	}

	workflowCache[name] = def
	log.Debug("loaded workflow", map[string]interface{}{"workflow": name})
	return def, nil
}

// parseWorkflowYAML handles custom unmarshaling of workflow YAML.
// Each step can be a WorkflowStep, ParallelBlock, LoopBlock, or ForEachBlock.
func parseWorkflowYAML(data []byte) (*WorkflowDefinition, error) {
	var raw struct {
		Name        string                   `yaml:"name"`
		Description string                   `yaml:"description"`
		Version     string                   `yaml:"version"`
		Input       map[string]InputParamDef `yaml:"input"`
		Steps       []yaml.Node              `yaml:"steps"`
		Output      map[string]string        `yaml:"output"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}

	def := &WorkflowDefinition{
		Name:        raw.Name,
		Description: raw.Description,
		Version:     raw.Version,
		Input:       raw.Input,
		Output:      raw.Output,
	}

	for i, node := range raw.Steps {
		stepType := detectStepType(&node)

		switch stepType {
		case "parallel":
			pb, err := parseParallelBlock(&node)
			if err != nil {
				return nil, fmt.Errorf("step %d: %w", i, err)
			}
			def.Steps = append(def.Steps, pb)

		case "loop":
			lb, err := parseLoopBlock(&node)
			if err != nil {
				return nil, fmt.Errorf("step %d: %w", i, err)
			}
			def.Steps = append(def.Steps, lb)

		case "for_each":
			feb, err := parseForEachBlock(&node)
			if err != nil {
				return nil, fmt.Errorf("step %d: %w", i, err)
			}
			def.Steps = append(def.Steps, feb)

		default:
			ws, err := parseWorkflowStep(&node)
			if err != nil {
				return nil, fmt.Errorf("step %d: %w", i, err)
			}
			def.Steps = append(def.Steps, ws)
		}
	}

	return def, nil
}

// detectStepType inspects YAML mapping keys to determine the step type.
func detectStepType(node *yaml.Node) string {
	if node.Kind != yaml.MappingNode {
		return "step"
	}
	for j := 0; j < len(node.Content)-1; j += 2 {
		switch node.Content[j].Value {
		case "parallel":
			return "parallel"
		case "loop":
			return "loop"
		case "for_each":
			return "for_each"
		}
	}
	return "step"
}

// parseWorkflowStep parses a regular agent or sub-workflow step.
func parseWorkflowStep(node *yaml.Node) (*WorkflowStep, error) {
	var ws WorkflowStep
	if err := node.Decode(&ws); err != nil {
		return nil, fmt.Errorf("failed to decode step: %w", err)
	}

	// Parse the next: field manually
	nextField, err := extractNextField(node)
	if err != nil {
		return nil, fmt.Errorf("step %s: %w", ws.ID, err)
	}
	ws.Next = nextField

	return &ws, nil
}

// parseParallelBlock parses a parallel: block.
func parseParallelBlock(node *yaml.Node) (*ParallelBlock, error) {
	// The YAML shape is: { parallel: { id, join, branches: [...] } }
	var wrapper struct {
		Parallel struct {
			ID       string           `yaml:"id"`
			Join     string           `yaml:"join"`
			Branches []ParallelBranch `yaml:"branches"`
		} `yaml:"parallel"`
	}
	if err := node.Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode parallel block: %w", err)
	}

	pb := &ParallelBlock{
		ID:       wrapper.Parallel.ID,
		Join:     wrapper.Parallel.Join,
		Branches: wrapper.Parallel.Branches,
	}
	if pb.Join == "" {
		pb.Join = "all"
	}

	// Check for next: inside the parallel mapping
	nextField, err := extractNextFieldFromKey(node, "parallel")
	if err != nil {
		return nil, fmt.Errorf("parallel %s: %w", pb.ID, err)
	}
	pb.Next = nextField

	return pb, nil
}

// parseLoopBlock parses a loop: block.
func parseLoopBlock(node *yaml.Node) (*LoopBlock, error) {
	var wrapper struct {
		Loop struct {
			ID            string                 `yaml:"id"`
			Agent         string                 `yaml:"agent"`
			Workflow      string                 `yaml:"workflow"`
			Input         map[string]interface{} `yaml:"input"`
			Config        map[string]interface{} `yaml:"config"`
			Until         string                 `yaml:"until"`
			MaxIterations int                    `yaml:"max_iterations"`
			Retry         *RetryConfig           `yaml:"retry"`
			Fallback      *FallbackConfig        `yaml:"fallback"`
		} `yaml:"loop"`
	}
	if err := node.Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode loop block: %w", err)
	}

	lb := &LoopBlock{
		ID:            wrapper.Loop.ID,
		Agent:         wrapper.Loop.Agent,
		Workflow:      wrapper.Loop.Workflow,
		Input:         wrapper.Loop.Input,
		Config:        wrapper.Loop.Config,
		Until:         wrapper.Loop.Until,
		MaxIterations: wrapper.Loop.MaxIterations,
		Retry:         wrapper.Loop.Retry,
		Fallback:      wrapper.Loop.Fallback,
	}

	nextField, err := extractNextFieldFromKey(node, "loop")
	if err != nil {
		return nil, fmt.Errorf("loop %s: %w", lb.ID, err)
	}
	lb.Next = nextField

	return lb, nil
}

// parseForEachBlock parses a for_each: block.
func parseForEachBlock(node *yaml.Node) (*ForEachBlock, error) {
	var wrapper struct {
		ForEach struct {
			ID             string                 `yaml:"id"`
			Agent          string                 `yaml:"agent"`
			Workflow       string                 `yaml:"workflow"`
			Input          map[string]interface{} `yaml:"input"`
			Config         map[string]interface{} `yaml:"config"`
			Collection     string                 `yaml:"collection"`
			MaxConcurrency int                    `yaml:"max_concurrency"`
			Retry          *RetryConfig           `yaml:"retry"`
			Fallback       *FallbackConfig        `yaml:"fallback"`
		} `yaml:"for_each"`
	}
	if err := node.Decode(&wrapper); err != nil {
		return nil, fmt.Errorf("failed to decode for_each block: %w", err)
	}

	feb := &ForEachBlock{
		ID:             wrapper.ForEach.ID,
		Agent:          wrapper.ForEach.Agent,
		Workflow:        wrapper.ForEach.Workflow,
		Input:          wrapper.ForEach.Input,
		Config:         wrapper.ForEach.Config,
		Collection:     wrapper.ForEach.Collection,
		MaxConcurrency: wrapper.ForEach.MaxConcurrency,
		Retry:          wrapper.ForEach.Retry,
		Fallback:       wrapper.ForEach.Fallback,
	}

	nextField, err := extractNextFieldFromKey(node, "for_each")
	if err != nil {
		return nil, fmt.Errorf("for_each %s: %w", feb.ID, err)
	}
	feb.Next = nextField

	return feb, nil
}

// extractNextField looks for a "next" key in a YAML mapping node and parses it.
func extractNextField(node *yaml.Node) (*NextField, error) {
	if node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for j := 0; j < len(node.Content)-1; j += 2 {
		if node.Content[j].Value == "next" {
			return parseNextNode(node.Content[j+1])
		}
	}
	return nil, nil
}

// extractNextFieldFromKey looks for "next" inside the value of a specific key.
// e.g., for parallel blocks, we look for next: inside the "parallel:" mapping value.
func extractNextFieldFromKey(node *yaml.Node, key string) (*NextField, error) {
	if node.Kind != yaml.MappingNode {
		return nil, nil
	}
	for j := 0; j < len(node.Content)-1; j += 2 {
		if node.Content[j].Value == key {
			innerNode := node.Content[j+1]
			return extractNextField(innerNode)
		}
	}
	return nil, nil
}

// parseNextNode parses a next: YAML value into a NextField.
// Handles: string target, switch object, or if object.
func parseNextNode(node *yaml.Node) (*NextField, error) {
	// Simple string target: next: "step-id" or next: _end
	if node.Kind == yaml.ScalarNode {
		return &NextField{Target: node.Value}, nil
	}

	// Mapping: switch or if
	if node.Kind == yaml.MappingNode {
		keys := map[string]*yaml.Node{}
		for j := 0; j < len(node.Content)-1; j += 2 {
			keys[node.Content[j].Value] = node.Content[j+1]
		}

		// switch: { switch, cases, default }
		if switchNode, ok := keys["switch"]; ok {
			sn := &SwitchNext{
				Expression: switchNode.Value,
				Cases:      map[string]string{},
			}
			if casesNode, ok := keys["cases"]; ok && casesNode.Kind == yaml.MappingNode {
				for j := 0; j < len(casesNode.Content)-1; j += 2 {
					sn.Cases[casesNode.Content[j].Value] = casesNode.Content[j+1].Value
				}
			}
			if defNode, ok := keys["default"]; ok {
				sn.Default = defNode.Value
			}
			return &NextField{Switch: sn}, nil
		}

		// if: { if, then, else }
		if ifNode, ok := keys["if"]; ok {
			in := &IfNext{
				Condition: ifNode.Value,
			}
			if thenNode, ok := keys["then"]; ok {
				in.Then = thenNode.Value
			}
			if elseNode, ok := keys["else"]; ok {
				in.Else = elseNode.Value
			}
			return &NextField{If: in}, nil
		}

		return nil, fmt.Errorf("next: mapping must contain 'switch' or 'if' key")
	}

	return nil, fmt.Errorf("next: must be a string or mapping, got %v", node.Kind)
}

// LoadSchema reads agentic-spec/schemas/<name>.json and returns the parsed object.
func LoadSchema(name string) (map[string]interface{}, error) {
	if cached, ok := schemaCache[name]; ok {
		return cached, nil
	}

	root := FindProjectRoot()
	jsonPath := filepath.Join(root, "agentic-spec", "schemas", name+".json")

	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read schema %s: %w", name, err)
	}

	var schema map[string]interface{}
	if err := json.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema %s: %w", name, err)
	}

	schemaCache[name] = schema
	log.Debug("loaded schema", map[string]interface{}{"schema": name})
	return schema, nil
}
