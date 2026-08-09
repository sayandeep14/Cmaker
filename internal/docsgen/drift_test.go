package docsgen

import "testing"

func TestFindDriftIssuesNoDrift(t *testing.T) {
	src := `/**
 * @brief Adds two integers.
 * @param a first operand
 * @param b second operand
 * @return the sum
 */
int add(int a, int b) {
    return a + b;
}
`
	if got := FindDriftIssues("a.cpp", src); len(got) != 0 {
		t.Errorf("FindDriftIssues() = %+v, want no issues", got)
	}
}

func TestFindDriftIssuesMissingParam(t *testing.T) {
	// Documented "b", but the real signature only has "a" - a stale @param.
	src := `/**
 * @brief Negates a number.
 * @param a the operand
 * @param b unused now
 * @return the negation
 */
int negate(int a) {
    return -a;
}
`
	got := FindDriftIssues("a.cpp", src)
	if len(got) != 1 {
		t.Fatalf("FindDriftIssues() = %+v, want 1 issue", got)
	}
	if len(got[0].Missing) != 1 || got[0].Missing[0] != "b" {
		t.Errorf("FindDriftIssues()[0].Missing = %v, want [b]", got[0].Missing)
	}
	if len(got[0].Undocumented) != 0 {
		t.Errorf("FindDriftIssues()[0].Undocumented = %v, want none", got[0].Undocumented)
	}
}

func TestFindDriftIssuesUndocumentedParam(t *testing.T) {
	// The function gained a new "scale" parameter with no @param.
	src := `/**
 * @brief Adds two integers.
 * @param a first operand
 * @param b second operand
 * @return the sum
 */
int add(int a, int b, int scale) {
    return (a + b) * scale;
}
`
	got := FindDriftIssues("a.cpp", src)
	if len(got) != 1 {
		t.Fatalf("FindDriftIssues() = %+v, want 1 issue", got)
	}
	if len(got[0].Undocumented) != 1 || got[0].Undocumented[0] != "scale" {
		t.Errorf("FindDriftIssues()[0].Undocumented = %v, want [scale]", got[0].Undocumented)
	}
}

func TestFindDriftIssuesSkipsUnrelatedComment(t *testing.T) {
	src := `/**
 * Just some notes, not attached to anything.
 */

// blank line above means this isn't "directly above" in the doc-comment sense either way
int helper() {
    return 0;
}
`
	if got := FindDriftIssues("a.cpp", src); len(got) != 0 {
		t.Errorf("FindDriftIssues() = %+v, want no issues (no @param at all)", got)
	}
}

func TestFindDriftIssuesSkipsFunctionPointerParam(t *testing.T) {
	// A function-pointer parameter is intentionally not confidently
	// name-extracted - never flag it rather than risk a false positive.
	src := `/**
 * @brief Registers a callback.
 * @param cb the callback
 */
void registerCallback(void (*cb)(int)) {
}
`
	if got := FindDriftIssues("a.cpp", src); len(got) != 0 {
		t.Errorf("FindDriftIssues() = %+v, want no issues (ambiguous signature skipped)", got)
	}
}

func TestFindDriftIssuesHandlesDefaultArgWithNestedParens(t *testing.T) {
	src := `/**
 * @brief Computes something.
 * @param a first
 * @param b second
 */
int compute(int a, int b = foo(1, 2)) {
    return a + b;
}
`
	if got := FindDriftIssues("a.cpp", src); len(got) != 0 {
		t.Errorf("FindDriftIssues() = %+v, want no issues (default-arg parens shouldn't split params)", got)
	}
}

func TestFindDriftIssuesVoidParams(t *testing.T) {
	src := `/**
 * @brief Does nothing useful, but documented with a stray @param.
 * @param x doesn't exist
 */
void noop(void) {
}
`
	got := FindDriftIssues("a.cpp", src)
	if len(got) != 1 || len(got[0].Missing) != 1 || got[0].Missing[0] != "x" {
		t.Errorf("FindDriftIssues() = %+v, want one issue with Missing=[x]", got)
	}
}

func TestFindDriftIssuesTemplateTypeNotSplitOnComma(t *testing.T) {
	src := `/**
 * @brief Sums a map's values.
 * @param m the map
 */
int sumValues(std::map<int, int> m) {
    return 0;
}
`
	if got := FindDriftIssues("a.cpp", src); len(got) != 0 {
		t.Errorf("FindDriftIssues() = %+v, want no issues (template commas shouldn't split params)", got)
	}
}

func TestFindDriftIssuesMultipleFunctions(t *testing.T) {
	src := `/**
 * @brief First.
 * @param a x
 */
int first(int a) { return a; }

/**
 * @brief Second.
 * @param missing not real
 */
int second() { return 0; }
`
	got := FindDriftIssues("a.cpp", src)
	if len(got) != 1 {
		t.Fatalf("FindDriftIssues() = %+v, want exactly 1 issue (only 'second' has drift)", got)
	}
	if got[0].Signature == "" {
		t.Error("expected a non-empty Signature field")
	}
}
