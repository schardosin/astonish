package common

import (
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ToolWithDeclaration allows inspecting a tool's schema.
// Matches ADK's internal toolinternal.FunctionTool.Declaration() method.
type ToolWithDeclaration interface {
	Declaration() *genai.FunctionDeclaration
}

// RunnableTool defines an interface for tools that can be executed.
// This matches the signature of Run method in adk-go's internal tool implementations.
type RunnableTool interface {
	Run(ctx tool.Context, args any) (map[string]any, error)
}
