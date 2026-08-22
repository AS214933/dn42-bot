package service

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// SaveBackupBootstrapToConfig writes the backup bootstrap credentials
// (node_name, git_instance, git_org, api_token) into the `backup:` block of
// the agent's YAML config file. The edit is comment-preserving: the document
// is parsed into a yaml.Node tree, only the affected scalar values are
// rewritten (creating the `backup` mapping and missing keys as needed), and
// the result is re-encoded — comments and key order elsewhere survive.
// It returns false when the file did not need changing because all four
// fields were already present with the same values.
func SaveBackupBootstrapToConfig(path, nodeName, gitInstance, gitOrg, apiToken string) (bool, error) {
	if path == "" {
		return false, errors.New("config path is empty")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false, fmt.Errorf("read config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return false, fmt.Errorf("parse config: %w", err)
	}
	if len(doc.Content) == 0 {
		return false, errors.New("config is empty")
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return false, fmt.Errorf("config top level is %v, expected a mapping", root.Kind)
	}

	backupNode := mappingValue(root, "backup")
	if backupNode == nil {
		// Append a new `backup` mapping at the end of the top-level mapping.
		key := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "backup"}
		backupNode = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		root.Content = append(root.Content, key, backupNode)
	} else if backupNode.Kind != yaml.MappingNode {
		return false, fmt.Errorf("backup block is %v, expected a mapping", backupNode.Kind)
	}

	values := map[string]string{
		"node_name":    nodeName,
		"git_instance": gitInstance,
		"git_org":      gitOrg,
		"api_token":    apiToken,
	}
	changed := false
	for key, value := range values {
		if value == "" {
			continue
		}
		existing := mappingValue(backupNode, key)
		if existing != nil && existing.Kind == yaml.ScalarNode && existing.Value == value {
			continue
		}
		if existing != nil && existing.Kind == yaml.ScalarNode {
			existing.Value = value
			existing.Tag = "!!str"
			// Drop explicit quoting so yaml.v3 re-quotes only when needed.
			existing.Style &^= yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle
		} else if existing == nil {
			keyNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}
			valueNode := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
			backupNode.Content = append(backupNode.Content, keyNode, valueNode)
		} else {
			return false, fmt.Errorf("backup.%s has unexpected kind %v", key, existing.Kind)
		}
		changed = true
	}
	if !changed {
		return false, nil
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return false, fmt.Errorf("encode config: %w", err)
	}

	mode := os.FileMode(0644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	if err := os.WriteFile(path, out, mode); err != nil {
		return changed, fmt.Errorf("write config: %w", err)
	}
	return changed, nil
}

func mappingValue(mapping *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		k := mapping.Content[i]
		if k.Kind == yaml.ScalarNode && k.Value == key {
			return mapping.Content[i+1]
		}
	}
	return nil
}
