// Package mcp implements the Model Context Protocol (MCP) server for Nexus.
// MCP is a JSON-RPC 2.0 protocol that lets AI agents use tools exposed by servers.
package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"sync"
)

// Server implements the Model Context Protocol for Nexus.
type Server struct {
	tools  map[string]Tool
	mu     sync.RWMutex
	logger *slog.Logger
}

// Tool describes an MCP tool with its schema and handler.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
	Handler     func(params map[string]interface{}) (interface{}, error) `json:"-"`
}

// JSON-RPC 2.0 message types.

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

// RPCError represents a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Standard JSON-RPC 2.0 error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
	CodeInternalError  = -32603
)

// NewServer creates a new MCP server.
func NewServer(logger *slog.Logger) *Server {
	return &Server{
		tools:  make(map[string]Tool),
		logger: logger,
	}
}

// RegisterTool adds a tool to the MCP server.
func (s *Server) RegisterTool(t Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[t.Name] = t
}

// HandleHTTP handles JSON-RPC 2.0 requests over HTTP.
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeResponse(w, Response{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &RPCError{Code: CodeInvalidRequest, Message: "POST required"},
		})
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeResponse(w, Response{
			JSONRPC: "2.0",
			ID:      nil,
			Error:   &RPCError{Code: CodeParseError, Message: "invalid JSON"},
		})
		return
	}

	if req.JSONRPC != "2.0" {
		writeResponse(w, Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidRequest, Message: "jsonrpc must be \"2.0\""},
		})
		return
	}

	resp := s.dispatch(req)
	writeResponse(w, resp)
}

func (s *Server) dispatch(req Request) Response {
	switch req.Method {
	case "initialize":
		return s.handleInitialize(req)
	case "tools/list":
		return s.handleToolsList(req)
	case "tools/call":
		return s.handleToolsCall(req)
	default:
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeMethodNotFound, Message: fmt.Sprintf("method not found: %s", req.Method)},
		}
	}
}

func (s *Server) handleInitialize(req Request) Response {
	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]interface{}{
				"name":    "nexus-mcp",
				"version": "0.1.0",
			},
		},
	}
}

// toolEntry is the wire format for a tool in tools/list responses.
type toolEntry struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

func (s *Server) handleToolsList(req Request) Response {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]toolEntry, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, toolEntry{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
		})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })

	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"tools": tools,
		},
	}
}

func (s *Server) handleToolsCall(req Request) Response {
	paramsMap, ok := req.Params.(map[string]interface{})
	if !ok {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: "params must be an object"},
		}
	}

	toolName, _ := paramsMap["name"].(string)
	if toolName == "" {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeInvalidParams, Message: "params.name is required"},
		}
	}

	s.mu.RLock()
	tool, exists := s.tools[toolName]
	s.mu.RUnlock()

	if !exists {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   &RPCError{Code: CodeMethodNotFound, Message: fmt.Sprintf("tool not found: %s", toolName)},
		}
	}

	args, _ := paramsMap["arguments"].(map[string]interface{})
	if args == nil {
		args = make(map[string]interface{})
	}

	result, err := tool.Handler(args)
	if err != nil {
		return Response{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result: map[string]interface{}{
				"content": []map[string]interface{}{
					{"type": "text", "text": fmt.Sprintf("error: %s", err.Error())},
				},
				"isError": true,
			},
		}
	}

	// Serialize the result to text for MCP content format.
	text, merr := json.Marshal(result)
	if merr != nil {
		text = []byte(fmt.Sprintf("%v", result))
	}

	return Response{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result: map[string]interface{}{
			"content": []map[string]interface{}{
				{"type": "text", "text": string(text)},
			},
		},
	}
}

func writeResponse(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
