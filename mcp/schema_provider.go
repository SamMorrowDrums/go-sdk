// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

package mcp

import "github.com/google/jsonschema-go/jsonschema"

// SchemaProvider can be implemented by types to provide pre-computed JSON schemas.
// When a type implements this interface, AddTool will use the provided schema
// instead of using reflection to generate one.
//
// This interface is optional - the SDK will work perfectly fine without it.
// Implement it when you need maximum performance or want to use a code generator
// like mcpgen to eliminate reflection overhead entirely.
//
// Example:
//
//	type CreateIssueInput struct {
//	    Title string `json:"title"`
//	    Body  string `json:"body"`
//	}
//
//	var createIssueInputSchema = &jsonschema.Schema{
//	    Type: "object",
//	    Properties: map[string]*jsonschema.Schema{
//	        "title": {Type: "string", Description: "Issue title"},
//	        "body":  {Type: "string", Description: "Issue body"},
//	    },
//	    Required: []string{"title"},
//	}
//
//	func (CreateIssueInput) MCPSchema() *jsonschema.Schema {
//	    return createIssueInputSchema
//	}
type SchemaProvider interface {
	// MCPSchema returns the JSON schema for this type.
	// The returned schema should be immutable and safe for concurrent use.
	// The schema will be resolved and cached automatically by the SDK.
	MCPSchema() *jsonschema.Schema
}

// ResolvedSchemaProvider extends SchemaProvider with a pre-resolved schema.
// Implementing this interface avoids the schema resolution step entirely,
// providing maximum performance for high-throughput scenarios.
//
// Types implementing ResolvedSchemaProvider must also implement SchemaProvider.
// The MCPResolvedSchema method should return a schema that was resolved from
// the same schema returned by MCPSchema.
//
// Example:
//
//	var createIssueInputSchema = &jsonschema.Schema{...}
//	var createIssueInputResolved, _ = createIssueInputSchema.Resolve(nil)
//
//	func (CreateIssueInput) MCPSchema() *jsonschema.Schema {
//	    return createIssueInputSchema
//	}
//
//	func (CreateIssueInput) MCPResolvedSchema() *jsonschema.Resolved {
//	    return createIssueInputResolved
//	}
type ResolvedSchemaProvider interface {
	SchemaProvider
	// MCPResolvedSchema returns a pre-resolved schema ready for validation.
	// The returned resolved schema should be immutable and safe for concurrent use.
	MCPResolvedSchema() *jsonschema.Resolved
}
