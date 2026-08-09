package suggest

import (
	"strings"
	"testing"
)

const sampleChecklist = `# cmaker suggest

- [ ] Add bounds checking to parseArgs

  Currently reads argv[2] without checking argc first.

- [x] Use const references in Widget::draw

- [ ] Extract shared validation logic
`

func TestParseChecklistTopLevel(t *testing.T) {
	items := ParseChecklist([]byte(sampleChecklist))
	if len(items) != 3 {
		t.Fatalf("ParseChecklist() = %d items, want 3", len(items))
	}
	if items[0].Text != "Add bounds checking to parseArgs" {
		t.Errorf("items[0].Text = %q", items[0].Text)
	}
	if items[0].Checked {
		t.Error("items[0].Checked = true, want false")
	}
	if !strings.Contains(items[0].Detail, "argv[2]") {
		t.Errorf("items[0].Detail = %q, want to contain the detail paragraph", items[0].Detail)
	}
	if !items[1].Checked {
		t.Error("items[1].Checked = false, want true")
	}
	if items[2].Text != "Extract shared validation logic" {
		t.Errorf("items[2].Text = %q", items[2].Text)
	}
}

func TestFindFirstUncheckedSkipsChecked(t *testing.T) {
	items := ParseChecklist([]byte(sampleChecklist))
	got := FindFirstUnchecked(items)
	if got == nil || got.Text != "Add bounds checking to parseArgs" {
		t.Fatalf("FindFirstUnchecked() = %v, want the first unchecked top-level item", got)
	}
}

func TestFindFirstUncheckedNoneLeft(t *testing.T) {
	allChecked := "- [x] one\n- [x] two\n"
	items := ParseChecklist([]byte(allChecked))
	if got := FindFirstUnchecked(items); got != nil {
		t.Errorf("FindFirstUnchecked() = %v, want nil", got)
	}
}

func TestFindFirstUncheckedPrefersUncheckedChildOverParent(t *testing.T) {
	data := "- [ ] Big task\n  - [x] sub one\n  - [ ] sub two\n"
	items := ParseChecklist([]byte(data))
	got := FindFirstUnchecked(items)
	if got == nil || got.Text != "sub two" {
		t.Fatalf("FindFirstUnchecked() = %v, want the first unchecked child, not the broken-down parent", got)
	}
	if got.Parent == nil || got.Parent.Text != "Big task" {
		t.Error("expected the returned child to have Parent set")
	}
}

func TestFindByIndex(t *testing.T) {
	items := ParseChecklist([]byte(sampleChecklist))
	got, err := FindByIndex(items, 3)
	if err != nil {
		t.Fatalf("FindByIndex() error = %v", err)
	}
	if got.Text != "Extract shared validation logic" {
		t.Errorf("FindByIndex(3) = %q", got.Text)
	}
}

func TestFindByIndexOutOfRange(t *testing.T) {
	items := ParseChecklist([]byte(sampleChecklist))
	if _, err := FindByIndex(items, 99); err == nil {
		t.Error("FindByIndex() with an out-of-range index: expected an error, got nil")
	}
}

func TestFindByIndexAlreadyCheckedLeaf(t *testing.T) {
	items := ParseChecklist([]byte(sampleChecklist))
	got, err := FindByIndex(items, 2) // "Use const references..." is already [x]
	if err != nil {
		t.Fatalf("FindByIndex() error = %v", err)
	}
	if got != nil {
		t.Errorf("FindByIndex() on an already-checked leaf = %v, want nil", got)
	}
}

func TestFindByIndexBrokenDownItem(t *testing.T) {
	data := "- [ ] Big task\n  - [x] sub one\n  - [ ] sub two\n"
	items := ParseChecklist([]byte(data))
	got, err := FindByIndex(items, 1)
	if err != nil {
		t.Fatalf("FindByIndex() error = %v", err)
	}
	if got == nil || got.Text != "sub two" {
		t.Fatalf("FindByIndex(1) on a broken-down item = %v, want its first unchecked child", got)
	}
}

func TestFullTaskIncludesParentContext(t *testing.T) {
	data := "- [ ] Big task\n  - [ ] sub one\n"
	items := ParseChecklist([]byte(data))
	child := items[0].Children[0]
	got := child.FullTask()
	if !strings.Contains(got, "Big task") || !strings.Contains(got, "sub one") {
		t.Errorf("FullTask() = %q, want both parent and child text", got)
	}
}

func TestSetChecked(t *testing.T) {
	items := ParseChecklist([]byte(sampleChecklist))
	updated := SetChecked([]byte(sampleChecklist), items[0], true)

	reparsed := ParseChecklist(updated)
	if !reparsed[0].Checked {
		t.Error("SetChecked() didn't mark the item done")
	}
	if !reparsed[1].Checked {
		t.Error("SetChecked() should leave other items' checked state untouched")
	}
	if reparsed[2].Checked {
		t.Error("SetChecked() should leave other items' checked state untouched")
	}
}

func TestInsertSubtasks(t *testing.T) {
	items := ParseChecklist([]byte(sampleChecklist))
	updated := InsertSubtasks([]byte(sampleChecklist), items[0], []string{"Sub A", "Sub B"})

	reparsed := ParseChecklist(updated)
	if len(reparsed) != 3 {
		t.Fatalf("InsertSubtasks() changed the number of top-level items: got %d, want 3", len(reparsed))
	}
	if len(reparsed[0].Children) != 2 {
		t.Fatalf("InsertSubtasks() = %d children under the first item, want 2", len(reparsed[0].Children))
	}
	if reparsed[0].Children[0].Text != "Sub A" || reparsed[0].Children[1].Text != "Sub B" {
		t.Errorf("InsertSubtasks() children = %+v", reparsed[0].Children)
	}
	// The original detail paragraph and the other two items must survive.
	if !strings.Contains(reparsed[0].Detail, "argv[2]") {
		t.Error("InsertSubtasks() lost the original item's detail paragraph")
	}
	if reparsed[1].Text != "Use const references in Widget::draw" {
		t.Errorf("InsertSubtasks() disturbed a later item: got %q", reparsed[1].Text)
	}
}

func TestParseChecklistEmpty(t *testing.T) {
	items := ParseChecklist([]byte("# cmaker suggest\n\nNo suggestions.\n"))
	if len(items) != 0 {
		t.Errorf("ParseChecklist() on a file with no checkboxes = %d items, want 0", len(items))
	}
}
