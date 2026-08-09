package explain

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"cmaker/internal/codegen"
)

// sourceExtensions are the file types considered part of the project's C/C++
// surface for explain's whole-project searches - the same set cmd/run.go's
// isBuildRequired already treats as "tracked source", plus .cc/.cxx/.hxx for
// completeness (isBuildRequired only cares about staleness, not identifying
// every real source file, so it didn't need those).
var sourceExtensions = map[string]bool{
	".c": true, ".cpp": true, ".cc": true, ".cxx": true,
	".h": true, ".hpp": true, ".hh": true, ".hxx": true,
}

// WalkSourceFiles returns every C/C++ source/header file under root,
// relative to root, skipping build/ and .git/ (generated output and VCS
// metadata - the same two directories cmd/run.go's isBuildRequired already
// skips, for the same reason: neither is part of the project's real source
// surface).
func WalkSourceFiles(root string) ([]string, error) {
	var files []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && (info.Name() == "build" || info.Name() == ".git") {
			return filepath.SkipDir
		}
		if !info.IsDir() && sourceExtensions[filepath.Ext(path)] {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				rel = path
			}
			files = append(files, rel)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk %s: %w", root, err)
	}
	sort.Strings(files)
	return files, nil
}

// ClassMatch is one occurrence of a class/struct definition found by
// FindClass, ready to hand straight to BuildClassPrompt.
type ClassMatch struct {
	File       string // relative to the search root
	Body       string // the class's full definition, brace-to-brace
	Start, End int    // byte offsets within the file's own content ('cmaker improve' needs these to splice a replacement back in; Body == data[Start:End+1])
}

// FindClass searches every source file under root for a class or struct
// definition named className, using codegen.ExtractClassBody per-file - a
// project can legitimately have same-named classes in different
// files/namespaces, which is exactly the ambiguity cmd/explain.go's
// selection prompt exists to resolve, so every match is returned rather than
// just the first.
func FindClass(root, className string) ([]ClassMatch, error) {
	files, err := WalkSourceFiles(root)
	if err != nil {
		return nil, err
	}

	var matches []ClassMatch
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		bodyOpen, bodyClose, err := codegen.ExtractClassBody(data, className)
		if err != nil {
			continue
		}
		start := classDeclStart(data, className, bodyOpen)
		matches = append(matches, ClassMatch{File: f, Body: string(data[start : bodyClose+1]), Start: start, End: bodyClose})
	}
	return matches, nil
}

// FuncMatch is one occurrence of a function/method definition found by
// FindFunction, ready to hand straight to BuildFunctionPrompt. Named
// distinctly from codegen.FunctionMatch (which carries byte offsets, not
// resolved content) to avoid confusion between the two.
type FuncMatch struct {
	File       string // relative to the search root
	Body       string // the function's full body, brace-to-brace
	Start, End int    // byte offsets within the file's own content ('cmaker improve' needs these to splice a replacement back in; Body == data[Start:End+1])
}

// FindFunction searches every source file under root for definitions
// (codegen.FindFunctionDefinitions - not declarations, not call sites) of a
// free function or method named funcName. A single file can hold multiple
// overloads, and a project can hold the same function name in multiple
// files, so every match found anywhere is returned.
func FindFunction(root, funcName string) ([]FuncMatch, error) {
	files, err := WalkSourceFiles(root)
	if err != nil {
		return nil, err
	}

	var matches []FuncMatch
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			continue
		}
		for _, m := range codegen.FindFunctionDefinitions(data, funcName) {
			start := declStartBackward(data, m.NameStart)
			matches = append(matches, FuncMatch{File: f, Body: string(data[start : m.BodyClose+1]), Start: start, End: m.BodyClose})
		}
	}
	return matches, nil
}

// classDeclStart locates the "class <className>"/"struct <className>"
// keyword text codegen.ExtractClassBody itself already searched for
// (recomputed here, not exported by codegen, since ExtractClassBody only
// returns the brace-balanced body range) and returns its start offset - or
// bodyOpen unchanged if, for any reason, it can't be found again. Without
// this, ClassMatch.Body would start at the bare "{", which reads as
// incomplete/syntactically broken on its own (a live end-to-end check
// against claude-haiku-4-5 misread such a body-only snippet as "missing the
// class declaration" and asked for more context instead of explaining it).
func classDeclStart(src []byte, className string, bodyOpen int) int {
	re := regexp.MustCompile(`\b(class|struct)\s+` + regexp.QuoteMeta(className) + `\b`)
	loc := re.FindIndex(src)
	if loc == nil {
		return bodyOpen
	}
	return loc[0]
}

// declStartAllowedRe matches one character that could plausibly still be
// part of a function's return type/qualifiers/enclosing-class-prefix when
// scanning backward from its name (letters, digits, and the punctuation a
// C++ signature can contain before the name: "::", "*", "&", "~", "<...>"
// template args, whitespace).
var declStartAllowedRe = regexp.MustCompile(`[A-Za-z0-9_:*&~<>,\[\] \t\r\n]`)

// declStartBackward scans backward from nameStart (a FunctionMatch's
// NameStart, i.e. the first character of the function's own name) over
// characters declStartAllowedRe allows, stopping at the first character that
// isn't (typically ';', '}', or '{' - the end of whatever statement came
// before this one), then trims any leading whitespace/newlines so the
// result starts cleanly. This recovers the full signature (return type,
// "ClassName::", qualifiers) FunctionMatch's own BodyOpen/BodyClose
// intentionally don't carry - same rationale as classDeclStart above,
// caught by the same live-testing check.
//
// The character-class scan has no notion of line boundaries, so on its
// own it can walk straight through an entire unrelated preceding
// preprocessor directive (#include, #define, ...) - that line's own text
// (identifiers, "<angle brackets>", whitespace) happens to match the same
// "plausible return type" character class, so the scan doesn't stop until
// it hits the '#' itself - but since '#' isn't itself in the allowed set,
// the scan halts having already stepped past it, landing mid-directive
// rather than before it entirely. A real bug caught live once 'cmaker
// improve' started splicing this range back into a file (harmless for
// explain/read, which only ever display Body, but a literal "##include"
// corruption once something actually writes at this offset).
// skipPastLeadingDirectives corrects for that.
func declStartBackward(src []byte, nameStart int) int {
	i := nameStart
	for i > 0 && declStartAllowedRe.Match(src[i-1:i]) {
		i--
	}
	for i < nameStart && (src[i] == ' ' || src[i] == '\t' || src[i] == '\r' || src[i] == '\n') {
		i++
	}
	return skipPastLeadingDirectives(src, i, nameStart)
}

// skipPastLeadingDirectives corrects declStartBackward's character-class
// scan for the case where it stopped mid-way through a preceding '#'-
// prefixed preprocessor directive line (see declStartBackward's own doc).
// Recovers the true start of the line start is currently inside, then
// skips forward past every leading '#'-prefixed line found there (there
// can be more than one, e.g. consecutive #include lines), up to limit.
func skipPastLeadingDirectives(src []byte, start, limit int) int {
	lineStart := 0
	if idx := bytes.LastIndexByte(src[:start], '\n'); idx != -1 {
		lineStart = idx + 1
	}

	pos := lineStart
	for pos < limit {
		lineEnd := limit
		next := limit
		if nl := bytes.IndexByte(src[pos:limit], '\n'); nl != -1 {
			lineEnd = pos + nl
			next = lineEnd + 1
		}
		line := bytes.TrimSpace(src[pos:lineEnd])
		if len(line) == 0 || line[0] != '#' {
			break
		}
		pos = next
	}
	if pos <= start {
		return start
	}
	for pos < limit && (src[pos] == ' ' || src[pos] == '\t' || src[pos] == '\r' || src[pos] == '\n') {
		pos++
	}
	return pos
}
