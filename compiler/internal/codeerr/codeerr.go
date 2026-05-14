package codeerr

type CodeError struct {
	Code    string
	Message string
}

func New(code, message string) CodeError {
	return CodeError{Code: code, Message: message}
}

func (e CodeError) Error() string {
	return e.Code + ": " + e.Message
}
