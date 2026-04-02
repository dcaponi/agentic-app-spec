package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var (
	agentCache    = map[string]*AgentDefinition{}
	workflowCache = map[string]*WorkflowDefinition{}
	schemaCache   = map[string]map[string]interface{}{}
	routerCache   = map[string]*RouterDefinition{}
	log           = NewLogger("loader")
)

// FindProjectRoot walks up from the working directory looking for
// agentic.config.yaml or an agents/ directory.
func FindProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "agentic.config.yaml")); err == nil {
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
	// Fallback to cwd
	cwd, _ := os.Getwd()
	return cwd
}

// LoadAgent reads agents/<id>/agent.yaml and its optional prompt.md.
func LoadAgent(agentID string) (*AgentDefinition, error) {
	if cached, ok := agentCache[agentID]; ok {
		return cached, nil
	}

	root := FindProjectRoot()
	agentDir := filepath.Join(root, "agents", agentID)
	yamlPath := filepath.Join(agentDir, "agent.yaml")

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read agent %s: %w", agentID, err)
	}

	var def AgentDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("failed to parse agent %s YAML: %w", agentID, err)
	}

	// Load prompt.md if it exists
	promptPath := filepath.Join(agentDir, "prompt.md")
	if promptData, err := os.ReadFile(promptPath); err == nil {
		def.Prompt = string(promptData)
	}

	agentCache[agentID] = &def
	log.Debug("loaded agent", map[string]interface{}{"agent": agentID})
	return &def, nil
}

// LoadAllAgents reads every agent under agents/.
func LoadAllAgents() (map[string]*AgentDefinition, error) {
	root := FindProjectRoot()
	agentsDir := filepath.Join(root, "agents")

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

// LoadRouter reads routers/<id>/router.yaml and its optional prompt.md.
func LoadRouter(routerID string) (*RouterDefinition, error) {
	if cached, ok := routerCache[routerID]; ok {
		return cached, nil
	}

	root := FindProjectRoot()
	routerDir := filepath.Join(root, "routers", routerID)
	if _, err := os.Stat(routerDir); err != nil {
		return nil, fmt.Errorf("router directory not found: %s", routerDir)
	}
	yamlPath := filepath.Join(routerDir, "router.yaml")

	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read router %s: %w", routerID, err)
	}

	var def RouterDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("failed to parse router %s YAML: %w", routerID, err)
	}

	// Load prompt.md if it exists
	promptPath := filepath.Join(routerDir, "prompt.md")
	if promptData, err := os.ReadFile(promptPath); err == nil {
		def.Prompt = strings.TrimSpace(string(promptData))
	}

	routerCache[routerID] = &def
	log.Debug("loaded router", map[string]interface{}{"router": routerID})
	return &def, nil
}

// LoadWorkflow reads workflows/<name>.yaml.
func LoadWorkflow(name string) (*WorkflowDefinition, error) {
	if cached, ok := workflowCache[name]; ok {
		return cached, nil
	}

	root := FindProjectRoot()
	yamlPath := filepath.Join(root, "workflows", name+".yaml")

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

// parseWorkflowYAML handles custom unmarshaling of workflow YAML, where each
// step can be either a plain WorkflowStep or a ParallelGroup.
func parseWorkflowYAML(data []byte) (*WorkflowDefinition, error) {
	// First pass: parse into a raw map so we can handle steps specially.
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
		isParallel := false
		isRoute := false
		if node.Kind == yaml.MappingNode {
			for j := 0; j < len(node.Content)-1; j += 2 {
				switch node.Content[j].Value {
				case "parallel":
					isParallel = true
				case "route":
					isRoute = true
				}
			}
		}

		if isParallel {
			var pg ParallelGroup
			if err := node.Decode(&pg); err != nil {
				return nil, fmt.Errorf("step %d: failed to decode parallel group: %w", i, err)
			}
			def.Steps = append(def.Steps, &pg)
		} else if isRoute {
			// The YAML has a top-level "route:" key whose value is the RouteBlock
			var wrapper struct {
				Route RouteBlock `yaml:"route"`
			}
			if err := node.Decode(&wrapper); err != nil {
				return nil, fmt.Errorf("step %d: failed to decode route block: %w", i, err)
			}
			def.Steps = append(def.Steps, &wrapper.Route)
		} else {
			var ws WorkflowStep
			if err := node.Decode(&ws); err != nil {
				return nil, fmt.Errorf("step %d: failed to decode step: %w", i, err)
			}
			def.Steps = append(def.Steps, &ws)
		}
	}

	return def, nil
}

// LoadSchema reads schemas/<name>.json and returns the parsed object.
func LoadSchema(name string) (map[string]interface{}, error) {
	if cached, ok := schemaCache[name]; ok {
		return cached, nil
	}

	root := FindProjectRoot()
	jsonPath := filepath.Join(root, "schemas", name+".json")

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
