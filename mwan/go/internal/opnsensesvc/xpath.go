package opnsensesvc

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"github.com/antchfx/xmlquery"
)

// xpathGet evaluates expr against the XML in input and returns string
// representations of every matching node. Element matches are
// serialized as XML; attribute and text matches are returned as their
// string value.
func xpathGet(input []byte, expr string) ([]string, error) {
	if expr == "" {
		return nil, errors.New("xpathGet: empty expression")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, fmt.Errorf("xpathGet: parse: %w", err)
	}
	nodes, err := xmlquery.QueryAll(doc, expr)
	if err != nil {
		return nil, fmt.Errorf("xpathGet: query %q: %w", expr, err)
	}
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeToString(n))
	}
	return out, nil
}

// xpathSet sets the InnerText of every node matched by expr to
// newValue. Returns the new bytes and the count of changed nodes.
func xpathSet(input []byte, expr, newValue string) ([]byte, int, error) {
	if expr == "" {
		return nil, 0, errors.New("xpathSet: empty expression")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, 0, fmt.Errorf("xpathSet: parse: %w", err)
	}
	nodes, err := xmlquery.QueryAll(doc, expr)
	if err != nil {
		return nil, 0, fmt.Errorf("xpathSet: query %q: %w", expr, err)
	}
	if len(nodes) == 0 {
		return input, 0, nil
	}
	for _, n := range nodes {
		// Replace all child text nodes with a single text node
		// holding newValue. Preserves the element identity (any
		// attributes stay).
		stripChildText(n)
		text := &xmlquery.Node{Type: xmlquery.TextNode, Data: newValue}
		xmlquery.AddChild(n, text)
	}
	out := []byte(doc.OutputXML(true))
	return out, len(nodes), nil
}

// xpathDelete removes every node matched by expr from its parent.
// Returns new bytes and the count of deleted nodes.
func xpathDelete(input []byte, expr string) ([]byte, int, error) {
	if expr == "" {
		return nil, 0, errors.New("xpathDelete: empty expression")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, 0, fmt.Errorf("xpathDelete: parse: %w", err)
	}
	nodes, err := xmlquery.QueryAll(doc, expr)
	if err != nil {
		return nil, 0, fmt.Errorf("xpathDelete: query %q: %w", expr, err)
	}
	if len(nodes) == 0 {
		return input, 0, nil
	}
	for _, n := range nodes {
		xmlquery.RemoveFromTree(n)
	}
	out := []byte(doc.OutputXML(true))
	return out, len(nodes), nil
}

// nodeToString serializes a node to a string. Elements roundtrip
// through xmlquery's OutputXML. Attributes and text nodes are
// returned as their value.
func nodeToString(n *xmlquery.Node) string {
	switch n.Type {
	case xmlquery.AttributeNode:
		return n.InnerText()
	case xmlquery.TextNode:
		return n.Data
	default:
		// ElementNode and friends: emit XML so the caller can see
		// children.
		var b strings.Builder
		b.WriteString(n.OutputXML(true))
		return b.String()
	}
}

// stripChildText removes every child text node from n. Used by
// xpathSet so we can replace the inner text without leaving the
// previous text node in place.
func stripChildText(n *xmlquery.Node) {
	child := n.FirstChild
	for child != nil {
		next := child.NextSibling
		if child.Type == xmlquery.TextNode {
			xmlquery.RemoveFromTree(child)
		}
		child = next
	}
}
