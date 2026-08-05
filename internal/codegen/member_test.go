package codegen

import (
	"context"
	"strings"
	"testing"
)

type fakeCompleter struct {
	response string
	err      error

	gotSystem, gotUser string
}

func (f *fakeCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	f.gotSystem, f.gotUser = system, user
	return f.response, f.err
}

func TestExtractMembers(t *testing.T) {
	fc := &fakeCompleter{response: `[{"name": "age_", "type": "int", "is_const": false, "return_by_reference": false}]`}

	members, err := ExtractMembers(context.Background(), fc, "Person", "int age_;")
	if err != nil {
		t.Fatalf("ExtractMembers() error = %v", err)
	}
	if len(members) != 1 || members[0].Name != "age_" || members[0].Type != "int" {
		t.Fatalf("ExtractMembers() = %+v, want a single age_/int member", members)
	}
	if !strings.Contains(fc.gotUser, "Person") {
		t.Errorf("class name not forwarded into the prompt: %q", fc.gotUser)
	}
}

func TestExtractMembersStripsCodeFences(t *testing.T) {
	fc := &fakeCompleter{response: "```json\n[{\"name\": \"x_\", \"type\": \"int\", \"is_const\": false, \"return_by_reference\": false}]\n```"}

	members, err := ExtractMembers(context.Background(), fc, "C", "int x_;")
	if err != nil {
		t.Fatalf("ExtractMembers() error = %v", err)
	}
	if len(members) != 1 || members[0].Name != "x_" {
		t.Fatalf("ExtractMembers() = %+v, want a single x_ member", members)
	}
}

func TestExtractMembersEmpty(t *testing.T) {
	fc := &fakeCompleter{response: `[]`}
	members, err := ExtractMembers(context.Background(), fc, "Empty", "")
	if err != nil {
		t.Fatalf("ExtractMembers() error = %v", err)
	}
	if len(members) != 0 {
		t.Errorf("ExtractMembers() = %+v, want empty", members)
	}
}

func TestExtractMembersMalformedJSON(t *testing.T) {
	fc := &fakeCompleter{response: "not json at all"}
	if _, err := ExtractMembers(context.Background(), fc, "Bad", ""); err == nil {
		t.Error("expected an error for a non-JSON LLM response")
	}
}
