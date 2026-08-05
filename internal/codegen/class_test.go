package codegen

import (
	"strings"
	"testing"
)

func TestExtractClassBody(t *testing.T) {
	src := []byte(`#include <string>

class Person {
private:
    std::string name_;
    int age_;

public:
    Person(std::string name, int age) : name_(std::move(name)), age_(age) {}

    void greet() const {
        if (age_ > 0) {
            std::string s = "{ still not a brace }";
            (void)s;
        }
    }
};

int main() { return 0; }
`)

	open, close, err := ExtractClassBody(src, "Person")
	if err != nil {
		t.Fatalf("ExtractClassBody() error = %v", err)
	}
	if src[open] != '{' || src[close] != '}' {
		t.Fatalf("ExtractClassBody() = (%d, %d), expected these to point at '{' and '}', got %q and %q", open, close, src[open], src[close])
	}

	body := string(src[open:close])
	if want := "std::string name_;"; !strings.Contains(body, want) {
		t.Errorf("extracted body missing %q:\n%s", want, body)
	}
	// The closing brace found must be the class's own, not one from
	// somewhere inside greet()'s body or the string literal containing a
	// stray '{'/'}'.
	if want := "int main"; strings.Contains(string(src[open:close+2]), want) {
		t.Errorf("ExtractClassBody() overran into code after the class:\n%s", src[open:close+2])
	}
}

func TestExtractClassBodyStruct(t *testing.T) {
	src := []byte(`struct Point { int x; int y; };`)
	open, close, err := ExtractClassBody(src, "Point")
	if err != nil {
		t.Fatalf("ExtractClassBody() error = %v", err)
	}
	if string(src[open:close+1]) != "{ int x; int y; }" {
		t.Errorf("ExtractClassBody() = %q, want %q", src[open:close+1], "{ int x; int y; }")
	}
}

func TestExtractClassBodyNotFound(t *testing.T) {
	if _, _, err := ExtractClassBody([]byte(`class Other {};`), "Missing"); err == nil {
		t.Error("expected an error for a class that doesn't exist")
	}
}

func TestExtractClassBodyForwardDeclaration(t *testing.T) {
	if _, _, err := ExtractClassBody([]byte(`class Fwd;`), "Fwd"); err == nil {
		t.Error("expected an error for a forward declaration with no body")
	}
}
