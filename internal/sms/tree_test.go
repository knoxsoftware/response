package sms_test

import (
	"strings"
	"testing"

	"github.com/mattventura/respond/internal/sms"
)

const validYAML = `
greeting: "Hello! Press 1 or 2."
timeout_minutes: 30
nodes:
  root:
    prompt: "Press 1 for A, 2 for B."
    options:
      "1": node_a
      "2": node_b
  node_a:
    response: "You chose A."
  node_b:
    prompt: "Urgent? Y or N."
    options:
      Y: node_b_yes
      N: node_b_no
  node_b_yes:
    response: "Connecting you now."
    action: notify_responders
  node_b_no:
    response: "Email us."
`

func TestLoadTree_Valid(t *testing.T) {
	tree, err := sms.LoadTree(strings.NewReader(validYAML))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tree.Greeting != "Hello! Press 1 or 2." {
		t.Errorf("wrong greeting: %s", tree.Greeting)
	}
	if tree.TimeoutMinutes != 30 {
		t.Errorf("wrong timeout: %d", tree.TimeoutMinutes)
	}
	root, ok := tree.Nodes["root"]
	if !ok {
		t.Fatal("root node missing")
	}
	if root.Prompt != "Press 1 for A, 2 for B." {
		t.Errorf("wrong root prompt: %s", root.Prompt)
	}
	if root.Options["1"] != "node_a" {
		t.Errorf("wrong option 1: %s", root.Options["1"])
	}
}

func TestLoadTree_MissingNode(t *testing.T) {
	bad := strings.ReplaceAll(validYAML, "  node_a:", "  node_x:")
	_, err := sms.LoadTree(strings.NewReader(bad))
	if err == nil {
		t.Error("expected error for missing node reference, got nil")
	}
}

func TestLoadTree_BranchNodeMissingOptions(t *testing.T) {
	bad := `
greeting: hi
timeout_minutes: 30
nodes:
  root:
    prompt: "Choose"
`
	_, err := sms.LoadTree(strings.NewReader(bad))
	if err == nil {
		t.Error("expected error for branch node with no options")
	}
}
