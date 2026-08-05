package heal

import (
	"reflect"
	"testing"
)

func TestExtractReferencedFiles(t *testing.T) {
	log := `[ 50%] Building CXX object CMakeFiles/main.dir/src/main.cpp.o
src/main.cpp:12:5: error: use of undeclared identifier 'foo'
    foo();
    ^
src/main.cpp:20:10: error: expected ';' after expression
1 error generated.
`
	got := ExtractReferencedFiles(log, 5)
	want := []string{"src/main.cpp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractReferencedFiles() = %v, want %v (deduped)", got, want)
	}
}

func TestExtractReferencedFilesMultipleFiles(t *testing.T) {
	log := `src/main.cpp:1:1: error: bad
include/mylib/mylib.h:5:3: error: also bad
src/helper.cpp:9:2: error: yet another
`
	got := ExtractReferencedFiles(log, 5)
	want := []string{"src/main.cpp", "include/mylib/mylib.h", "src/helper.cpp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractReferencedFiles() = %v, want %v (first-seen order)", got, want)
	}
}

func TestExtractReferencedFilesRespectsMax(t *testing.T) {
	log := `a.cpp:1:1: error: x
b.cpp:1:1: error: x
c.cpp:1:1: error: x
`
	got := ExtractReferencedFiles(log, 2)
	if len(got) != 2 {
		t.Errorf("ExtractReferencedFiles(max=2) = %v, want 2 entries", got)
	}
}

func TestExtractReferencedFilesNoMatches(t *testing.T) {
	got := ExtractReferencedFiles("nothing relevant here\njust some log text\n", 5)
	if len(got) != 0 {
		t.Errorf("ExtractReferencedFiles() = %v, want empty", got)
	}
}

func TestExtractReferencedFilesNoColumnNumber(t *testing.T) {
	log := "src/main.cpp:42: error: something went wrong\n"
	got := ExtractReferencedFiles(log, 5)
	want := []string{"src/main.cpp"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractReferencedFiles() = %v, want %v (no column number)", got, want)
	}
}
