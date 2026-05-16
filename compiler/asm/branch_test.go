package asm

import (
	"bytes"
	"testing"
)

func TestEncodeBackwardLoopJmp(t *testing.T) {
	code, diags := ParseBody("loop: hlt; jmp loop", nil)
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %v", diags)
	}

	out, diags := Encode(code)
	if len(diags) != 0 {
		t.Fatalf("encode diagnostics: %v", diags)
	}
	want := []byte{0xF4, 0xE9, 0xFA, 0xFF, 0xFF, 0xFF}
	if !bytes.Equal(out, want) {
		t.Fatalf("jump bytes = %#x, want %#x", out, want)
	}
}

func TestEncodeConditionalBranchNear32(t *testing.T) {
	src := "loop:\nhlt\nje loop\njne loop\njb loop\njbe loop\njl loop\njle loop\njg loop\njge loop"
	code, diags := ParseBody(src, nil)
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %v", diags)
	}

	out, diags := Encode(code)
	if len(diags) != 0 {
		t.Fatalf("encode diagnostics: %v", diags)
	}

	want := []byte{
		0xF4,
		0x0F, 0x84, 0xF9, 0xFF, 0xFF, 0xFF,
		0x0F, 0x85, 0xF3, 0xFF, 0xFF, 0xFF,
		0x0F, 0x82, 0xED, 0xFF, 0xFF, 0xFF,
		0x0F, 0x86, 0xE7, 0xFF, 0xFF, 0xFF,
		0x0F, 0x8C, 0xE1, 0xFF, 0xFF, 0xFF,
		0x0F, 0x8E, 0xDB, 0xFF, 0xFF, 0xFF,
		0x0F, 0x8F, 0xD5, 0xFF, 0xFF, 0xFF,
		0x0F, 0x8D, 0xCF, 0xFF, 0xFF, 0xFF,
	}
	if !bytes.Equal(out, want) {
		t.Fatalf("branch bytes = %#x, want %#x", out, want)
	}
}
