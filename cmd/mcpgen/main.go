// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// mcpgen generates SchemaProvider implementations for Go types.
//
// This enables zero-reflection schema generation at runtime by pre-computing
// JSON schemas at build time.
//
// Usage:
//
//	//go:generate mcpgen -type=CreateIssueInput,UpdateIssueInput
//
// This will generate a file with SchemaProvider implementations for the
// specified types, allowing the MCP SDK to use pre-computed schemas instead
// of using reflection at runtime.
//
// Flags:
//
//	-type    Comma-separated list of type names to generate schemas for
//	-output  Output file name (default: <input>_mcp_gen.go)
//	-package Package name for generated file (default: same as input)
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/token"
	"go/types"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"text/template"

	"golang.org/x/tools/go/packages"
)

var (
	typeNames  = flag.String("type", "", "comma-separated list of type names")
	outputFile = flag.String("output", "", "output file name (default: <package>_mcp_gen.go)")
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("mcpgen: ")
	flag.Parse()

	if *typeNames == "" {
		log.Fatal("no types specified; use -type flag")
	}

	types := strings.Split(*typeNames, ",")
	for i, t := range types {
		types[i] = strings.TrimSpace(t)
	}

	// Get the directory to process
	dir := "."
	if args := flag.Args(); len(args) > 0 {
		dir = args[0]
	}

	g := &Generator{
		dir:   dir,
		types: types,
	}

	if err := g.Run(); err != nil {
		log.Fatal(err)
	}
}

// Generator generates SchemaProvider implementations.
type Generator struct {
	dir   string
	types []string

	pkg     *packages.Package
	fset    *token.FileSet
	typeMap map[string]*TypeInfo
}

// TypeInfo holds information about a type to generate a schema for.
type TypeInfo struct {
	Name   string
	Fields []FieldInfo
}

// FieldInfo holds information about a struct field.
type FieldInfo struct {
	Name        string
	JSONName    string
	Type        string
	Description string
	Required    bool
	HasDefault  bool
	Default     string
	Enum        []string
}

// Run generates the schema implementations.
func (g *Generator) Run() error {
	// Load the package
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedTypes | packages.NeedSyntax |
			packages.NeedTypesInfo | packages.NeedFiles,
		Dir: g.dir,
	}

	pkgs, err := packages.Load(cfg, ".")
	if err != nil {
		return fmt.Errorf("loading package: %w", err)
	}

	if len(pkgs) == 0 {
		return fmt.Errorf("no packages found in %s", g.dir)
	}

	g.pkg = pkgs[0]
	if len(g.pkg.Errors) > 0 {
		return fmt.Errorf("package errors: %v", g.pkg.Errors)
	}

	g.fset = g.pkg.Fset
	g.typeMap = make(map[string]*TypeInfo)

	// Find the types we need to generate
	for _, typeName := range g.types {
		info, err := g.findType(typeName)
		if err != nil {
			return fmt.Errorf("finding type %s: %w", typeName, err)
		}
		g.typeMap[typeName] = info
	}

	// Generate the output
	return g.generate()
}

// findType finds a type by name and extracts its field information.
func (g *Generator) findType(name string) (*TypeInfo, error) {
	obj := g.pkg.Types.Scope().Lookup(name)
	if obj == nil {
		return nil, fmt.Errorf("type %s not found in package %s", name, g.pkg.Name)
	}

	typeObj, ok := obj.(*types.TypeName)
	if !ok {
		return nil, fmt.Errorf("%s is not a type", name)
	}

	named, ok := typeObj.Type().(*types.Named)
	if !ok {
		return nil, fmt.Errorf("%s is not a named type", name)
	}

	underlying := named.Underlying()
	structType, ok := underlying.(*types.Struct)
	if !ok {
		return nil, fmt.Errorf("%s is not a struct type", name)
	}

	info := &TypeInfo{Name: name}

	// Extract field information from the struct
	for i := 0; i < structType.NumFields(); i++ {
		field := structType.Field(i)
		tag := structType.Tag(i)

		fieldInfo := g.extractFieldInfo(field, tag)
		if fieldInfo != nil {
			info.Fields = append(info.Fields, *fieldInfo)
		}
	}

	return info, nil
}

// extractFieldInfo extracts schema-relevant information from a struct field.
func (g *Generator) extractFieldInfo(field *types.Var, tag string) *FieldInfo {
	if !field.Exported() {
		return nil
	}

	info := &FieldInfo{
		Name: field.Name(),
	}

	// Parse json tag
	info.JSONName = field.Name()
	if jsonTag := getTagValue(tag, "json"); jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		if parts[0] != "-" {
			if parts[0] != "" {
				info.JSONName = parts[0]
			}
		} else {
			return nil // Field is ignored
		}
	}

	// Parse jsonschema tag for additional metadata
	if schemaTag := getTagValue(tag, "jsonschema"); schemaTag != "" {
		parts := strings.Split(schemaTag, ",")
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "required" {
				info.Required = true
			} else if strings.HasPrefix(part, "description=") {
				info.Description = strings.TrimPrefix(part, "description=")
			} else if strings.HasPrefix(part, "default=") {
				info.HasDefault = true
				info.Default = strings.TrimPrefix(part, "default=")
			} else if strings.HasPrefix(part, "enum=") {
				enumStr := strings.TrimPrefix(part, "enum=")
				info.Enum = strings.Split(enumStr, "|")
			}
		}
	}

	// Determine JSON schema type from Go type
	info.Type = g.goTypeToJSONSchemaType(field.Type())

	// Auto-detect enum values from const declarations if field type is a named string type
	// and no explicit enum was specified in the tag
	if len(info.Enum) == 0 && info.Type == "string" {
		if named, ok := field.Type().(*types.Named); ok {
			if enumValues := g.findEnumValues(named); len(enumValues) > 0 {
				info.Enum = enumValues
			}
		}
	}

	return info
}

// findEnumValues finds const values defined for a named string type.
// This allows automatic enum detection from Go const declarations like:
//
//	type Priority string
//	const (
//	    PriorityHigh   Priority = "high"
//	    PriorityMedium Priority = "medium"
//	    PriorityLow    Priority = "low"
//	)
func (g *Generator) findEnumValues(named *types.Named) []string {
	// Only handle string-based types
	basic, ok := named.Underlying().(*types.Basic)
	if !ok || basic.Kind() != types.String {
		return nil
	}

	typeName := named.Obj().Name()
	typePkg := named.Obj().Pkg()

	var values []string

	// Look through all const declarations in the package
	scope := typePkg.Scope()
	for _, name := range scope.Names() {
		obj := scope.Lookup(name)
		constObj, ok := obj.(*types.Const)
		if !ok {
			continue
		}

		// Check if this const is of our named type
		if types.Identical(constObj.Type(), named) {
			// Extract the string value from the const
			val := constObj.Val()
			if val.Kind() == constant.String {
				// Get the string value without quotes
				strVal := constant.StringVal(val)
				values = append(values, strVal)
			}
		}
	}

	// Sort for deterministic output
	sort.Strings(values)

	// Only return if we found at least 2 values (single value isn't really an enum)
	if len(values) >= 2 {
		log.Printf("found enum type %s with values: %v", typeName, values)
		return values
	}

	return nil
}

// goTypeToJSONSchemaType converts a Go type to a JSON schema type string.
func (g *Generator) goTypeToJSONSchemaType(t types.Type) string {
	switch t := t.Underlying().(type) {
	case *types.Basic:
		switch t.Kind() {
		case types.Bool:
			return "boolean"
		case types.Int, types.Int8, types.Int16, types.Int32, types.Int64,
			types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64:
			return "integer"
		case types.Float32, types.Float64:
			return "number"
		case types.String:
			return "string"
		}
	case *types.Slice:
		return "array"
	case *types.Map:
		return "object"
	case *types.Struct:
		return "object"
	case *types.Pointer:
		return g.goTypeToJSONSchemaType(t.Elem())
	}
	return "object"
}

// getTagValue extracts a value from a struct tag using Go's reflect.StructTag.
func getTagValue(tag, key string) string {
	// Use reflect.StructTag to properly parse the tag
	st := reflect.StructTag(tag)
	return st.Get(key)
}

// generate creates the output file with SchemaProvider implementations.
func (g *Generator) generate() error {
	var buf bytes.Buffer

	// Check if any type has fields with defaults
	hasDefaults := false
	for _, name := range g.types {
		info := g.typeMap[name]
		for _, f := range info.Fields {
			if f.HasDefault {
				hasDefaults = true
				break
			}
		}
		if hasDefaults {
			break
		}
	}

	data := struct {
		Package     string
		Types       []*TypeInfo
		HasDefaults bool
	}{
		Package:     g.pkg.Name,
		HasDefaults: hasDefaults,
	}

	for _, name := range g.types {
		data.Types = append(data.Types, g.typeMap[name])
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("executing template: %w", err)
	}

	// Format the generated code
	formatted, err := format.Source(buf.Bytes())
	if err != nil {
		// Write unformatted for debugging
		log.Printf("warning: could not format generated code: %v", err)
		formatted = buf.Bytes()
	}

	// Determine output filename
	output := *outputFile
	if output == "" {
		output = filepath.Join(g.dir, g.pkg.Name+"_mcp_gen.go")
	}

	if err := os.WriteFile(output, formatted, 0644); err != nil {
		return fmt.Errorf("writing output: %w", err)
	}

	log.Printf("generated %s", output)
	return nil
}

var tmpl = template.Must(template.New("mcp_gen").Funcs(template.FuncMap{
	"quote": func(s string) string {
		return fmt.Sprintf("%q", s)
	},
	"lower": strings.ToLower,
	"formatDefault": func(f FieldInfo) string {
		// Default is json.RawMessage, so we need to output valid JSON bytes.
		// The value is already the raw string from the jsonschema tag.
		switch f.Type {
		case "string":
			// JSON strings need to be quoted
			return fmt.Sprintf("json.RawMessage(%q)", fmt.Sprintf("%q", f.Default))
		case "boolean", "integer", "number":
			// These are valid JSON literals as-is
			return fmt.Sprintf("json.RawMessage(%q)", f.Default)
		default:
			// For arrays and objects, assume it's already valid JSON
			return fmt.Sprintf("json.RawMessage(%q)", f.Default)
		}
	},
}).Parse(`// Code generated by mcpgen. DO NOT EDIT.

package {{.Package}}

import (
{{- if .HasDefaults}}
	"encoding/json"
{{end}}
	"github.com/google/jsonschema-go/jsonschema"
)

{{range .Types}}
// {{.Name}} schema variables (generated)
var (
	_{{lower .Name}}Schema = &jsonschema.Schema{
		Type: "object",
		Properties: map[string]*jsonschema.Schema{
			{{- range .Fields}}
			{{quote .JSONName}}: {
				Type: {{quote .Type}},
				{{- if .Description}}
				Description: {{quote .Description}},
				{{- end}}
				{{- if .HasDefault}}
				Default: {{formatDefault .}},
				{{- end}}
				{{- if .Enum}}
				Enum: []any{ {{range $i, $e := .Enum}}{{if $i}}, {{end}}{{quote $e}}{{end}} },
				{{- end}}
			},
			{{- end}}
		},
		Required: []string{
			{{- range .Fields}}{{if .Required}}
			{{quote .JSONName}},
			{{- end}}{{end}}
		},
		AdditionalProperties: &jsonschema.Schema{Not: &jsonschema.Schema{}},
	}
	_{{lower .Name}}Resolved, _ = _{{lower .Name}}Schema.Resolve(nil)
)

// MCPSchema returns the pre-computed JSON schema for {{.Name}}.
// This implements mcp.SchemaProvider.
func ({{.Name}}) MCPSchema() *jsonschema.Schema {
	return _{{lower .Name}}Schema
}

// MCPResolvedSchema returns the pre-resolved JSON schema for {{.Name}}.
// This implements mcp.ResolvedSchemaProvider.
func ({{.Name}}) MCPResolvedSchema() *jsonschema.Resolved {
	return _{{lower .Name}}Resolved
}
{{end}}
`))

// findASTType finds the AST node for a type declaration.
// This can be used for more detailed analysis if needed.
func (g *Generator) findASTType(name string) *ast.TypeSpec {
	for _, file := range g.pkg.Syntax {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.TYPE {
				continue
			}
			for _, spec := range genDecl.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if typeSpec.Name.Name == name {
					return typeSpec
				}
			}
		}
	}
	return nil
}
