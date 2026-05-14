package asm

type Operand interface {
	isOperand()
}

type Instruction struct {
	Mnemonic string
	Operands []Operand
	Label    string
}

type RegOperand struct {
	Reg Reg
}

func (RegOperand) isOperand() {}

type ImmOperand struct {
	Value int64
}

func (ImmOperand) isOperand() {}

type LabelRef struct {
	Name string
}

func (LabelRef) isOperand() {}

type FieldOperand struct {
	Base  string
	Field string
}

func (FieldOperand) isOperand() {}

type ParamOperand struct {
	Name string
}

func (ParamOperand) isOperand() {}

type MemOperand struct {
	Base  Reg
	Disp  int64
	Width int
}

func (MemOperand) isOperand() {}
