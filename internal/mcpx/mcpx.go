package mcpx

import "github.com/mark3labs/mcp-go/mcp"

func StringArgument(req mcp.CallToolRequest, name string) (string, bool) {
	v, ok := req.GetArguments()[name]
	if !ok {
		return "", false
	}
	s, ok := v.(string)
	return s, ok
}

func Int64Argument(req mcp.CallToolRequest, name string) (int64, bool) {
	v, ok := req.GetArguments()[name]
	if !ok {
		return 0, false
	}
	f, ok := v.(float64)
	return int64(f), ok
}

func BoolArgument(req mcp.CallToolRequest, name string) (bool, bool) {
	v, ok := req.GetArguments()[name]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}
