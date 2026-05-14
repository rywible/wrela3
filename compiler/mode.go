package compiler

import "fmt"

type Mode string

const (
	ModeDev     Mode = "dev"
	ModeRelease Mode = "release"
)

func ParseMode(raw string) (Mode, error) {
	switch raw {
	case string(ModeDev):
		return ModeDev, nil
	case string(ModeRelease):
		return ModeRelease, nil
	default:
		return "", NewCodeError("CLI0001", fmt.Sprintf("invalid mode %q; expected dev or release", raw))
	}
}
