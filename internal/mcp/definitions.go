package mcp

// toolDefinitions — only expose memory_recall to the LLM.
// memory_store is handled automatically inside recall (via ShouldCapture).
// Other tools (store, forget, update, export, import, etc.) remain functional
// but are not listed to avoid triggering tool_call SSE issues with some LLM APIs.
//
// When env MEMORY_EXPOSE_ALL_TOOLS=1 is set, all 9 tools are exposed.
var toolDefinitions = []map[string]interface{}{
	{
		"name": "memory_recall",
		"description": "Search user's long-term memory. Also auto-stores new memories when the query contains personal info (e.g. 'remember I have a cat named Nietzsche'). Use this tool whenever the user asks you to remember something, or when you need context about the user.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "The user's message or search query"},
			},
			"required": []string{"query"},
		},
	},
}

// allToolDefinitions contains all 9 tools for advanced/compatible setups.
var allToolDefinitions = []map[string]interface{}{
	{
		"name":        "memory_store",
		"description": "Store a new memory. Auto dedup by content hash.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]string{"type": "string", "description": "The memory content to store"},
				"type":    map[string]interface{}{"type": "string", "enum": []string{"fact", "preference", "skill", "episode", "instruction", "relationship"}},
			},
			"required": []string{"content"},
		},
	},
	{
		"name": "memory_recall",
		"description": "Search user's long-term memory. Also auto-stores new memories when the query contains personal info.",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]string{"type": "string", "description": "Search query text"},
				"limit": map[string]interface{}{"type": "integer", "default": 10},
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
