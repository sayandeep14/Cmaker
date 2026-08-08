package codegen

import (
	"strings"
	"testing"
)

func bodyText(src []byte, m FunctionMatch) string {
	return string(src[m.BodyOpen : m.BodyClose+1])
}

func TestFindFunctionDefinitionsSimple(t *testing.T) {
	src := []byte(`int add(int a, int b) {
    return a + b;
}
`)
	matches := FindFunctionDefinitions(src, "add")
	if len(matches) != 1 {
		t.Fatalf("FindFunctionDefinitions() = %d matches, want 1", len(matches))
	}
	if !strings.Contains(bodyText(src, matches[0]), "return a + b;") {
		t.Errorf("body = %q, missing the function body", bodyText(src, matches[0]))
	}
}

func TestFindFunctionDefinitionsSkipsDeclarationOnly(t *testing.T) {
	src := []byte(`int add(int a, int b);

int main() { return 0; }
`)
	matches := FindFunctionDefinitions(src, "add")
	if len(matches) != 0 {
		t.Errorf("FindFunctionDefinitions() = %d matches, want 0 (declaration only, no body)", len(matches))
	}
}

func TestFindFunctionDefinitionsSkipsCallSite(t *testing.T) {
	src := []byte(`int add(int a, int b) { return a + b; }

int main() {
    int x = add(2, 3);
    return x;
}
`)
	matches := FindFunctionDefinitions(src, "add")
	if len(matches) != 1 {
		t.Fatalf("FindFunctionDefinitions() = %d matches, want exactly 1 (the definition, not the call in main)", len(matches))
	}
}

func TestFindFunctionDefinitionsConstMethod(t *testing.T) {
	src := []byte(`class Foo {
public:
    int getValue() const {
        return value_;
    }
private:
    int value_ = 0;
};
`)
	matches := FindFunctionDefinitions(src, "getValue")
	if len(matches) != 1 {
		t.Fatalf("FindFunctionDefinitions() = %d matches, want 1", len(matches))
	}
	if !strings.Contains(bodyText(src, matches[0]), "return value_;") {
		t.Errorf("body = %q, missing the method body", bodyText(src, matches[0]))
	}
}

func TestFindFunctionDefinitionsOverloads(t *testing.T) {
	src := []byte(`int add(int a, int b) {
    return a + b;
}

double add(double a, double b) {
    return a + b;
}
`)
	matches := FindFunctionDefinitions(src, "add")
	if len(matches) != 2 {
		t.Fatalf("FindFunctionDefinitions() = %d matches, want 2 (both overloads)", len(matches))
	}
}

func TestFindFunctionDefinitionsNestedBracesAndStrings(t *testing.T) {
	src := []byte(`void process() {
    if (true) {
        std::string s = "{ not a real brace }";
        for (int i = 0; i < 10; i++) {
            (void)s;
        }
    }
}
`)
	matches := FindFunctionDefinitions(src, "process")
	if len(matches) != 1 {
		t.Fatalf("FindFunctionDefinitions() = %d matches, want 1", len(matches))
	}
	got := bodyText(src, matches[0])
	if !strings.HasSuffix(strings.TrimSpace(got), "}") || !strings.Contains(got, "not a real brace") {
		t.Errorf("body = %q, expected the full nested body including the string-literal brace", got)
	}
}

func TestFindFunctionDefinitionsNoMatch(t *testing.T) {
	src := []byte(`int main() { return 0; }`)
	matches := FindFunctionDefinitions(src, "nonexistent")
	if len(matches) != 0 {
		t.Errorf("FindFunctionDefinitions() = %d matches, want 0", len(matches))
	}
}

func TestFindFunctionDefinitionsDefaultedSpecialMember(t *testing.T) {
	src := []byte(`class Foo {
public:
    Foo() = default;
    ~Foo() = default;
};
`)
	matches := FindFunctionDefinitions(src, "Foo")
	if len(matches) != 0 {
		t.Errorf("FindFunctionDefinitions() = %d matches, want 0 (= default has no real body)", len(matches))
	}
}
