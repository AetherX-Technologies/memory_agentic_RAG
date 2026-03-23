package mcp

// toolDefinitions contains the MCP tool definitions per the 2024-11-05 spec.
var toolDefinitions = []map[string]interface{}{
	{
		"name":        "memory_store",
		"description": "存储一条新的记忆。自动去重和冲突检测。",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content":    map[string]string{"type": "string", "description": "记忆内容（一句话）"},
				"type":       map[string]interface{}{"type": "string", "enum": []string{"fact", "preference", "skill", "episode", "instruction", "relationship"}},
				"importance": map[string]interface{}{"type": "number", "minimum": 0, "maximum": 1},
				"tags":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
			},
			"required": []string{"content"},
		},
	},
	{
		"name":        "memory_recall",
		"description": "语义检索相关记忆，返回格式化上下文。",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":          map[string]string{"type": "string"},
				"limit":          map[string]interface{}{"type": "integer", "default": 10},
				"types":          map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
				"min_importance": map[string]interface{}{"type": "number", "default": 0.1},
				"max_tokens":     map[string]interface{}{"type": "integer", "default": 1000},
			},
			"required": []string{"query"},
		},
	},
	{
		"name":        "memory_forget",
		"description": "删除一条记忆（移入垃圾桶，30天内可恢复）。",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id": map[string]string{"type": "string"},
			},
			"required": []string{"id"},
		},
	},
	{
		"name":        "memory_update",
		"description": "更新记忆内容或属性。content 变更会自动重新向量化。",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"id":         map[string]string{"type": "string"},
				"content":    map[string]string{"type": "string"},
				"importance": map[string]string{"type": "number"},
				"tags":       map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
			},
			"required": []string{"id"},
		},
	},
	{
		"name":        "memory_export",
		"description": "导出全部记忆为 JSON 数组（备份/迁移用）。",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"types":           map[string]interface{}{"type": "array", "items": map[string]string{"type": "string"}},
				"include_expired": map[string]interface{}{"type": "boolean", "default": false},
			},
		},
	},
	{
		"name":        "memory_import",
		"description": "从 JSON 数组批量导入记忆（恢复备份用）。自动跳过 content_hash 冲突。",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"memories":  map[string]interface{}{"type": "array", "description": "memory_export 输出的 JSON 数组"},
				"overwrite": map[string]interface{}{"type": "boolean", "default": false},
			},
			"required": []string{"memories"},
		},
	},
	{
		"name":        "memory_forget_by_tag",
		"description": "按标签批量删除记忆（如删除所有 pii:employer 标签的记忆）。",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tag":     map[string]string{"type": "string", "description": "要删除的标签"},
				"dry_run": map[string]interface{}{"type": "boolean", "default": true},
			},
			"required": []string{"tag"},
		},
	},
	{
		"name":        "memory_consolidate",
		"description": "分析未合并的记忆，发现关联和模式，生成跨记忆洞察。需要至少2条未合并记忆且配置LLM。",
		"inputSchema": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	},
	{
		"name":        "memory_should_capture",
		"description": "判断一段文本是否包含值得记忆的内容（触发词检测）。用于在存储前预检，避免存储噪音。",
		"inputSchema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"text": map[string]string{"type": "string", "description": "要检测的文本"},
			},
			"required": []string{"text"},
		},
	},
}
