package opnsensesvc

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/antchfx/xmlquery"
)

func xpathGetWithLog(
	ctx context.Context,
	log *slog.Logger,
	input []byte,
	expr string,
) ([]string, error) {
	if expr == "" {
		return nil, errors.New("xpathGet: empty expression")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, logWrappedErrorContext(ctx, log,
			"opnsensesvc: XPathGet parse failed", "xpathGet: parse", err)
	}
	nodes, err := xmlquery.QueryAll(doc, expr)
	if err != nil {
		return nil, logWrappedErrorContext(ctx, log,
			"opnsensesvc: XPathGet query failed", "xpathGet: query "+expr, err,
			slog.String("expression", expr))
	}
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, nodeToString(n))
	}
	return out, nil
}

func xpathSetWithLog(
	ctx context.Context,
	log *slog.Logger,
	input []byte,
	expr string,
	newValue string,
) ([]byte, int, error) {
	if expr == "" {
		return nil, 0, errors.New("xpathSet: empty expression")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, 0, logWrappedErrorContext(ctx, log,
			"opnsensesvc: XPathSet parse failed", "xpathSet: parse", err)
	}
	nodes, err := xmlquery.QueryAll(doc, expr)
	if err != nil {
		return nil, 0, logWrappedErrorContext(ctx, log,
			"opnsensesvc: XPathSet query failed", "xpathSet: query "+expr, err,
			slog.String("expression", expr))
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

func xpathDeleteWithLog(
	ctx context.Context,
	log *slog.Logger,
	input []byte,
	expr string,
) ([]byte, int, error) {
	if expr == "" {
		return nil, 0, errors.New("xpathDelete: empty expression")
	}
	doc, err := xmlquery.Parse(bytes.NewReader(input))
	if err != nil {
		return nil, 0, logWrappedErrorContext(ctx, log,
			"opnsensesvc: XPathDelete parse failed", "xpathDelete: parse", err)
	}
	nodes, err := xmlquery.QueryAll(doc, expr)
	if err != nil {
		return nil, 0, logWrappedErrorContext(ctx, log,
			"opnsensesvc: XPathDelete query failed", "xpathDelete: query "+expr, err,
			slog.String("expression", expr))
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
	case xmlquery.DocumentNode,
		xmlquery.DeclarationNode,
		xmlquery.ElementNode,
		xmlquery.CharDataNode,
		xmlquery.CommentNode,
		xmlquery.NotationNode,
		xmlquery.ProcessingInstruction:
		// ElementNode and friends: emit XML so the caller can see
		// children.
		var b strings.Builder
		b.WriteString(n.OutputXML(true))
		return b.String()
	}
	return ""
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
