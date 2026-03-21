package mcp

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/yourusername/hybridmem-rag/internal/store"
)

// typePriority defines the display order for memory types.
var typePriority = map[string]int{
	"instruction":  0,
	"preference":   1,
	"fact":         2,
	"skill":        3,
	"relationship": 4,
	"episode":      5,
}

// typeEmoji maps memory types to display emojis.
var typeEmoji = map[string]string{
	"instruction":  "📌",
	"preference":   "💡",
	"fact":         "👤",
	"skill":        "🔧",
	"relationship": "🔗",
	"episode":      "📅",
}

// typeLabel maps memory types to display labels.
var typeLabel = map[string]string{
	"instruction":  "指令",
	"preference":   "偏好",
	"fact":         "用户信息",
	"skill":        "技能",
	"relationship": "关系",
	"episode":      "事件",
}

// formatContext formats search results into a human-readable context string.
// Groups by type, sorts by confidence*importance, respects token budget.
func formatContext(results []store.SearchResult, maxTokens int) string {
	if len(results) == 0 {
		return "[记忆系统无相关记忆]"
	}

	// Group by type
	groups := make(map[string][]store.SearchResult)
	for _, r := range results {
		t := r.Entry.MemoryType
		if t == "" {
			t = "fact"
		}
		groups[t] = append(groups[t], r)
	}

	// Sort within each group by confidence * importance
	for t := range groups {
		sort.Slice(groups[t], func(i, j int) bool {
			scoreI := groups[t][i].Entry.Confidence * groups[t][i].Entry.Importance
			scoreJ := groups[t][j].Entry.Confidence * groups[t][j].Entry.Importance
			return scoreI > scoreJ
		})
	}

	// Build output in priority order
	var sb strings.Builder
	header := fmt.Sprintf("[记忆系统召回 %d 条]\n", len(results))
	sb.WriteString(header)

	tokenCount := estimateTokens(header)
	typeOrder := []string{"instruction", "preference", "fact", "skill", "relationship", "episode"}

	// Collect any types not in the standard order
	for t := range groups {
		found := false
		for _, std := range typeOrder {
			if t == std { found = true; break }
		}
		if !found {
			typeOrder = append(typeOrder, t)
		}
	}

	for _, t := range typeOrder {
		items, ok := groups[t]
		if !ok || len(items) == 0 {
			continue
		}

		emoji := typeEmoji[t]
		label := typeLabel[t]
		if emoji == "" {
			emoji = "📝"
		}
		if label == "" {
			label = t
		}

		header := fmt.Sprintf("\n%s %s\n", emoji, label)
		headerTokens := estimateTokens(header)
		if tokenCount+headerTokens > maxTokens {
			break // token budget exceeded
		}
		sb.WriteString(header)
		tokenCount += headerTokens

		for _, item := range items {
			line := formatMemoryLine(item, t)
			lineTokens := estimateTokens(line)
			if tokenCount+lineTokens > maxTokens {
				break
			}
			sb.WriteString(line)
			tokenCount += lineTokens
		}
	}

	return sb.String()
}

// formatMemoryLine formats a single memory for display.
func formatMemoryLine(r store.SearchResult, memType string) string {
	text := r.Entry.Text
	if memType == "episode" && r.Entry.Timestamp > 0 {
		date := time.Unix(r.Entry.Timestamp, 0).Format("2006-01-02")
		return fmt.Sprintf("- [%s] %s\n", date, text)
	}
	return fmt.Sprintf("- %s\n", text)
}

// estimateTokens gives a rough token estimate for display purposes.
func estimateTokens(s string) int {
	runes := utf8.RuneCountInString(s)
	return runes * 2 / 3 // rough: Chinese ~1.5 chars/token
}
