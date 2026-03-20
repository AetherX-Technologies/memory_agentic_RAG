package parser

import (
	"testing"
)

func TestHasHeadings(t *testing.T) {
	tests := []struct {
		content string
		want    bool
	}{
		{"# Title\nContent here", true},
		{"## Subtitle\nMore content", true},
		{"### Deep heading\n", true},
		{"No headings here\nJust plain text", false},
		{"Not a #heading", false},
		{"", false},
	}
	for _, tt := range tests {
		got := HasHeadings(tt.content)
		if got != tt.want {
			t.Errorf("HasHeadings(%q) = %v, want %v", tt.content[:min(30, len(tt.content))], got, tt.want)
		}
	}
}

func TestSplitByHeadings(t *testing.T) {
	content := `Preamble text here.

# Chapter 1

Content of chapter 1.

## Section 1.1

Content of section 1.1.

# Chapter 2

Content of chapter 2.`

	sections := SplitByHeadings(content)

	// Expect: preamble + Chapter 1 + Section 1.1 + Chapter 2 = 4 sections
	if len(sections) != 4 {
		for i, s := range sections {
			t.Logf("section[%d]: title=%q content=%q", i, s.Title, s.Content[:min(50, len(s.Content))])
		}
		t.Fatalf("expected 4 sections, got %d", len(sections))
	}

	if sections[0].Title != "" {
		t.Errorf("preamble title should be empty, got %q", sections[0].Title)
	}
	if sections[1].Title != "Chapter 1" {
		t.Errorf("section[1].Title = %q, want %q", sections[1].Title, "Chapter 1")
	}
	if sections[2].Title != "Section 1.1" {
		t.Errorf("section[2].Title = %q, want %q", sections[2].Title, "Section 1.1")
	}
	if sections[3].Title != "Chapter 2" {
		t.Errorf("section[3].Title = %q, want %q", sections[3].Title, "Chapter 2")
	}
}

func TestSplitByHeadings_NoHeadings(t *testing.T) {
	content := "Just plain text without any headings."
	sections := SplitByHeadings(content)
	if len(sections) != 1 {
		t.Fatalf("expected 1 section, got %d", len(sections))
	}
	if sections[0].Content != content {
		t.Errorf("content mismatch")
	}
}

func TestSplitByParagraph(t *testing.T) {
	content := "First paragraph.\n\nSecond paragraph.\n\nThird paragraph.\n\nFourth paragraph."
	sections := splitByParagraph(content, 5) // very small max to force splitting
	if len(sections) < 2 {
		t.Fatalf("expected multiple sections, got %d", len(sections))
	}
	for _, s := range sections {
		if s.Content == "" {
			t.Error("got empty section content")
		}
	}
}
