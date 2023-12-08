package vm

import (
	"charm/source/ast"
	"charm/source/parser"
)

type Compiler struct {
	p           *parser.Parser
	vm          *Vm
	memTop      uint32
	constantTop uint32
	codeTop     uint32
}

const MAX_32 = 4294967295

func NewCompiler(p *parser.Parser) *Compiler {
	return &Compiler{
		p:           p,
		vm:          blankVm(),
		memTop:      1,
		constantTop: 2,
	}
}

func (cp *Compiler) Run() {
	cp.vm.Run(0)
}

func (cp *Compiler) Compile(source, sourcecode string) {
	print("\nCompiling :\n\n")
	cp.vm = blankVm()
	node := cp.p.ParseLine(source, sourcecode)
	cp.compileNode(node, true)
	cp.vm.mem = make([]Value, cp.memTop)
}

// When we want to return froma block of bytecode we want the result to be stashed in m0,
// and then we want a rtn statement. Whereas otherwise we mant the result appended to the end
// of memory.
//
// To know when we have in fact finished, we pass the 'dest' flag along with the node to be
// compiled. This tells it to use the first of these options. So 'dest' is set when we run 'main', or
// a line from the REPL, or whatever. We usually then want to unset it when making a recusive call to
// compileNode, except in the case where the node we're compiling is an 'if' branch, where we can and
// should induce an early return by this means.
//
// We have two different ways of emiting an opcode:
//
//   (1) 'emit' is for opcodes without a destination memory address at all, e.g. jumps, returns,
//       flow-of-control generally.
//
//   (2) 'put' takes the value of 'dest' as its second parameter (after the opcode) and emits code
//       accordingly.
//

func (cp *Compiler) compileNode(node ast.Node, dest bool) typeList {
	// var offset uint32
	// if dest {
	// 	offset = 1
	// }
	switch node := node.(type) {
	case *ast.IntegerLiteral:
		cp.put(asgc, dest, cp.constantTop)
		cp.addConstant(INT, node.Value)
		return []valType{&simpleType{t: INT}}
	case *ast.StringLiteral:
		cp.put(asgc, dest, cp.constantTop)
		cp.addConstant(STRING, node.Value)
		return []valType{&simpleType{t: STRING}}
	case *ast.BooleanLiteral:
		cp.put(asgc, dest, cp.constantTop)
		cp.addConstant(BOOL, node.Value)
		return []valType{&simpleType{t: BOOL}}
	case *ast.FloatLiteral:
		cp.put(asgc, dest, cp.constantTop)
		cp.addConstant(FLOAT, node.Value)
		return []valType{&simpleType{t: FLOAT}}
	case *ast.InfixExpression:
		if node.Operator == "==" {
			lTypes := cp.compileNode(node.Args[0], false)
			if lTypes.only(ERROR) {
				cp.p.Throw("comp/eq/err/a", node.Token)
				return []valType{&simpleType{t: TYPE_ERROR}}
			}
			leftRg := cp.memTop - 1
			rTypes := cp.compileNode(node.Args[2], false)
			if rTypes.only(ERROR) {
				cp.p.Throw("comp/eq/err/b", node.Token)
				return []valType{&simpleType{t: TYPE_ERROR}}
			}
			rightRg := cp.memTop - 1
			oL := lTypes.intersect(rTypes)
			if oL.only(ERROR) {
				cp.p.Throw("comp/eq/err/c", node.Token)
				return []valType{&simpleType{t: TYPE_ERROR}}
			}
			if len(oL) == 0 {
				cp.p.Throw("comp/eq/types", node.Token)
				return []valType{&simpleType{t: TYPE_ERROR}}
			}
			if len(oL) == 1 && len(lTypes) == 1 && len(rTypes) == 1 {
				switch oL[0].concreteType() {
				case INT:
					cp.put(equi, dest, leftRg, rightRg)
				case STRING:
					cp.put(equs, dest, leftRg, rightRg)
				case BOOL:
					cp.put(equb, dest, leftRg, rightRg)
				case FLOAT:
					cp.put(equf, dest, leftRg, rightRg)
				default:
					panic("Unimplemented comparison type.")
				}
				return []valType{&simpleType{t: BOOL}}
			} else {
				panic("Haven't implemented this bit because of having no way to test it at this point.")
			}
		} else {
			panic("Unimplemented infix.")
		}
	case *ast.LazyInfixExpression:
		if node.Operator == "or" {
			lTypes := cp.compileNode(node.Left, false)
			if !lTypes.contains(BOOL) {
				cp.p.Throw("comp/or/bool/left", node.Token)
				return []valType{&simpleType{t: TYPE_ERROR}}
			}
			leftRg := cp.memTop - 1
			cp.emit(qtrue, leftRg)
			cp.emit(jmp, cp.codeTop+2)
			backtrack := cp.codeTop
			cp.emit(jmp, MAX_32)
			rTypes := cp.compileNode(node.Right, false)
			if !rTypes.contains(BOOL) {
				cp.p.Throw("comp/or/bool/right", node.Token)
				return []valType{&simpleType{t: TYPE_ERROR}}
			}
			rightRg := cp.memTop - 1
			cp.vm.code[backtrack].args[0] = cp.codeTop
			cp.put(orb, dest, leftRg, rightRg)
			return []valType{&simpleType{t: BOOL}}
		}
		panic("Unimplemented lazy infix.")
	case *ast.PrefixExpression:
		if node.Operator == "not" {
			allTypes := cp.compileNode(node.Args[0], false)
			if allTypes.only(BOOL) {
				cp.put(notb, dest, cp.memTop-1)
				return []valType{&simpleType{t: BOOL}}
			}
			if !allTypes.contains(BOOL) {
				cp.p.Throw("comp/not/bool", node.Token)
				return []valType{&simpleType{t: TYPE_ERROR}}
			}
			panic("Haven't implemented this bit because of having no way to test it at this point.")
		} else {
			panic("Unimplemented prefix.")
		}
	default:
		panic("Unimplemented node type.")
	}
}

func (cp *Compiler) addConstant(t uint32, v any) {
	cp.vm.con = append(cp.vm.con, Value{T: t, V: v})
	cp.constantTop++
}

func (cp *Compiler) put(opcode opcode, dest bool, args ...uint32) {
	if dest {
		args = append([]uint32{0}, args...)
		cp.emit(opcode, args...)
		cp.emit(ret)
	} else {
		args = append([]uint32{cp.memTop}, args...)
		cp.emit(opcode, args...)
		cp.memTop++
	}
}

func (cp *Compiler) emit(opcode opcode, args ...uint32) {
	cp.vm.code = append(cp.vm.code, makeOp(opcode, args...))
	cp.codeTop++
	println(describe(cp.vm.code[len(cp.vm.code)-1]))
}
