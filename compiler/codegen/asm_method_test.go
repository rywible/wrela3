package codegen

import (
	"bytes"
	"testing"

	"github.com/ryanwible/wrela3/compiler/ir"
)

func TestCompileAsmMethodWrite8ContainsOutDxAl(t *testing.T) {
	method := ir.AsmMethod{
		Symbol:       "SerialWriterRegisters.write8",
		ReceiverType: "SerialWriterRegisters",
		Params: []ir.Value{
			&ir.Param{Symbol: "self", Type: ir.Type{Name: "SerialWriterRegisters"}},
			&ir.Param{Symbol: "value", Type: ir.Type{Name: "U8"}},
		},
		Body: "mov dx, self.port_base; mov al, value; out dx, al; ret",
		ReceiverFieldOffsets: map[string]int{
			"port_base": 0,
		},
		ReceiverFieldWidths: map[string]int{
			"port_base": 16,
		},
	}

	image, diags := Compile(&ir.Program{
		AsmMethods: []ir.AsmMethod{method},
	})
	if len(diags) != 0 {
		t.Fatalf("Compile() diagnostics = %#v", diags)
	}
	if !bytes.Contains(image.Sections[0].Data, []byte{0xEE}) {
		t.Fatalf("asm output should contain out dx, al (EE): %#x", image.Sections[0].Data)
	}
}
