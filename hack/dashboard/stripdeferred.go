// SPDX-License-Identifier: CC0-1.0

package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// deferredMarker is the substring stripDeferred looks for in a disable
// entry's head comment to identify it as deferred rather than permanent.
const deferredMarker = "deferred:"

// yamlIndent matches the 2-space indentation the repository's .golangci.yml
// already uses.
const yamlIndent = 2

// Errors returned when a golangci-lint config does not have the shape
// stripDeferred expects. Any of these means the config was restructured and
// this tool needs updating, not that the linters.disable list is merely
// empty.
var (
	ErrEmptyDocument = errors.New("empty YAML document")
	ErrNotAMapping   = errors.New("expected a mapping")
	ErrKeyNotFound   = errors.New("key not found")
	ErrNotASequence  = errors.New("expected a sequence")
)

// stripDeferred removes every `linters.disable` sequence entry in a
// golangci-lint v2 config whose head comment contains "deferred:", leaving
// permanent disables untouched, and re-encodes the result with 2-space
// indentation. It fails if `linters.disable` cannot be located.
func stripDeferred(in []byte) ([]byte, error) {
	var doc yaml.Node

	err := yaml.Unmarshal(in, &doc)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	disable, err := findDisableSequence(&doc)
	if err != nil {
		return nil, err
	}

	disable.Content = filterDeferred(disable.Content)

	encoded, err := encodeNode(&doc)
	if err != nil {
		return nil, fmt.Errorf("encoding config: %w", err)
	}

	return encoded, nil
}

// filterDeferred returns items whose head comment is not marked deferred.
func filterDeferred(items []*yaml.Node) []*yaml.Node {
	kept := make([]*yaml.Node, 0, len(items))

	for _, item := range items {
		if isDeferredComment(item.HeadComment) {
			continue
		}

		kept = append(kept, item)
	}

	return kept
}

// isDeferredComment reports whether headComment marks its entry as deferred:
// one of its lines, with the leading "#" and whitespace trimmed, starts with
// "deferred:". A plain substring search would also match "deferred:" showing
// up mid-sentence in an unrelated (permanent-disable) comment.
func isDeferredComment(headComment string) bool {
	for line := range strings.SplitSeq(headComment, "\n") {
		trimmed := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if strings.HasPrefix(trimmed, deferredMarker) {
			return true
		}
	}

	return false
}

// findDisableSequence walks doc for linters.disable and returns its sequence
// node.
func findDisableSequence(doc *yaml.Node) (*yaml.Node, error) {
	if len(doc.Content) == 0 {
		return nil, ErrEmptyDocument
	}

	root := doc.Content[0]

	linters, err := mappingValue(root, "linters")
	if err != nil {
		return nil, err
	}

	disable, err := mappingValue(linters, "disable")
	if err != nil {
		return nil, err
	}

	if disable.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("linters.disable: %w", ErrNotASequence)
	}

	return disable, nil
}

// mappingValue returns the value node for key in a YAML mapping node.
func mappingValue(mapping *yaml.Node, key string) (*yaml.Node, error) {
	if mapping.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("looking for %q: %w", key, ErrNotAMapping)
	}

	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			return mapping.Content[i+1], nil
		}
	}

	return nil, fmt.Errorf("%q: %w", key, ErrKeyNotFound)
}

// encodeNode re-marshals a YAML node tree with 2-space indentation,
// preserving comments.
func encodeNode(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer

	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(yamlIndent)

	err := enc.Encode(doc)
	if err != nil {
		return nil, fmt.Errorf("encoding: %w", err)
	}

	err = enc.Close()
	if err != nil {
		return nil, fmt.Errorf("closing encoder: %w", err)
	}

	return buf.Bytes(), nil
}
