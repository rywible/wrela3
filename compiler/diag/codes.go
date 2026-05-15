package diag

const (
	CLI0001  = "CLI0001"
	CLI0002  = "CLI0002"
	CLI0003  = "CLI0003"
	CLI0004  = "CLI0004"
	SRC0001  = "SRC0001"
	SRC0002  = "SRC0002"
	SRC0003  = "SRC0003"
	SRC0004  = "SRC0004"
	SRC0005  = "SRC0005"
	PAR0001  = "PAR0001"
	PAR0002  = "PAR0002"
	SEM0001  = "SEM0001"
	SEM0002  = "SEM0002"
	SEM0003  = "SEM0003"
	SEM0004  = "SEM0004"
	SEM0005  = "SEM0005"
	SEM0006  = "SEM0006"
	SEM0007  = "SEM0007"
	SEM0008  = "SEM0008"
	SEM0009  = "SEM0009"
	SEM0010  = "SEM0010"
	SEM0011  = "SEM0011"
	SEM0012  = "SEM0012"
	SEM0013  = "SEM0013"
	SEM0014  = "SEM0014"
	SEM0015  = "SEM0015"
	SEM0016  = "SEM0016"
	SEM0017  = "SEM0017"
	SEM0018  = "SEM0018"
	SEM0019  = "SEM0019"
	SEM0020  = "SEM0020"
	SEM0021  = "SEM0021" // invalid arena receiver
	SEM0022  = "SEM0022" // frame expression must be arena.frame(length = ...)
	SEM0023  = "SEM0023" // frame length must be U64
	SEM0024  = "SEM0024" // frame lifetime escapes with block value
	SEM0025  = "SEM0025" // frame value stored in longer-lived state
	SEM0026  = "SEM0026" // place argument must be constructor expression
	SEM0027  = "SEM0027" // reserve length and align must be U64
	SEM0028  = "SEM0028" // raw physical byte authority construction is not allowed here
	SEM0029  = "SEM0029" // ArenaFrame cannot be constructed directly
	SEM0030  = "SEM0030" // cache lookup must copy into frame
	SEM0031  = "SEM0031" // cache entry cannot escape lookup scope
	SEM0032  = "SEM0032" // asm raw memory access requires edge-capability module
	ASM0001  = "ASM0001"
	ASM0002  = "ASM0002"
	ASM0003  = "ASM0003"
	CG0001   = "CG0001"
	PE0001   = "PE0001"
	QEMU0001 = "QEMU0001"
	INT0001  = "INT0001"
)
