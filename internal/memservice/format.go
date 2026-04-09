package memservice

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/yourusername/hybridmem-rag/internal/store"
	"github.com/yourusername/hybridmem-rag/internal/tokutil"
)

var typeEmoji = map[string]string{
	"instruction":  "📌",
	"preference":   "💡",
	"fact":         "👤",
	"skill":        "🔧",
	"relationship": "🔗",
	"episode":      "📅",
}

var typeLabel = map[string]string{
	"instruction":  "指令",
	"preference":   "偏好",
	"fact":         "用户信息",
	"skill":        "技能",
	"relationship": "关系",
	"episode":      "事件",
}

func formatContext(results []store.SearchResult, maxTokens int) string {
	if len(results) == 0 {
		empty := "[记忆系统无相关记忆]"
		if estimateTokens(empty) > maxTokens {
			return ""
		}
		return empty
	}

	groups := make(map[string][]store.SearchResult)
	for _, r := range results {
		t := r.Entry.MemoryType
		if t == "" {
			t = "fact"
		}
		groups[t] = append(groups[t], r)
	}

	for t := range groups {
		sort.Slice(groups[t], func(i, j int) bool {
			scoreI := groups[t][i].Entry.Confidence * groups[t][i].Entry.Importance
			scoreJ := groups[t][j].Entry.Confidence * groups[t][j].Entry.Importance
			return scoreI > scoreJ
		})
	}

	// Reserve header budget based on actual input count
	headerTemplate := fmt.Sprintf("[记忆系统召回 %d 条]\n", len(results))
	headerReserve := estimateTokens(headerTemplate)
	if headerReserve > maxTokens {
		return ""
	}

	var sb strings.Builder
	tokenCount := headerReserve
	emittedCount := 0
	typeOrder := []string{"instruction", "preference", "fact", "skill", "relationship", "episode"}

	for t := range groups {
		found := false
		for _, std := range typeOrder {
			if t == std {
				found = true
				break
			}
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

		typeHeader := fmt.Sprintf("\n%s %s\n", emoji, label)
		typeHeaderTokens := estimateTokens(typeHeader)
		if tokenCount+typeHeaderTokens > maxTokens {
			continue // skip this section header, try shorter ones
		}

		// Check if at least one item fits before committing the header
		anyFits := false
		for _, item := range items {
			line := formatMemoryLine(item, t)
			if tokenCount+typeHeaderTokens+estimateTokens(line) <= maxTokens {
				anyFits = true
				break
			}
		}
		if !anyFits {
			continue // skip this type entirely to avoid orphaned headers
		}

		sb.WriteString(typeHeader)
		tokenCount += typeHeaderTokens

		for _, item := range items {
			line := formatMemoryLine(item, t)
			lineTokens := estimateTokens(line)
			if tokenCount+lineTokens > maxTokens {
				continue // skip this item but try shorter ones
			}
			sb.WriteString(line)
			tokenCount += lineTokens
			emittedCount++
		}
	}

	header := fmt.Sprintf("[记忆系统召回 %d 条]\n", emittedCount)
	return header + sb.String()
}

func formatMemoryLine(result store.SearchResult, memType string) string {
	text := strings.ReplaceAll(result.Entry.Text, "\n", " ")
	suffix := ""
	if result.Entry.SourceConv != "" {
		suffix = fmt.Sprintf(" [conv:%s]", result.Entry.SourceConv)
	}
	if memType == "episode" && result.Entry.Timestamp > 0 {
		date := time.Unix(result.Entry.Timestamp, 0).Format("2006-01-02")
		return fmt.Sprintf("- [%s] %s%s\n", date, text, suffix)
	}
	return fmt.Sprintf("- %s%s\n", text, suffix)
}

func estimateTokens(s string) int {
	return tokutil.EstimateTokens(s)
}
