package sms

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

type Node struct {
	Prompt   string            `yaml:"prompt"`
	Options  map[string]string `yaml:"options"`
	Response string            `yaml:"response"`
	Action   string            `yaml:"action"`
}

func (n *Node) IsTerminal() bool {
	return n.Response != ""
}

type Tree struct {
	Greeting       string          `yaml:"greeting"`
	TimeoutMinutes int             `yaml:"timeout_minutes"`
	Nodes          map[string]Node `yaml:"nodes"`
}

func LoadTree(r io.Reader) (*Tree, error) {
	var tree Tree
	if err := yaml.NewDecoder(r).Decode(&tree); err != nil {
		return nil, fmt.Errorf("parse tree: %w", err)
	}
	if err := validate(&tree); err != nil {
		return nil, err
	}
	return &tree, nil
}

func validate(tree *Tree) error {
	if _, ok := tree.Nodes["root"]; !ok {
		return fmt.Errorf("tree must have a 'root' node")
	}
	for name, node := range tree.Nodes {
		if node.IsTerminal() {
			continue
		}
		if len(node.Options) == 0 {
			return fmt.Errorf("node %q has no response and no options", name)
		}
		for opt, target := range node.Options {
			if _, ok := tree.Nodes[target]; !ok {
				return fmt.Errorf("node %q option %q references unknown node %q", name, opt, target)
			}
		}
	}
	return nil
}
