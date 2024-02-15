package vm

import "charm/source/token"

type functionAndReturnType struct {
	f func(cp *Compiler, vm *Vm, tok *token.Token, dest uint32, args []uint32)
	t alternateType
}

var BUILTINS = map[string]functionAndReturnType{
	"add_floats":        {(*Compiler).btAddFloats, simpleList(FLOAT)},
	"add_integers":      {(*Compiler).btAddIntegers, simpleList(INT)},
	"add_strings":       {(*Compiler).btAddStrings, simpleList(STRING)},
	"divide_floats":     {(*Compiler).btDivideFloats, alternateType{ERROR, FLOAT}},
	"divide_integers":   {(*Compiler).btDivideIntegers, alternateType{ERROR, INT}},
	"float_of_int":      {(*Compiler).btFloatOfInt, simpleList(FLOAT)},
	"float_of_string":   {(*Compiler).btFloatOfString, alternateType{ERROR, FLOAT}},
	"gt_floats":         {(*Compiler).btGtFloats, simpleList(BOOL)},
	"gte_floats":        {(*Compiler).btGteFloats, simpleList(BOOL)},
	"gt_ints":           {(*Compiler).btGtInts, simpleList(BOOL)},
	"gte_ints":          {(*Compiler).btGteInts, simpleList(BOOL)},
	"int_of_float":      {(*Compiler).btIntOfFloat, alternateType{INT}},
	"int_of_string":     {(*Compiler).btIntOfString, alternateType{ERROR, INT}},
	"len_string":        {(*Compiler).btLenString, simpleList(INT)},
	"literal":           {(*Compiler).btLiteral, simpleList(STRING)},
	"lt_floats":         {(*Compiler).btLtFloats, simpleList(BOOL)},
	"lte_floats":        {(*Compiler).btLteFloats, simpleList(BOOL)},
	"lt_ints":           {(*Compiler).btLtInts, simpleList(BOOL)},
	"lte_ints":          {(*Compiler).btLteInts, simpleList(BOOL)},
	"make_error":        {(*Compiler).btMakeError, simpleList(ERROR)},
	"modulo_integers":   {(*Compiler).btModuloIntegers, alternateType{ERROR, INT}},
	"multiply_floats":   {(*Compiler).btMultiplyFloats, simpleList(FLOAT)},
	"multiply_integers": {(*Compiler).btMultiplyIntegers, simpleList(INT)},
	"negate_float":      {(*Compiler).btNegateFloat, simpleList(FLOAT)},
	"negate_integer":    {(*Compiler).btNegateInteger, simpleList(INT)},
	"string":            {(*Compiler).btString, simpleList(STRING)},
	"subtract_floats":   {(*Compiler).btSubtractFloats, simpleList(FLOAT)},
	"subtract_integers": {(*Compiler).btSubtractIntegers, simpleList(INT)},
	"tuple_of_single?":  {(*Compiler).btTupleOfSingle, alternateType{finiteTupleType{}}},
	"tuple_of_tuple":    {(*Compiler).btTupleOfTuple, alternateType{finiteTupleType{}}},
	"type":              {(*Compiler).btType, simpleList(TYPE)},
	"type_of_tuple":     {(*Compiler).btTypeOfTuple, simpleList(TYPE)},
}

func (cp *Compiler) btAddFloats(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, addf, dest, args[0], args[2])
}

func (cp *Compiler) btAddIntegers(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, addi, dest, args[0], args[2])
}

func (cp *Compiler) btAddStrings(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, adds, dest, args[0], args[2])
}

func (cp *Compiler) btDivideFloats(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.reserve(vm, FLOAT, 0.0)
	cp.put(vm, equf, args[2], vm.that())
	cp.emit(vm, qtru, vm.that(), vm.codeTop()+3)
	cp.reserveError(vm, "built/div/float", tok, []any{})
	cp.emit(vm, asgm, dest, vm.that())
	cp.emit(vm, jmp, vm.codeTop()+2)
	cp.emit(vm, divf, dest, args[0], args[2])
}

func (cp *Compiler) btDivideIntegers(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.reserve(vm, INT, 0)
	cp.put(vm, equi, args[2], vm.that())
	cp.emit(vm, qtru, vm.that(), vm.codeTop()+3)
	cp.reserveError(vm, "built/div/int", tok, []any{})
	cp.emit(vm, asgm, dest, vm.that())
	cp.emit(vm, jmp, vm.codeTop()+2)
	cp.emit(vm, divi, dest, args[0], args[2])
}

func (cp *Compiler) btFloatOfInt(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, flti, dest, args[0])
}

func (cp *Compiler) btFloatOfString(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, flts, dest, args[0])
}

func (cp *Compiler) btGtFloats(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, gthf, dest, args[0], args[2])
}

func (cp *Compiler) btGteFloats(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, gtef, dest, args[0], args[2])
}

func (cp *Compiler) btGtInts(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, gthi, dest, args[0], args[2])
}

func (cp *Compiler) btGteInts(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, gtei, dest, args[0], args[2])
}

func (cp *Compiler) btIntOfFloat(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, intf, dest, args[0])
}

func (cp *Compiler) btIntOfString(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, ints, dest, args[0])
}

func (cp *Compiler) btLenString(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, lens, dest, args[0])
}

func (cp *Compiler) btLiteral(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, litx, dest, args[0])
}

func (cp *Compiler) btLtFloats(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, gthf, dest, args[2], args[0])
}

func (cp *Compiler) btLteFloats(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, gtef, dest, args[2], args[0])
}

func (cp *Compiler) btLtInts(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, gthi, dest, args[2], args[0])
}

func (cp *Compiler) btLteInts(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, gtei, dest, args[2], args[0])
}

func (cp *Compiler) btMakeError(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, mker, dest, args[0], cp.reserveToken(vm, tok))
}

func (cp *Compiler) btModuloIntegers(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.reserve(vm, INT, 0)
	cp.put(vm, equi, args[2], vm.that())
	cp.emit(vm, qtru, vm.that(), vm.codeTop()+3)
	cp.reserveError(vm, "built/mod", tok, []any{})
	cp.emit(vm, asgm, dest, vm.that())
	cp.emit(vm, jmp, vm.codeTop()+2)
	cp.emit(vm, modi, dest, args[0], args[2])
}

func (cp *Compiler) btMultiplyFloats(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, mulf, dest, args[0], args[2])
}

func (cp *Compiler) btMultiplyIntegers(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, muli, dest, args[0], args[2])
}

func (cp *Compiler) btNegateFloat(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, negf, dest, args[0])
}

func (cp *Compiler) btNegateInteger(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, negi, dest, args[0])
}

func (cp *Compiler) btSubtractFloats(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, subf, dest, args[0], args[2])
}

func (cp *Compiler) btString(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, strx, dest, args[0])
}

func (cp *Compiler) btSubtractIntegers(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, subi, dest, args[0], args[2])
}

func (cp *Compiler) btType(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, typx, dest, args[0])
}

func (cp *Compiler) btTupleOfSingle(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, cv1T, dest, args[0])
}

func (cp *Compiler) btTupleOfTuple(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, asgm, dest, args[0])
}

func (cp *Compiler) btTypeOfTuple(vm *Vm, tok *token.Token, dest uint32, args []uint32) {
	cp.emit(vm, asgm, dest, cp.tupleType)
}