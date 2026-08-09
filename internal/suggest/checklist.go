package suggest

import (
	"fmt"
	"regexp"
	"strings"
)

// Item is one checklist entry parsed back out of RenderChecklist's own
// markdown format (or a hand-edited file in the same shape) - the read
// side of the format suggest.go's RenderChecklist writes, used by
// `cmaker codegen --intent-from` (see cmd/codegen.go) to pull a task,
// mark it done, or break it into sub-tasks in place.
type Item struct {
	Text     string
	Detail   string
	Checked  bool
	Line     int // index into the file's lines, where the "- [ ]"/"- [x]" text lives
	Parent   *Item
	Children []*Item
}

var (
	topItemRe = regexp.MustCompile(`^- \[([ xX])\] (.+)$`)
	subItemRe = regexp.MustCompile(`^  - \[([ xX])\] (.+)$`)
)

// ParseChecklist parses a markdown checklist in RenderChecklist's format:
// top-level items as "- [ ] Title" (optionally followed by an indented
// detail paragraph), and (only ever written by codegen's own breakdown
// step, never by RenderChecklist itself) 2-space-indented sub-items
// "  - [ ] Sub-task" nested under the most recently seen top-level item.
func ParseChecklist(data []byte) []*Item {
	lines := strings.Split(string(data), "\n")
	var top []*Item
	var current *Item

	for i, line := range lines {
		if m := topItemRe.FindStringSubmatch(line); m != nil {
			it := &Item{Text: strings.TrimSpace(m[2]), Checked: isCheckedMark(m[1]), Line: i}
			top = append(top, it)
			current = it
			continue
		}
		if m := subItemRe.FindStringSubmatch(line); m != nil && current != nil {
			child := &Item{Text: strings.TrimSpace(m[2]), Checked: isCheckedMark(m[1]), Line: i, Parent: current}
			current.Children = append(current.Children, child)
			continue
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" || current == nil || !strings.HasPrefix(line, " ") {
			continue
		}
		target := current
		if len(current.Children) > 0 {
			target = current.Children[len(current.Children)-1]
		}
		if target.Detail != "" {
			target.Detail += " "
		}
		target.Detail += trimmed
	}
	return top
}

func isCheckedMark(mark string) bool {
	return strings.EqualFold(mark, "x")
}

// FindFirstUnchecked walks items in document order and returns the first
// actionable unchecked one: a leaf (no children) that's unchecked, or -
// if a top-level item has already been broken down into sub-items - its
// first unchecked child, never the parent itself (a broken-down parent is
// just a container from that point on, see AssessBreakdown/InsertSubtasks
// in cmd/codegen.go). Returns nil if every item is checked (or checked
// via all its children being checked).
func FindFirstUnchecked(items []*Item) *Item {
	for _, it := range items {
		if len(it.Children) > 0 {
			for _, c := range it.Children {
				if !c.Checked {
					return c
				}
			}
			continue
		}
		if !it.Checked {
			return it
		}
	}
	return nil
}

// FindByIndex returns the num'th (1-based) top-level item - if it's
// already been broken down into sub-items, its first unchecked child is
// returned instead (mirroring FindFirstUnchecked's "never the parent
// itself once broken down" rule), or nil if every child is already
// checked. Returns an error if num is out of range.
func FindByIndex(items []*Item, num int) (*Item, error) {
	if num < 1 || num > len(items) {
		return nil, fmt.Errorf("suggestion #%d out of range - there are %d top-level suggestions", num, len(items))
	}
	it := items[num-1]
	if len(it.Children) == 0 {
		return it, nil
	}
	for _, c := range it.Children {
		if !c.Checked {
			return c, nil
		}
	}
	return nil, nil
}

// FullTask builds the task text to send an LLM for item - its own text
// plus detail, prefixed with its parent's text for context if it's a
// sub-task from a breakdown.
func (i *Item) FullTask() string {
	task := i.Text
	if i.Detail != "" {
		task += "\n\n" + i.Detail
	}
	if i.Parent != nil {
		return "Parent task: " + i.Parent.Text + "\nSub-task: " + task
	}
	return task
}

var checkboxMarkRe = regexp.MustCompile(`\[[ xX]\]`)

// SetChecked returns data with item's checkbox line rewritten to
// "- [x]"/"- [ ]" - a targeted single-line edit, leaving the rest of the
// file (including any hand-written formatting) untouched.
func SetChecked(data []byte, item *Item, checked bool) []byte {
	lines := strings.Split(string(data), "\n")
	if item.Line < 0 || item.Line >= len(lines) {
		return data
	}
	mark := "[ ]"
	if checked {
		mark = "[x]"
	}
	lines[item.Line] = checkboxMarkRe.ReplaceAllString(lines[item.Line], mark)
	return []byte(strings.Join(lines, "\n"))
}

// InsertSubtasks returns data with subtasks inserted as new 2-space-indented
// "  - [ ] <text>" checklist lines - used when AssessBreakdown
// (internal/agentic) decides a task is too large for one shot. Inserted
// right before parent's next top-level sibling (or at EOF if it has none),
// not immediately after parent's own line, so any existing detail
// paragraph under parent stays attached to parent rather than being
// mistaken for trailing detail on the last inserted sub-item. The caller
// should re-parse the result (ParseChecklist) rather than keep using
// stale *Item line numbers, since every line at or after the insertion
// point has shifted.
func InsertSubtasks(data []byte, parent *Item, subtasks []string) []byte {
	if len(subtasks) == 0 {
		return data
	}
	lines := strings.Split(string(data), "\n")
	if parent.Line < 0 || parent.Line >= len(lines) {
		return data
	}
	insertAt := len(lines)
	for i := parent.Line + 1; i < len(lines); i++ {
		if topItemRe.MatchString(lines[i]) {
			insertAt = i
			break
		}
	}
	newLines := make([]string, 0, len(subtasks))
	for _, t := range subtasks {
		newLines = append(newLines, "  - [ ] "+strings.TrimSpace(t))
	}

	result := make([]string, 0, len(lines)+len(newLines))
	result = append(result, lines[:insertAt]...)
	result = append(result, newLines...)
	result = append(result, lines[insertAt:]...)
	return []byte(strings.Join(result, "\n"))
}
