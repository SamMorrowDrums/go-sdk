// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
)

// TestSchemaProvider_Input implements SchemaProvider for testing
type TestSchemaProvider_Input struct {
	Query string `json:"query"`
	Limit int    `json:"limit"`
}

var testSchemaProviderSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"query": {Type: "string", Description: "Search query"},
		"limit": {Type: "integer", Description: "Max results"},
	},
	Required: []string{"query"},
}

func (TestSchemaProvider_Input) MCPSchema() *jsonschema.Schema {
	return testSchemaProviderSchema
}

// Verify interface is implemented
var _ SchemaProvider = TestSchemaProvider_Input{}

// TestResolvedSchemaProvider_Input implements ResolvedSchemaProvider for testing
type TestResolvedSchemaProvider_Input struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

var testResolvedSchemaProviderSchema = &jsonschema.Schema{
	Type: "object",
	Properties: map[string]*jsonschema.Schema{
		"name":  {Type: "string", Description: "Item name"},
		"value": {Type: "integer", Description: "Item value"},
	},
	Required: []string{"name"},
}

var testResolvedSchemaProviderResolved, _ = testResolvedSchemaProviderSchema.Resolve(nil)

func (TestResolvedSchemaProvider_Input) MCPSchema() *jsonschema.Schema {
	return testResolvedSchemaProviderSchema
}

func (TestResolvedSchemaProvider_Input) MCPResolvedSchema() *jsonschema.Resolved {
	return testResolvedSchemaProviderResolved
}

// Verify interface is implemented
var _ ResolvedSchemaProvider = TestResolvedSchemaProvider_Input{}

type TestSchemaProvider_Output struct {
	Success bool `json:"success"`
}

func TestSetSchema_UsesSchemaProvider(t *testing.T) {
	globalSchemaCache.resetForTesting()

	var sfield any
	var rfield *jsonschema.Resolved

	_, err := setSchema[TestSchemaProvider_Input](&sfield, &rfield)
	if err != nil {
		t.Fatalf("setSchema failed: %v", err)
	}

	// Verify the schema from SchemaProvider was used
	gotSchema, ok := sfield.(*jsonschema.Schema)
	if !ok {
		t.Fatal("expected *jsonschema.Schema")
	}

	// Check that it's the same schema we provided
	if gotSchema != testSchemaProviderSchema {
		t.Error("expected schema from SchemaProvider to be used")
	}

	// Verify it was cached
	rt := reflect.TypeFor[TestSchemaProvider_Input]()
	_, _, cached := globalSchemaCache.getByType(rt)
	if !cached {
		t.Error("expected schema to be cached after SchemaProvider usage")
	}
}

func TestSetSchema_UsesResolvedSchemaProvider(t *testing.T) {
	globalSchemaCache.resetForTesting()

	var sfield any
	var rfield *jsonschema.Resolved

	_, err := setSchema[TestResolvedSchemaProvider_Input](&sfield, &rfield)
	if err != nil {
		t.Fatalf("setSchema failed: %v", err)
	}

	// Verify the schema from ResolvedSchemaProvider was used
	gotSchema, ok := sfield.(*jsonschema.Schema)
	if !ok {
		t.Fatal("expected *jsonschema.Schema")
	}

	if gotSchema != testResolvedSchemaProviderSchema {
		t.Error("expected schema from ResolvedSchemaProvider to be used")
	}

	// Verify the pre-resolved schema was used directly
	if rfield != testResolvedSchemaProviderResolved {
		t.Error("expected pre-resolved schema from ResolvedSchemaProvider to be used")
	}

	// Verify it was cached
	rt := reflect.TypeFor[TestResolvedSchemaProvider_Input]()
	_, cachedResolved, cached := globalSchemaCache.getByType(rt)
	if !cached {
		t.Error("expected schema to be cached after ResolvedSchemaProvider usage")
	}
	if cachedResolved != testResolvedSchemaProviderResolved {
		t.Error("expected cached resolved schema to be the pre-resolved one")
	}
}

func TestAddTool_WithSchemaProvider(t *testing.T) {
	globalSchemaCache.resetForTesting()

	handler := func(ctx context.Context, req *CallToolRequest, in TestSchemaProvider_Input) (*CallToolResult, TestSchemaProvider_Output, error) {
		return &CallToolResult{}, TestSchemaProvider_Output{Success: true}, nil
	}

	tool := &Tool{
		Name:        "search",
		Description: "Search for items",
	}

	s := NewServer(&Implementation{Name: "test", Version: "1.0"}, nil)
	AddTool(s, tool, handler)

	// Verify the schema was cached using the SchemaProvider's schema
	rt := reflect.TypeFor[TestSchemaProvider_Input]()
	cachedSchema, _, cached := globalSchemaCache.getByType(rt)
	if !cached {
		t.Fatal("expected schema to be cached")
	}
	if cachedSchema != testSchemaProviderSchema {
		t.Error("expected cached schema to be from SchemaProvider")
	}
}

func TestAddTool_WithResolvedSchemaProvider(t *testing.T) {
	globalSchemaCache.resetForTesting()

	handler := func(ctx context.Context, req *CallToolRequest, in TestResolvedSchemaProvider_Input) (*CallToolResult, TestSchemaProvider_Output, error) {
		return &CallToolResult{}, TestSchemaProvider_Output{Success: true}, nil
	}

	tool := &Tool{
		Name:        "create",
		Description: "Create an item",
	}

	s := NewServer(&Implementation{Name: "test", Version: "1.0"}, nil)
	AddTool(s, tool, handler)

	// Verify the pre-resolved schema was cached
	rt := reflect.TypeFor[TestResolvedSchemaProvider_Input]()
	cachedSchema, cachedResolved, cached := globalSchemaCache.getByType(rt)
	if !cached {
		t.Fatal("expected schema to be cached")
	}
	if cachedSchema != testResolvedSchemaProviderSchema {
		t.Error("expected cached schema to be from ResolvedSchemaProvider")
	}
	if cachedResolved != testResolvedSchemaProviderResolved {
		t.Error("expected cached resolved schema to be the pre-resolved one")
	}
}
