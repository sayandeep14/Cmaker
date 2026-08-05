package codegen

import (
	"bytes"
	"fmt"
	"strings"
)

// markerBegin/markerEnd wrap cmaker's own generated output inside a class
// body so a second `cmaker generate accessors` run can find and replace its
// previous output in place instead of duplicating it.
const (
	markerBegin = "// --- cmaker generated accessors: begin ---"
	markerEnd   = "// --- cmaker generated accessors: end ---"
)

// RenderAccessors deterministically renders a public: block of getX()/
// setX(...) methods for members, indented by indent. Members whose IsConst
// is true only get a getter (there is nothing sensible to mutate).
func RenderAccessors(members []Member, indent string) string {
	var b strings.Builder
	b.WriteString(indent + markerBegin + "\n")
	b.WriteString(indent + "public:\n")
	for _, m := range members {
		suffix := accessorSuffix(m.Name)

		paramType := m.Type
		if m.ReturnByReference {
			paramType = "const " + m.Type + "&"
		}

		fmt.Fprintf(&b, "%s%s get%s() const { return %s; }\n", indent, paramType, suffix, m.Name)
		if !m.IsConst {
			fmt.Fprintf(&b, "%svoid set%s(%s value) { %s = value; }\n", indent, suffix, paramType, m.Name)
		}
	}
	b.WriteString(indent + markerEnd + "\n")
	return b.String()
}

// accessorSuffix derives the "X" in getX()/setX() from a member's raw name:
// strips the common private-member naming conventions (trailing/leading
// underscore, "m_" prefix) so e.g. "x_", "_x", and "m_x" all produce "X"
// rather than leaking the convention into the public method name, then
// CamelCases any remaining underscore-separated words (e.g. "full_name_"
// produces "FullName", not "Full_name").
func accessorSuffix(memberName string) string {
	n := memberName
	n = strings.TrimSuffix(n, "_")
	n = strings.TrimPrefix(n, "_")
	n = strings.TrimPrefix(n, "m_")
	if n == "" {
		n = memberName
	}

	var b strings.Builder
	for word := range strings.SplitSeq(n, "_") {
		if word == "" {
			continue
		}
		b.WriteString(strings.ToUpper(word[:1]) + word[1:])
	}
	if b.Len() == 0 {
		return memberName
	}
	return b.String()
}

// InsertAccessors renders members and inserts them into className's body in
// src. If a previous cmaker-generated block (delimited by markerBegin/
// markerEnd) is already present, it's replaced in place; otherwise the new
// block is inserted just before the class's closing brace.
func InsertAccessors(src []byte, className string, members []Member) ([]byte, error) {
	bodyOpen, bodyClose, err := ExtractClassBody(src, className)
	if err != nil {
		return nil, err
	}

	indent := closingBraceIndent(src, bodyClose)
	block := RenderAccessors(members, indent+"    ")

	existingBegin := bytes.Index(src[bodyOpen:bodyClose], []byte(markerBegin))
	if existingBegin < 0 {
		var out []byte
		out = append(out, src[:bodyClose]...)
		out = append(out, []byte(block)...)
		out = append(out, src[bodyClose:]...)
		return out, nil
	}

	existingBegin += bodyOpen
	existingEnd := bytes.Index(src[existingBegin:bodyClose], []byte(markerEnd))
	if existingEnd < 0 {
		return nil, fmt.Errorf("found a cmaker accessors begin-marker without a matching end-marker in class %q - fix or remove it manually before regenerating", className)
	}
	existingEnd += existingBegin + len(markerEnd)
	if nl := bytes.IndexByte(src[existingEnd:], '\n'); nl >= 0 {
		existingEnd += nl + 1
	}
	lineStart := bytes.LastIndexByte(src[:existingBegin], '\n') + 1

	var out []byte
	out = append(out, src[:lineStart]...)
	out = append(out, []byte(block)...)
	out = append(out, src[existingEnd:]...)
	return out, nil
}

// closingBraceIndent returns the whitespace prefix of the line containing
// bodyClose, falling back to a plain 4-space indent when that line has
// non-whitespace content before the brace (e.g. "};" written densely with
// no line of its own).
func closingBraceIndent(src []byte, bodyClose int) string {
	lineStart := bytes.LastIndexByte(src[:bodyClose], '\n') + 1
	prefix := string(src[lineStart:bodyClose])
	if strings.TrimSpace(prefix) != "" {
		return "    "
	}
	return prefix
}
