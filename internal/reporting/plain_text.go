package reporting

import (
	"regexp"
	"strings"
)

var markdownLinkPattern = regexp.MustCompile(
	`\[([^\]]+)\]\(([^)]+)\)`,
)

func markdownToPlainText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	lines := strings.Split(value, "\n")
	output := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			appendPlainTextLine(&output, "")
			continue
		}
		line = stripMarkdownHeading(line)
		line = stripMarkdownQuote(line)
		if isMarkdownHorizontalRule(line) {
			continue
		}
		if strings.Contains(line, "|") {
			cells := markdownTableCells(line)
			if len(cells) >= 2 {
				if isMarkdownTableSeparator(cells) {
					continue
				}
				line = strings.Join(cells, " / ")
			}
		}
		line = stripMarkdownBullet(line)
		line = markdownLinkPattern.ReplaceAllString(
			line,
			"$1 ($2)",
		)
		for _, marker := range []string{
			"**",
			"__",
			"~~",
			"`",
			"*",
		} {
			line = strings.ReplaceAll(line, marker, "")
		}
		line = strings.TrimSpace(line)
		if line != "" {
			appendPlainTextLine(&output, line)
		}
	}
	for len(output) > 0 && output[len(output)-1] == "" {
		output = output[:len(output)-1]
	}
	return strings.TrimSpace(strings.Join(output, "\n"))
}

func stripMarkdownHeading(line string) string {
	index := 0
	for index < len(line) && line[index] == '#' {
		index++
	}
	if index > 0 && index < len(line) && line[index] == ' ' {
		return strings.TrimSpace(line[index+1:])
	}
	return line
}

func stripMarkdownQuote(line string) string {
	for strings.HasPrefix(line, ">") {
		line = strings.TrimSpace(strings.TrimPrefix(line, ">"))
	}
	return line
}

func stripMarkdownBullet(line string) string {
	for _, prefix := range []string{"- ", "* ", "+ "} {
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return line
}

func isMarkdownHorizontalRule(line string) bool {
	compact := strings.ReplaceAll(line, " ", "")
	if len(compact) < 3 {
		return false
	}
	for _, marker := range []rune{'-', '*', '_'} {
		if strings.Trim(compact, string(marker)) == "" {
			return true
		}
	}
	return false
}

func markdownTableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "|")
	line = strings.TrimSuffix(line, "|")
	rawCells := strings.Split(line, "|")
	cells := make([]string, 0, len(rawCells))
	for _, cell := range rawCells {
		cell = strings.TrimSpace(cell)
		for _, marker := range []string{"**", "__", "`", "*"} {
			cell = strings.ReplaceAll(cell, marker, "")
		}
		cells = append(cells, strings.TrimSpace(cell))
	}
	return cells
}

func isMarkdownTableSeparator(cells []string) bool {
	for _, cell := range cells {
		cell = strings.TrimSpace(cell)
		if !strings.Contains(cell, "-") ||
			strings.Trim(cell, "-: ") != "" {
			return false
		}
	}
	return len(cells) > 0
}

func appendPlainTextLine(lines *[]string, line string) {
	if line == "" &&
		(len(*lines) == 0 || (*lines)[len(*lines)-1] == "") {
		return
	}
	*lines = append(*lines, line)
}
