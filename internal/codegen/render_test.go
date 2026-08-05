package codegen

import (
	"strings"
	"testing"
)

func TestAccessorSuffix(t *testing.T) {
	tests := map[string]string{
		"x_":      "X",
		"_x":      "X",
		"m_count": "Count",
		"name":    "Name",
		"age":     "Age",
	}
	for in, want := range tests {
		if got := accessorSuffix(in); got != want {
			t.Errorf("accessorSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRenderAccessors(t *testing.T) {
	members := []Member{
		{Name: "age_", Type: "int"},
		{Name: "name_", Type: "std::string", ReturnByReference: true},
		{Name: "id_", Type: "int", IsConst: true},
	}
	out := RenderAccessors(members, "    ")

	for _, want := range []string{
		markerBegin,
		markerEnd,
		"public:",
		"int getAge() const { return age_; }",
		"void setAge(int value) { age_ = value; }",
		"const std::string& getName() const { return name_; }",
		"void setName(const std::string& value) { name_ = value; }",
		"int getId() const { return id_; }",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("RenderAccessors() missing %q:\n%s", want, out)
		}
	}
	// const member must not get a setter.
	if strings.Contains(out, "setId") {
		t.Errorf("RenderAccessors() generated a setter for a const member:\n%s", out)
	}
}

func TestInsertAccessorsFreshInsert(t *testing.T) {
	src := []byte(`class Person {
private:
    std::string name_;

public:
    Person() = default;
};
`)
	members := []Member{{Name: "name_", Type: "std::string", ReturnByReference: true}}

	out, err := InsertAccessors(src, "Person", members)
	if err != nil {
		t.Fatalf("InsertAccessors() error = %v", err)
	}
	if !strings.Contains(string(out), "getName") {
		t.Errorf("InsertAccessors() output missing generated getter:\n%s", out)
	}
	if !strings.Contains(string(out), "Person() = default;") {
		t.Errorf("InsertAccessors() lost existing class content:\n%s", out)
	}
}

func TestInsertAccessorsReplacesPreviousBlock(t *testing.T) {
	src := []byte(`class Person {
private:
    std::string name_;
};
`)
	members := []Member{{Name: "name_", Type: "std::string"}}

	first, err := InsertAccessors(src, "Person", members)
	if err != nil {
		t.Fatalf("first InsertAccessors() error = %v", err)
	}

	// Simulate a member being renamed and regenerated - the second run
	// should replace the first block, not append a second one.
	renamed := []Member{{Name: "full_name_", Type: "std::string"}}
	second, err := InsertAccessors(first, "Person", renamed)
	if err != nil {
		t.Fatalf("second InsertAccessors() error = %v", err)
	}

	out := string(second)
	if strings.Count(out, markerBegin) != 1 {
		t.Errorf("expected exactly one generated block after regenerating, got %d:\n%s", strings.Count(out, markerBegin), out)
	}
	if strings.Contains(out, "getName") {
		t.Errorf("stale accessor from the first run was not replaced:\n%s", out)
	}
	if !strings.Contains(out, "getFullName") {
		t.Errorf("regenerated accessor missing:\n%s", out)
	}
}
