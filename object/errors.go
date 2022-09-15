package object

import (
	"strconv"

	"charm/text"
	"charm/token"
)

// This handles the creation of errors, their messages, and their explanations, and the maintainance
// of the lists of syntax errors generated by the lexer, parser, and initializer.
//
// The logic to create a specific error given its error identifier and the relevant parameters
// is in errorfile.go.

type Errors = []*Error

type ErrorCreator struct {
	Message     func(tok token.Token, args ...any) string
	Explanation func(errors Errors, pos int, tok token.Token, args ...any) string
}

func Emph(s string) string {
	return text.Emph(s)
}

func Put(message string, tok token.Token, ers Errors) []*Error {
	for _, v := range ers {
		if v.Token.Line == tok.Line && v.Token.ChStart == tok.ChStart {
			return ers
		}
	}
	ers = append(ers, &Error{Message: message, Token: tok})
	return ers
}

func GetList(ers Errors) string {
	result := "\n"
	for i, v := range ers {
		result = result + "[" + strconv.Itoa(i) + "] " + v.Inspect(ViewStdOut) + "\n"
	}
	return result + "\n"
}

func AddErr(err *Error, ers Errors, tok token.Token) Errors {
	for _, v := range ers {
		if v.Token.Line == tok.Line && v.Token.ChStart == tok.ChStart {
			return ers
		}
	}
	ers = append(ers, err)
	return ers
}

func Throw(errorId string, ers Errors, tok token.Token, args ...any) Errors {
	ers = AddErr(CreateErr(errorId, tok, args...), ers, tok)
	return ers
}

func CreateErr(errorId string, tok token.Token, args ...any) *Error {
	errorCreator, ok := ErrorCreatorMap[errorId]
	if !ok {
		return CreateErr("err/misdirect", tok, errorId)
	}
	return &Error{ErrorId: errorId, Message: errorCreator.Message(tok, args...),
		Token: tok, Info: args}
}

// Merges two lists of errors in order of occurrence, on the assumption that they
// are each already ordered.
func MergeErrors(a, b Errors) Errors {
	var result Errors
	for i, j := 0, 0; (i < len(a)) || (j < len(b)); {

		if i == len(a) {
			result = append(result, b[j])
			j++
			continue
		}

		if j == len(b) {
			result = append(result, a[i])
			i++
			continue
		}

		if a[i].Token.Line == b[j].Token.Line && a[i].Token.ChStart == b[j].Token.ChStart {
			result = append(result, a[i])
			i++
			j++ // By policy we don't report two errors in the same place
			continue
		}
		if a[i].Token.Line < b[j].Token.Line ||
			a[i].Token.Line == b[j].Token.Line && a[i].Token.ChStart < b[j].Token.ChStart {
			result = append(result, a[i])
			i++
			continue
		}
		result = append(result, b[j])
		j++
	}
	return result
}
