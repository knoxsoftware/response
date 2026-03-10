package sms

import (
	"context"
	"fmt"
	"strings"
)

type SMSStore interface {
	GetNode(ctx context.Context, phone string) (string, error)
	Upsert(ctx context.Context, phone, node string) error
	Delete(ctx context.Context, phone string) error
}

type Response struct {
	Message  string
	Terminal bool
	Action   string
}

type Engine struct {
	tree  *Tree
	store SMSStore
}

func NewEngine(tree *Tree, store SMSStore) *Engine {
	return &Engine{tree: tree, store: store}
}

// Handle processes an inbound SMS from phone with body text.
func (e *Engine) Handle(ctx context.Context, phone, body string) (*Response, error) {
	currentNode, err := e.store.GetNode(ctx, phone)
	if err != nil {
		// No session — start at root
		if err := e.store.Upsert(ctx, phone, "root"); err != nil {
			return nil, fmt.Errorf("create session: %w", err)
		}
		root := e.tree.Nodes["root"]
		return &Response{Message: e.tree.Greeting + "\n" + root.Prompt}, nil
	}

	node, ok := e.tree.Nodes[currentNode]
	if !ok {
		e.store.Delete(ctx, phone)
		return &Response{Message: "Sorry, something went wrong. " + e.tree.Greeting}, nil
	}

	if node.IsTerminal() {
		// Already at terminal — reset and re-greet
		e.store.Delete(ctx, phone)
		root := e.tree.Nodes["root"]
		e.store.Upsert(ctx, phone, "root")
		return &Response{Message: e.tree.Greeting + "\n" + root.Prompt}, nil
	}

	input := strings.TrimSpace(strings.ToUpper(body))
	nextKey, ok := node.Options[input]
	if !ok {
		// Try case-insensitive match
		for k, v := range node.Options {
			if strings.EqualFold(k, input) {
				nextKey = v
				ok = true
				break
			}
		}
	}
	if !ok {
		return &Response{Message: "Sorry, I didn't understand that.\n" + node.Prompt}, nil
	}

	next := e.tree.Nodes[nextKey]
	if next.IsTerminal() {
		e.store.Delete(ctx, phone)
		return &Response{
			Message:  next.Response,
			Terminal: true,
			Action:   next.Action,
		}, nil
	}

	e.store.Upsert(ctx, phone, nextKey)
	return &Response{Message: next.Prompt}, nil
}
