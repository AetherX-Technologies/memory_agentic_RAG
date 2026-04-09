package memservice

var singleToolDefinitions = []map[string]interface{}{
	{
		"name":        "memory_recall",
		"description": "Search user's long-term memory. Auto-stores only when the user explicitly asks to remember (e.g. '记住.../remember...'). Use this tool when you need context about the user, or when the user asks you to remember something.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":       map[string]string{"type": "string", "description": "The user's message or search query"},
				"limit":       map[string]interface{}{"type": "integer", "default": 10},
				"max_tokens":  map[string]interface{}{"type": "integer", "description": "Max tokens for the returned context (default: 1000). Use this to fit results within your available context budget."},
				"source_conv": map[string]string{"type": "string", "description": "Filter by conversation ID (only return memories from this conversation)"},
			},
			"required": []string{"query"},
		},
	},
}

var allToolDefinitions = []map[string]interface{}{
	{
		"name":        "memory_store",
		"description": "Store a new memory. Auto dedup by content hash.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]string{"type": "string", "description": "The memory content to store"},
				"type":    map[string]interface{}{"type": "string", "enum": []string{"fact", "preference", "skill", "episode", "instruction", "relationship"}},
				"tags":    map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Optional tags for categorization (e.g. [\"Go\", \"backend\"])"},
			},
			"required": []string{"content"},
		},
	},
	{
		"name":        "memory_recall",
		"description": "Search user's long-term memory. Auto-stores only on explicit '记住/remember' requests.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":       map[string]string{"type": "string", "description": "Search query text"},
				"limit":       map[string]interface{}{"type": "integer", "default": 10},
				"max_tokens":  map[string]interface{}{"type": "integer", "description": "Max tokens for the returned context (default: 1000). Use this to fit results within your available context budget."},
				"source_conv": map[string]string{"type": "string", "description": "Filter by conversation ID (only return memories from this conversation)"},
			},
			"required": []string{"query"},
		},
	},
	{
		"name":        "memory_forget",
		"description": "Delete a memory by ID (soft delete, recoverable within 30 days).",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]string{"type": "string", "description": "Memory ID to delete"},
			},
			"required": []string{"id"},
		},
	},
	{
		"name":        "memory_update",
		"description": "Update a memory's content or importance.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":         map[string]string{"type": "string", "description": "Memory ID"},
				"content":    map[string]string{"type": "string", "description": "New content"},
				"importance": map[string]string{"type": "number", "description": "Importance 0-1"},
				"tags":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}, "description": "Replace tags (omit to keep existing)"},
			},
			"required": []string{"id"},
		},
	},
	{
		"name":        "memory_export",
		"description": "Export all memories as JSON array for backup.",
		"inputSchema": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	},
	{
		"name":        "memory_import",
		"description": "Import memories from JSON array (restore backup).",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"memories": map[string]interface{}{"type": "array", "description": "Array of memory objects from memory_export"},
			},
			"required": []string{"memories"},
		},
	},
	{
		"name":        "memory_forget_by_tag",
		"description": "Batch delete memories by tag.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tag":     map[string]string{"type": "string", "description": "Tag to match"},
				"dry_run": map[string]interface{}{"type": "boolean", "default": true},
			},
			"required": []string{"tag"},
		},
	},
	{
		"name":        "memory_consolidate",
		"description": "Analyze memories to find patterns and generate insights. Requires LLM config.",
		"inputSchema": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
			"required":   []string{},
		},
	},
	{
		"name":        "memory_should_capture",
		"description": "Check if text contains memory-worthy content (trigger word detection).",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]string{"type": "string", "description": "Text to check"},
			},
			"required": []string{"text"},
		},
	},
}

var knownToolNames = map[string]struct{}{
	"memory_store":          {},
	"memory_recall":         {},
	"memory_forget":         {},
	"memory_update":         {},
	"memory_export":         {},
	"memory_import":         {},
	"memory_forget_by_tag":  {},
	"memory_consolidate":    {},
	"memory_should_capture": {},
}

func VisibleToolDefinitions(singleTool bool) []map[string]interface{} {
	if singleTool {
		return singleToolDefinitions
	}
	return allToolDefinitions
}

func IsKnownTool(name string) bool {
	_, ok := knownToolNames[name]
	return ok
}

func IsVisibleTool(name string, singleTool bool) bool {
	if !IsKnownTool(name) {
		return false
	}
	if !singleTool {
		return true
	}
	return name == "memory_recall"
}
