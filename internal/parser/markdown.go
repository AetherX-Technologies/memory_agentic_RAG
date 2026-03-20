package parser

import (
	"regexp"
	"strings"
)

var headingRe = regexp.MustCompile(`(?m)^(#{1,6})\s+(.+)$`)

// HasHeadings returns true if the content contains any Markdown headings (# to ######).
func HasHeadings(content string) bool {
	return headingRe.MatchString(content)
}

// SplitByHeadings splits Markdown content at heading boundaries.
// Each returned Section has the heading as Title and the content below it (until the next heading).
func SplitByHeadings(content string) []Section {
	matches := headingRe.FindAllStringIndex(content, -1)
	if len(matches) == 0 {
		return []Section{{Content: content, Title: ""}}
	}

	var sections []Section

	// Content before the first heading (preamble)
	if matches[0][0] > 0 {
		preamble := strings.TrimSpace(content[:matches[0][0]])
		if preamble != "" {
			sections = append(sections, Section{
				Content: preamble,
				Title:   "",
			})
		}
	}

	// Split at each heading
	for i, match := range matches {
		headingLine := content[match[0]:match[1]]
		title := extractHeadingTitle(headingLine)

		// Content runs from after this heading line to the start of the next heading (or end)
		contentStart := match[1]
		var contentEnd int
		if i+1 < len(matches) {
			contentEnd = matches[i+1][0]
		} else {
			contentEnd = len(content)
		}

		body := strings.TrimSpace(content[contentStart:contentEnd])

		sections = append(sections, Section{
			Content: body,
			Title:   title,
		})
	}

	return sections
}

// extractHeadingTitle strips the leading '#' characters and whitespace from a heading line.
func extractHeadingTitle(line string) string {
	line = strings.TrimSpace(line)
	line = strings.TrimLeft(line, "#")
	return strings.TrimSpace(line)
}

// splitByParagraph splits content into chunks of approximately maxTokens each,
// breaking at paragraph boundaries (\n\n).
func splitByParagraph(content string, maxTokens int) []Section {
	paragraphs := strings.Split(content, "\n\n")
	var sections []Section
	var current strings.Builder

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}

		proposed := current.String()
		if proposed != "" {
			proposed += "\n\n"
		}
		proposed += para

		if EstimateTokenCount(proposed) > maxTokens && current.Len() > 0 {
			// Flush current chunk
			sections = append(sections, Section{
				Content:    strings.TrimSpace(current.String()),
				TokenCount: EstimateTokenCount(current.String()),
			})
			current.Reset()
			current.WriteString(para)
		} else {
			if current.Len() > 0 {
				current.WriteString("\n\n")
			}
			current.WriteString(para)
		}
	}

	// Flush remaining
	if current.Len() > 0 {
		s := strings.TrimSpace(current.String())
		sections = append(sections, Section{
			Content:    s,
			TokenCount: EstimateTokenCount(s),
		})
	}

	return sections
}
