package parser

import (
	"charm/ast"
	"charm/object"
	"charm/signature"
	"charm/token"
	"fmt"
)


type FunctionTable map[string] []ast.Function

func (ft FunctionTable) Add(T TypeSystem, keyword string, f ast.Function) (ok bool) {
	if functions, ok := ft[keyword]; ok {
		functions, ok = AddInOrder(T, functions, f)
		ft[keyword] = functions
		return ok
	}
	ft[keyword] = []ast.Function{f}
	return true
} 

func (p *Parser) FindFunction(keyword string, parameters []object.Object, fromRepl bool) (returnFunction ast.Function, returnError string) {
	if _, ok := p.FunctionTable[keyword]; !ok {
		return returnFunction, "keyword"
	}
	for _, f := range p.FunctionTable[keyword] {
		if p.ParamsFitSig(f.Sig, parameters) && !(fromRepl && f.Private) {
			return f, ""
		}	
	}	
	return returnFunction, "sig"
}

func (p *Parser) ParamsFitSig(s signature.Signature, parameters []object.Object) bool {
	
	if len(parameters) == 0 && len(s) == 0 { return true } 
	if len(parameters) > len(s) &&
	    ((len(s) == 0) || !(s[len(s) - 1].VarType == "tuple" || s[len(s) - 1].VarType == "any")) {
		return false
	}
	if len(parameters) < len(s) && 
	    ! (len(s) == len(parameters) + 1 && (s[len(parameters)].VarType == "tuple") || s[len(parameters)].VarType == "any")   {
		return false
	}
	for i, param := range parameters {
		if i == len(s) - 1 && (s[i].VarType == "tuple" || s[i].VarType == "any") {
			return true
		}
		if s[i].VarType != object.TrueType(param) &&
		!p.TypeSystem.PointsTo(object.TrueType(param), s[i].VarType) &&
				s[i].VarType != "varname" {
			return false
		}
		if param.Type() == object.BLING_OBJ &&
			s[i].VarType == "bling" &&
			param.(*object.Bling).Value != s[i].VarName {
				return false
		}
		if i == len(parameters) - 1 {
			return true
		}	
	}

	if (len(s) == len(parameters) + 1 && 
	    (s[len(parameters)].VarType == "tuple") || s[len(parameters)].VarType == "any") { return true }

	return false
}

func UpdateEnvironment(sig signature.Signature, params []object.Object, env *object.Environment) *object.Environment { 
	if len(sig) == 0 {
		return env
	}
	sigPos := 0;
	tupleAccumulator := []object.Object{}
	
	for paramPos := 0; paramPos < len(params); paramPos++ {
		if sig[sigPos].VarType == object.TUPLE_OBJ {
			if params[paramPos].Type() == object.BLING_OBJ {
				env.Set(sig[sigPos].VarName, &object.Tuple{Elements: tupleAccumulator})
				tupleAccumulator = []object.Object{}
				sigPos = sigPos + 2
				continue
			}
			if paramPos == len(params) - 1 {
				tupleAccumulator = append(tupleAccumulator, params[paramPos])
				env.Set(sig[sigPos].VarName, &object.Tuple{Elements: tupleAccumulator})
				break
			}
			tupleAccumulator = append(tupleAccumulator, params[paramPos])
			continue
		}	
		if sig[sigPos].VarType == "varname" {
			obj, ok := env.Get(sig[sigPos].VarName)
			if !ok {
				fmt.Println("Oops 1")
				return env
			}
			if obj.Type() != object.CODE_OBJ {
				fmt.Println("Oops 2")
				return env
			}
			if obj.(*object.Code).Value.GetToken().Type != token.IDENT {
				fmt.Println("Oops 3")
				return env
			}
			env.Set(obj.(*object.Code).Value.GetToken().Literal, params[paramPos])
		} else {
			env.Set(sig[sigPos].VarName, params[paramPos])
		}
		sigPos++
	}
	return env
}