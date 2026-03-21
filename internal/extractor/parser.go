package extractor

import (
	"encoding/json"
	"strings"
)

// validTypes is the set of allowed memory types.
var validTypes = map[string]bool{
	"fact":         true,
	"preference":   true,
	"skill":        true,
	"episode":      true,
	"instruction":  true,
	"relationship": true,
}

// parseExtraction parses the LLM's raw text output into RawMemory slices.
// It handles common LLM output quirks: markdown code blocks, extra text around JSON.
func parseExtraction(raw string) ([]RawMemory, error) {
	cleaned := stripMarkdown(raw)

	// Reject null/blank before attempting JSON parse
	trimmed := strings.TrimSpace(cleaned)
	if trimmed == "null" || trimmed == "" {
		return nil, &ParseError{Raw: raw} // trigger fallback
	}

	// Attempt 1: direct unmarshal
	var memories []RawMemory
	if err := json.Unmarshal([]byte(cleaned), &memories); err == nil && memories != nil {
		return memories, nil
	}

	// Attempt 2: extract [...] substrings and try each until one unmarshals with content
	foundEmpty := false
	remaining := cleaned
	for {
		extracted := extractJSONArray(remaining)
		if extracted == "" {
			break
		}
		if err := json.Unmarshal([]byte(extracted), &memories); err == nil {
			if len(memories) > 0 {
				return memories, nil
			}
			foundEmpty = true // remember we saw a valid empty array
		}
		// Skip past this [...] and try the next one
		idx := strings.Index(remaining, extracted)
		if idx < 0 {
			break
		}
		remaining = remaining[idx+len(extracted):]
	}
	// If we found at least one valid empty array and no non-empty one, treat as "no memories"
	if foundEmpty {
		return nil, nil
	}

	// Attempt 3: maybe it's a single object, not array
	var single RawMemory
	if err := json.Unmarshal([]byte(cleaned), &single); err == nil && single.Content != "" {
		return []RawMemory{single}, nil
	}

	// Attempt 4: extract first {...} from prose (single object wrapped in text)
	objStr := extractJSONObject(cleaned)
	if objStr != "" {
		if err := json.Unmarshal([]byte(objStr), &single); err == nil && single.Content != "" {
			return []RawMemory{single}, nil
		}
	}

	// Check if the LLM returned empty array text
	if trimmed == "[]" {
		return nil, nil // explicit empty — no memories to extract
	}

	return nil, &ParseError{Raw: raw}
}

// ParseError indicates the LLM output could not be parsed.
type ParseError struct {
	Raw string
}

func (e *ParseError) Error() string {
	return "failed to parse LLM extraction output"
}

// stripMarkdown removes markdown code block markers from the input.
func stripMarkdown(s string) string {
	s = strings.TrimSpace(s)
	// Remove ```json ... ``` or ``` ... ```
	if strings.HasPrefix(s, "```") {
		// Find end of first line
		idx := strings.Index(s, "\n")
		if idx > 0 {
			s = s[idx+1:]
		}
		// Remove trailing ```
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// extractJSONObject extracts the first valid JSON object substring by brace matching.
func extractJSONObject(s string) string {
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inString {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// extractJSONArray extracts the first valid JSON array substring by bracket matching.
func extractJSONArray(s string) string {
	start := strings.IndexByte(s, '[')
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if c == '\\' && inString {
			escape = true
			continue
		}
		if c == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		if c == '[' {
			depth++
		} else if c == ']' {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// validateRaw validates and normalizes a RawMemory.
// Returns the validated memory and true if valid, or zero value and false if invalid.
func validateRaw(raw RawMemory) (RawMemory, bool) {
	// Content is required
	raw.Content = strings.TrimSpace(raw.Content)
	if raw.Content == "" {
		return RawMemory{}, false
	}

	// Validate type
	raw.Type = strings.ToLower(strings.TrimSpace(raw.Type))
	if !validTypes[raw.Type] {
		raw.Type = "fact" // default
	}

	// Clamp importance to [0, 1]
	if raw.Importance < 0 {
		raw.Importance = 0
	}
	if raw.Importance > 1 {
		raw.Importance = 1
	}
	if raw.Importance == 0 {
		raw.Importance = 0.5 // default
	}

	// Clamp confidence to [0.05, 1]
	if raw.Confidence <= 0 {
		raw.Confidence = 0.5 // default
	}
	if raw.Confidence < 0.05 {
		raw.Confidence = 0.05
	}
	if raw.Confidence > 1 {
		raw.Confidence = 1
	}

	// Clean tags
	var cleanTags []string
	for _, tag := range raw.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			cleanTags = append(cleanTags, tag)
		}
	}
	raw.Tags = cleanTags

	return raw, true
}
