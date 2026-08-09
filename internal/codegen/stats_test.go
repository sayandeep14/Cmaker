package codegen

import "testing"

func TestCountGeneratedBlocksInContentNoBlocks(t *testing.T) {
	blocks, lines := CountGeneratedBlocksInContent("int main() { return 0; }\n")
	if blocks != 0 || lines != 0 {
		t.Errorf("CountGeneratedBlocksInContent() = (%d, %d), want (0, 0)", blocks, lines)
	}
}

func TestCountGeneratedBlocksInContentOneBlock(t *testing.T) {
	content := "class Widget {\n" +
		markerBegin + "\n" +
		"public:\n" +
		"    int getX() const { return x_; }\n" +
		markerEnd + "\n" +
		"};\n"
	blocks, lines := CountGeneratedBlocksInContent(content)
	if blocks != 1 {
		t.Errorf("CountGeneratedBlocksInContent() blocks = %d, want 1", blocks)
	}
	if lines != 2 {
		t.Errorf("CountGeneratedBlocksInContent() lines = %d, want 2", lines)
	}
}

func TestCountGeneratedBlocksInContentMultipleBlocks(t *testing.T) {
	content := markerBegin + "\n" + "a\n" + markerEnd + "\n" +
		"unrelated\n" +
		markerBegin + "\n" + "b\n" + "c\n" + markerEnd + "\n"
	blocks, lines := CountGeneratedBlocksInContent(content)
	if blocks != 2 {
		t.Errorf("CountGeneratedBlocksInContent() blocks = %d, want 2", blocks)
	}
	if lines != 3 {
		t.Errorf("CountGeneratedBlocksInContent() lines = %d, want 3", lines)
	}
}

func TestCountGeneratedBlocksInContentUnmatchedBeginIgnored(t *testing.T) {
	content := markerBegin + "\n" + "dangling, no end marker\n"
	blocks, lines := CountGeneratedBlocksInContent(content)
	if blocks != 0 || lines != 0 {
		t.Errorf("CountGeneratedBlocksInContent() with an unmatched begin marker = (%d, %d), want (0, 0)", blocks, lines)
	}
}
