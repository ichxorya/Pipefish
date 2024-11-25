package service

import (
	"database/sql"
	"fmt"
	"html/template"
	"os"
	"strconv"
	"strings"

	"src.elv.sh/pkg/persistent/vector"

	"pipefish/source/dtypes"
	"pipefish/source/parser"
	"pipefish/source/report"
	"pipefish/source/settings"
	"pipefish/source/text"
	"pipefish/source/token"
	"pipefish/source/values"
)

type Vm struct {
	// Temporary state: things we change at runtime.
	Mem            []values.Value
	Code           []*Operation
	callstack      []uint32
	recursionStack []recursionData
	logging        bool
	LiveTracking   []TrackingData // "Live" tracking data in which the uint32s in the permanent tracking data have been replaced by the corresponding memory registers.

	// Permanent state: things established at compile time.

	concreteTypes              []typeInformation
	Labels                     []string // Array from the number of a field label to its name.
	Tokens                     []*token.Token
	LambdaFactories            []*LambdaFactory
	SnippetFactories           []*SnippetFactory
	GoFns                      []GoFn
	tracking                   []TrackingData // Data needed by the 'trak' opcode to produce the live tracking data.
	IoHandle                   IoHandler
	Database                   *sql.DB
	AbstractTypes              []values.AbstractTypeInfo
	OwnService                 *Service              // The service that owns the vm. Much of the useful metadata will be in the compiler attached to the service.
	HubServices                map[string]*Service   // The same map that the hub has.
	ExternalCallHandlers       []externalCallHandler // The services declared external, whether on the same hub or a different one.
	typeNumberOfUnwrappedError values.ValueType      // What it says. When we unwrap an 'error' to an 'Error' struct, the vm needs to know the number of the struct.
	Stringify                  *CpFunc

	// Strictly speaking this field should not be here since it is only used at compile time. However it refers to something which is *not* naturally shared by the parser, uberparser, vmm, compiler, etc, so what to do?
	codeGeneratingTypes dtypes.Set[values.ValueType]
	// These also may not belong in the VM, but it is shared state among all the compilers for the various modules, and there's nothing else
	// tying them together but the vm. TODO --- make some common bindle for them so I'm not doing this.
	sharedTypenameToTypeList map[string]AlternateType
	AnyTypeScheme            AlternateType
	AnyTuple                 AlternateType
	LabelIsPrivate           []bool
	IsRangeable              AlternateType
	FieldLabelsInMem         map[string]uint32 // We have these so that we can introduce a label by putting Asgm location of label and then transitively squishing.
}

func (vm *Vm) AddTypeNumberToSharedAlternateTypes(typeNo values.ValueType, abTypes ...string) {
	abTypes = append(abTypes, "any")
	for _, ty := range abTypes {
		vm.sharedTypenameToTypeList[ty] = vm.sharedTypenameToTypeList[ty].Union(altType(typeNo))
		vm.sharedTypenameToTypeList[ty+"?"] = vm.sharedTypenameToTypeList[ty+"?"].Union(altType(typeNo))
	}
	vm.AnyTuple = AlternateType{TypedTupleType{vm.sharedTypenameToTypeList["any?"]}}
	vm.AnyTypeScheme = vm.AnyTypeScheme.Union(altType(typeNo))
	vm.AnyTypeScheme[len(vm.AnyTypeScheme)-1] = vm.AnyTuple
	vm.sharedTypenameToTypeList["tuple"] = vm.AnyTuple
}

// This takes a snapshot of how much code, memory locations, etc, have been added to the respective lists at a given
// point. Then to roll back the vm, we can call the rollback function (below) on the state returned by getState.
type vmState struct {
	mem              int
	code             int
	tokens           int
	lambdaFactories  int
	snippetFactories int
}

type GoFn struct {
	Code   func(args ...any) any
	GoToPf func(v any) (uint32, []any, bool)
	GoToPfEnum func(v any) (uint32, int)
	PfToGo func(T uint32, args []any) any
	Raw    []bool
}

type Lambda struct {
	capturesStart  uint32
	capturesEnd    uint32
	parametersEnd  uint32
	resultLocation uint32
	addressToCall  uint32
	captures       []values.Value
	sig            []values.AbstractType // To represent the call signature. Unusual in that the types of the AbstractType will be nil in case the type is 'any?'
	tok            *token.Token
}

// All the information we need to make a lambda at a particular point in the code.
type LambdaFactory struct {
	Model            *Lambda  // Copy this to make the lambda.
	CaptureLocations []uint32 // Then these are the location of the values we're closing over, so we copy them into the lambda.
}

// All the information we need to make a snippet at a particular point in the code.
type SnippetFactory struct {
	snippetType  values.ValueType // The type of the snippet, adoy.
	sourceString string           // The plain text of the snippet before processing.
	bindle       *SnippetBindle   // Points to the structure defined below.
}

// A grouping of all the things a snippet from a given snippet factory have in common.
type SnippetBindle struct {
	compiledSnippetKind compiledSnippetKind // An enum type saying whether it's uncompiled, an external service, SQL, or HTML.
	codeLoc             uint32              // Where to find the code to compute the object string and the values.
	objectStringLoc     uint32              // Where to find the object string.
	valueLocs           []uint32            // The locations where we put the computed values to inject into SQL or HTML snippets.
}

type recursionData struct {
	mems []values.Value
	loc  uint32
}

// Used for injecting data into HTML.
type HTMLInjector struct {
	Data []any
}

// These inhabit the first few memory addresses of the VM.
var CONSTANTS = []values.Value{values.UNDEF, values.FALSE, values.TRUE, values.U_OBJ, values.ONE, values.BLNG, values.OK, values.EMPTY}

// Type names in upper case are things the user should never see.
var nativeTypeNames = []string{"UNDEFINED VALUE", "INT ARRAY", "SNIPPET DATA", "THUNK",
	"CREATED LOCAL CONSTANT", "COMPILE TIME ERROR", "BLING", "UNSATISFIED CONDITIONAL", "REFERENCE VARIABLE",
	"ITERATOR", "ok", "tuple", "error", "null", "int", "bool", "string", "rune", "float", "type", "func",
	"pair", "list", "map", "set", "label"}

func BlankVm(db *sql.DB, hubServices map[string]*Service) *Vm {
	vm := &Vm{Mem: make([]values.Value, len(CONSTANTS)), Database: db, HubServices: hubServices,
		logging: true, IoHandle: MakeStandardIoHandler(os.Stdout),
		codeGeneratingTypes: (make(dtypes.Set[values.ValueType])).Add(values.FUNC),
		sharedTypenameToTypeList: map[string]AlternateType{
			"any":  AltType(values.INT, values.BOOL, values.STRING, values.RUNE, values.TYPE, values.FUNC, values.PAIR, values.LIST, values.MAP, values.SET, values.LABEL),
			"any?": AltType(values.NULL, values.INT, values.BOOL, values.STRING, values.RUNE, values.FLOAT, values.TYPE, values.FUNC, values.PAIR, values.LIST, values.MAP, values.SET, values.LABEL),
		},
		AnyTypeScheme: AlternateType{},
		AnyTuple:      AlternateType{},
	}
	for _, name := range parser.AbstractTypesOtherThanSingle {
		vm.sharedTypenameToTypeList[name] = AltType()
		vm.sharedTypenameToTypeList[name+"?"] = AltType(values.NULL)
	}
	for name, ty := range parser.ClonableTypes {
		vm.sharedTypenameToTypeList[name+"like"] = AltType(ty)
		vm.sharedTypenameToTypeList[name+"like?"] = AltType(values.NULL, ty)
	}
	copy(vm.Mem, CONSTANTS)
	for _, name := range nativeTypeNames {
		vm.concreteTypes = append(vm.concreteTypes, builtinType{name: name})
	}
	vm.typeNumberOfUnwrappedError = DUMMY
	vm.Mem = append(vm.Mem, values.Value{values.SUCCESSFUL_VALUE, nil}) // TODO --- why?
	vm.AnyTuple = AlternateType{TypedTupleType{vm.sharedTypenameToTypeList["any?"]}}
	vm.AnyTypeScheme = make(AlternateType, len(vm.sharedTypenameToTypeList["any?"]), 1+len(vm.sharedTypenameToTypeList["any?"]))
	copy(vm.AnyTypeScheme, vm.sharedTypenameToTypeList["any?"])
	vm.AnyTypeScheme = vm.AnyTypeScheme.Union(vm.AnyTuple)
	vm.sharedTypenameToTypeList["tuple"] = vm.AnyTuple
	vm.IsRangeable = altType(values.TUPLE, values.STRING, values.TYPE, values.PAIR, values.LIST, values.MAP, values.SET)
	vm.FieldLabelsInMem = make(map[string]uint32)
	return vm
}

func (vm *Vm) Run(loc uint32) {
	if settings.SHOW_RUNTIME {
		println()
	}
	stackHeight := len(vm.callstack)
loop:
	for {
		if settings.SHOW_RUNTIME {
			println(text.GREEN + vm.DescribeCode(loc) + text.RESET)
		}
		if settings.SHOW_RUNTIME_VALUES {
			print(vm.DescribeOperandValues(loc))
		}
		args := vm.Code[loc].Args
	Switch:
		switch vm.Code[loc].Opcode {
		case Addf:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(float64) + vm.Mem[args[2]].V.(float64)}
		case Addi:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(int) + vm.Mem[args[2]].V.(int)}
		case AddL:
			result := vm.Mem[args[1]].V.(vector.Vector)
			rhs := vm.Mem[args[2]].V.(vector.Vector)
			for i := 0; ; i++ {
				el, ok := rhs.Index(i)
				if !ok {
					break
				}
				result = result.Conj(el)
			}
			vm.Mem[args[0]] = values.Value{values.LIST, result}
		case AddS:
			result := vm.Mem[args[1]].V.(values.Set)
			result.Union(vm.Mem[args[2]].V.(values.Set))
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, result}
		case Adds:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(string) + vm.Mem[args[2]].V.(string)}
		case Adrr:
			vm.Mem[args[0]] = values.Value{values.STRING, string(vm.Mem[args[1]].V.(rune)) + string(vm.Mem[args[2]].V.(rune))}
		case Adrs:
			vm.Mem[args[0]] = values.Value{values.STRING, string(vm.Mem[args[1]].V.(rune)) + vm.Mem[args[2]].V.(string)}
		case Adsr:
			vm.Mem[args[0]] = values.Value{values.STRING, vm.Mem[args[1]].V.(string) + string(vm.Mem[args[2]].V.(rune))}
		case Adtk:
			vm.Mem[args[0]] = vm.Mem[args[1]]
			vm.Mem[args[0]].V.(*report.Error).AddToTrace(vm.Tokens[args[2]])
		case Andb:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(bool) && vm.Mem[args[2]].V.(bool)}
		case Aref:
			vm.Mem[vm.Mem[args[0]].V.(uint32)] = vm.Mem[args[1]]
		case Asgm:
			vm.Mem[args[0]] = vm.Mem[args[1]]
		case Call:
			paramNumber := args[1]
			argNumber := 3
			for paramNumber < args[2] {
				v := vm.Mem[args[argNumber]]
				if v.T == values.TUPLE {
					tup := v.V.([]values.Value)
					for ix := 0; ix < len(tup); ix++ {
						vm.Mem[paramNumber] = tup[ix]
						paramNumber++
					}
					argNumber++
				} else {
					vm.Mem[paramNumber] = v
					paramNumber++
					argNumber++
				}
			}
			vm.callstack = append(vm.callstack, loc)
			loc = args[0]
			continue
		case CalT: // A more complicated version which can do vararg or tuple captures. TODO --- the chances of anything needing to do both are infinitessimally rare so maybe two simpler versions, one for varargs and one for tuples?
			paramNumber := args[1]
			argNumber := 3
			tupleOrVarargsData := vm.Mem[args[2]].V.([]uint32)
			var varargsTime bool
			for paramNumber < args[2] {
				torvIndex := paramNumber - args[1]
				if tupleOrVarargsData[torvIndex] == 1 && !varargsTime {
					vm.Mem[paramNumber] = values.Value{values.TUPLE, []values.Value{}}
					varargsTime = true
				}
				v := vm.Mem[args[argNumber]]
				if v.T == values.TUPLE && tupleOrVarargsData[torvIndex] != 2 { // Then we're exploding a tuple.
					tup := v.V.([]values.Value)
					if varargsTime { // We may be doing a varargs, in which case we suck the whole tuple up into the vararg.
						vararg := vm.Mem[paramNumber].V.([]values.Value)
						vm.Mem[paramNumber].V = append(vararg, tup...)
						paramNumber++
					} else { // Otherwise we need to explode it and put it into the parameters one at a time unless and untill we run out of them or we meet a varargs.
						for ix := 0; ix < len(tup); ix++ {
							if tupleOrVarargsData[paramNumber-args[1]] == 1 { // The vararg will slurp up what remains of the tuple.
								varargsTime = true
								vm.Mem[paramNumber] = values.Value{values.TUPLE, tup[ix:]}
								break
							}
							vm.Mem[paramNumber] = tup[ix]
							paramNumber++
						}
					}
					argNumber++
				} else { // Otherwise we're not exploding a tuple.
					if varargsTime {
						for (argNumber < len(args)) && vm.Mem[args[argNumber]].T != values.BLING {
							vararg := vm.Mem[paramNumber].V.([]values.Value)
							vm.Mem[paramNumber].V = append(vararg, vm.Mem[args[argNumber]])
							argNumber++
						}
						varargsTime = false
						paramNumber++
					} else {
						vm.Mem[paramNumber] = v
						paramNumber++
						argNumber++
					}
				}
			}
			vm.callstack = append(vm.callstack, loc)
			loc = args[0]
			continue
		case Cast:
			vm.Mem[args[0]] = values.Value{values.ValueType(args[2]), vm.Mem[args[1]].V}
		case Casx:
			castToAbstract := vm.Mem[args[2]].V.(values.AbstractType)
			if len(castToAbstract.Types) != 1 {
				vm.Mem[args[0]] = vm.makeError("vm/cast/a", args[3], args[1], args[2])
				break Switch
			}
			targetType := castToAbstract.Types[0]
			currentType := vm.Mem[args[1]].T
			if targetType == currentType {
				vm.Mem[args[0]] = vm.Mem[args[1]]
				break Switch
			}
			if cloneInfoForCurrentType, ok := vm.concreteTypes[currentType].(cloneType); ok {
				if cloneInfoForCurrentType.parent == targetType {
					vm.Mem[args[0]] = values.Value{targetType, vm.Mem[args[1]].V}
					break Switch
				}
				if cloneInfoForTargetType, ok := vm.concreteTypes[currentType].(cloneType); ok && cloneInfoForTargetType.parent == cloneInfoForCurrentType.parent {
					vm.Mem[args[0]] = values.Value{targetType, vm.Mem[args[1]].V}
					break Switch
				}
			}
			// Otherwise by elimination the current type is the parent and the target type is a clone, or we have an error.
			if cloneInfoForTargetType, ok := vm.concreteTypes[targetType].(cloneType); ok && cloneInfoForTargetType.parent == currentType {
				vm.Mem[args[0]] = values.Value{targetType, vm.Mem[args[1]].V}
				break Switch
			}
			vm.Mem[args[0]] = vm.makeError("vm/cast/b", args[3], args[1], args[2])
		case Cc11:
			vm.Mem[args[0]] = values.Value{values.TUPLE, []values.Value{vm.Mem[args[1]], vm.Mem[args[2]]}}
		case Cc1T:
			vm.Mem[args[0]] = values.Value{values.TUPLE, append([]values.Value{vm.Mem[args[1]]}, vm.Mem[args[2]].V.([]values.Value)...)}
		case CcT1:
			vm.Mem[args[0]] = values.Value{values.TUPLE, append(vm.Mem[args[1]].V.([]values.Value), vm.Mem[args[2]])}
		case CcTT:
			vm.Mem[args[0]] = values.Value{values.TUPLE, append(vm.Mem[args[1]].V.([]values.Value), vm.Mem[args[2]])}
		case Ccxx:
			if vm.Mem[args[1]].T == values.TUPLE {
				if vm.Mem[args[2]].T == values.TUPLE {
					vm.Mem[args[0]] = values.Value{values.TUPLE, append(vm.Mem[args[1]].V.([]values.Value), vm.Mem[args[2]])}
				} else {
					vm.Mem[args[0]] = values.Value{values.TUPLE, append(vm.Mem[args[1]].V.([]values.Value), vm.Mem[args[2]])}
				}
			} else {
				if vm.Mem[args[2]].T == values.TUPLE {
					vm.Mem[args[0]] = values.Value{values.TUPLE, append([]values.Value{vm.Mem[args[1]]}, vm.Mem[args[2]].V.([]values.Value)...)}
				} else {
					vm.Mem[args[0]] = values.Value{values.TUPLE, []values.Value{vm.Mem[args[1]], vm.Mem[args[2]]}}
				}
			}
		case Cpnt:
			vm.Mem[args[0]] = values.Value{values.INT, int(vm.Mem[args[1]].V.(rune))}
		case Cv1T:
			vm.Mem[args[0]] = values.Value{values.TUPLE, []values.Value{vm.Mem[args[1]]}}
		case CvTT:
			slice := make([]values.Value, len(args)-1)
			for i := 0; i < len(slice); i++ {
				slice[i] = vm.Mem[args[i+1]]
			}
			vm.Mem[args[0]] = values.Value{values.TUPLE, slice}
		case Divf:
			divisor := vm.Mem[args[2]].V.(float64)
			if divisor == 0 {
				vm.Mem[args[0]] = vm.makeError("vm/div/float", args[3])
			} else {
				vm.Mem[args[0]] = values.Value{values.FLOAT, vm.Mem[args[1]].V.(float64) / divisor}
			}
		case Divi:
			divisor := vm.Mem[args[2]].V.(int)
			if divisor == 0 {
				vm.Mem[args[0]] = vm.makeError("vm/div/int", args[3])
			} else {
				vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(int) / vm.Mem[args[2]].V.(int)}
			}
		case Dvfi:
			divisor := vm.Mem[args[2]].V.(int)
			if divisor == 0 {
				vm.Mem[args[0]] = vm.makeError("vm/div/float", args[3])
			} else {
				vm.Mem[args[0]] = values.Value{values.FLOAT, vm.Mem[args[1]].V.(float64) / float64(divisor)}
			}
		case Dvif:
			divisor := vm.Mem[args[2]].V.(float64)
			if divisor == 0 {
				vm.Mem[args[0]] = vm.makeError("vm/div/float", args[3])
			} else {
				vm.Mem[args[0]] = values.Value{values.FLOAT, float64(vm.Mem[args[1]].V.(int)) / divisor}
			}
		case Dofn:
			lhs := vm.Mem[args[1]].V.(Lambda)
			for i := 0; i < int(lhs.capturesEnd-lhs.capturesStart); i++ {
				vm.Mem[int(lhs.capturesStart)+i] = lhs.captures[i]
			}
			for i := 0; i < int(lhs.parametersEnd-lhs.capturesEnd); i++ {
				vm.Mem[int(lhs.capturesEnd)+i] = vm.Mem[args[2+i]]
			}
			success := true
			if lhs.sig != nil {
				for i, abType := range lhs.sig { // TODO --- as with other such cases there will be a threshold at which linear search becomes inferior to binary search and we should find out what it is.
					success = false
					if abType.Types == nil {
						success = true
						continue
					} else {
						for _, ty := range abType.Types {
							if ty == vm.Mem[int(lhs.capturesEnd)+i].T {
								success = true
								if vm.Mem[int(lhs.capturesEnd)+i].T == values.STRING && len(vm.Mem[int(lhs.capturesEnd)+i].V.(string)) > abType.Len() {
									success = false
								}
							}
						}
					}
					if !success {
						vm.Mem[args[0]] = values.Value{values.ERROR, report.CreateErr("vm/func/types", lhs.tok)}
						break
					}
				}
			}
			if success {
				vm.Run(lhs.addressToCall)
				vm.Mem[args[0]] = vm.Mem[lhs.resultLocation]
			}
		case Dref:
			vm.Mem[args[0]] = vm.Mem[vm.Mem[args[1]].V.(uint32)]
		case Equb:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(bool) == vm.Mem[args[2]].V.(bool)}
		case Equf:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(float64) == vm.Mem[args[2]].V.(float64)}
		case Equi:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(int) == vm.Mem[args[2]].V.(int)}
		case Equs:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(string) == vm.Mem[args[2]].V.(string)}
		case Equt:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(values.AbstractType).Equals(vm.Mem[args[2]].V.(values.AbstractType))}
		case Eqxx:
			if vm.Mem[args[1]].T != vm.Mem[args[2]].T {
				vm.Mem[args[0]] = vm.Mem[args[3]]
			} else {
				vm.Mem[args[0]] = values.Value{values.BOOL, vm.equals(vm.Mem[args[1]], vm.Mem[args[2]])}
			}
		case Extn:
			externalOrdinal := args[1]
			operatorType := args[2]
			remainingNamespace := vm.Mem[args[3]].V.(string)
			name := vm.Mem[args[4]].V.(string)
			argLocs := args[5:]
			var buf strings.Builder
			if operatorType == PREFIX {
				buf.WriteString(remainingNamespace)
				buf.WriteString(name)
			}
			buf.WriteString("(")
			for i, loc := range argLocs {
				serialize := vm.Literal(vm.Mem[loc])
				if vm.Mem[loc].T == values.BLING && serialize == name {
					buf.WriteString(") ")
					buf.WriteString(remainingNamespace)
					buf.WriteString(serialize)
					buf.WriteString(" (")
					continue
				}
				buf.WriteString(serialize)
				if vm.Mem[loc].T == values.BLING || i+1 == len(argLocs) || vm.Mem[argLocs[i+1]].T == values.BLING {
					buf.WriteString(" ")
				} else {
					buf.WriteString(", ")
				}
			}
			buf.WriteString(")")
			if operatorType == SUFFIX {
				buf.WriteString(remainingNamespace)
				buf.WriteString(name)
			}
			vm.Mem[args[0]] = vm.ExternalCallHandlers[externalOrdinal].evaluate(vm, buf.String())
		case Flti:
			vm.Mem[args[0]] = values.Value{values.FLOAT, float64(vm.Mem[args[1]].V.(int))}
		case Flts:
			i, err := strconv.ParseFloat(vm.Mem[args[1]].V.(string), 64)
			if err != nil {
				vm.Mem[args[0]] = values.Value{values.ERROR, DUMMY}
			} else {
				vm.Mem[args[0]] = values.Value{values.FLOAT, i}
			}
		case Gofn:
			F := vm.GoFns[args[1]]
			goTpl := make([]any, 0, len(args))
			for i, v := range args[2:] {
				el := vm.Mem[v]
				if i < len(F.Raw) && F.Raw[i] { // We may not have a Raw value for every argument, if we're receiving a tuple.
					goTpl = append(goTpl, el)
				} else {
					goTpl = append(goTpl, vm.pipefishToGo(el, F.PfToGo))
				}
			}
			vm.Mem[args[0]] = vm.goToPipefish(F.Code(goTpl...), F.GoToPf, F.GoToPfEnum)
		case Gtef:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(float64) >= vm.Mem[args[2]].V.(float64)}
		case Gtei:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(int) >= vm.Mem[args[2]].V.(int)}
		case Gthf:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(float64) > vm.Mem[args[2]].V.(float64)}
		case Gthi:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].V.(int) > vm.Mem[args[2]].V.(int)}
		case IdxL:
			vec := vm.Mem[args[1]].V.(vector.Vector)
			ix := vm.Mem[args[2]].V.(int)
			val, ok := vec.Index(ix)
			if !ok {
				vm.Mem[args[0]] = vm.makeError("vm/index/list", args[3], ix, vec.Len(), args[1], args[2])
			} else {
				vm.Mem[args[0]] = val.(values.Value)
			}
		case Idxp:
			pair := vm.Mem[args[1]].V.([]values.Value)
			ix := vm.Mem[args[2]].V.(int)
			ok := ix == 0 || ix == 1
			if ok {
				vm.Mem[args[0]] = pair[ix]
			} else {
				vm.Mem[args[0]] = vm.makeError("vm/index/pair", args[3])
			}
		case Idxs:
			str := vm.Mem[args[1]].V.(string)
			ix := vm.Mem[args[2]].V.(int)
			ok := 0 <= ix && ix < len(str)
			if ok {
				val := values.Value{values.RUNE, rune(str[ix])}
				vm.Mem[args[0]] = val
			} else {
				vm.Mem[args[0]] = vm.makeError("vm/index/string", args[3], ix, len(str), args[1], args[2])
			}
		case Idxt:
			typ := (vm.Mem[args[1]].V.(values.AbstractType)).Types[0]
			if !vm.concreteTypes[typ].isEnum() {
				vm.Mem[args[0]] = vm.makeError("vm/index/type/a", args[3], vm.DescribeType(typ, LITERAL))
				break
			}
			ix := vm.Mem[args[2]].V.(int)
			ok := 0 <= ix && ix < len(vm.concreteTypes[typ].(enumType).elementNames)
			if ok {
				vm.Mem[args[0]] = values.Value{typ, ix}
			} else {
				vm.Mem[args[0]] = vm.makeError("vm/index/type/b", args[3], vm.DescribeType(typ, LITERAL), ix)
			}
		case IdxT:
			tuple := vm.Mem[args[1]].V.([]values.Value)
			ix := vm.Mem[args[2]].V.(int)
			ok := 0 <= ix && ix < len(tuple)
			if ok {
				vm.Mem[args[0]] = tuple[ix]
			} else {
				vm.Mem[args[0]] = vm.makeError("vm/index/tuple", args[3], ix, len(tuple), args[1], args[2])
			}
		case Inpt:
			vm.Mem[args[0]] = values.Value{values.STRING, vm.IoHandle.InHandle.Get(vm.Mem[args[1]].V.([]values.Value)[0].V.(string))}
		case Inte:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(int)}
		case Intf:
			vm.Mem[args[0]] = values.Value{values.INT, int(vm.Mem[args[1]].V.(float64))}
		case Ints:
			i, err := strconv.Atoi(vm.Mem[args[1]].V.(string))
			if err != nil {
				vm.Mem[args[0]] = values.Value{values.ERROR, DUMMY}
			} else {
				vm.Mem[args[0]] = values.Value{values.INT, i}
			}
		case InxL:
			x := vm.Mem[args[1]]
			L := vm.Mem[args[2]].V.(vector.Vector)
			i := 0
			vm.Mem[args[0]] = values.Value{values.BOOL, false}
			for el, ok := L.Index(i); ok; {
				if x.T == el.(values.Value).T {
					if vm.equals(x, el.(values.Value)) {
						vm.Mem[args[0]] = values.Value{values.BOOL, true}
						break
					}
				}
				i++
				el, ok = L.Index(i)
			}
		case InxS:
			x := vm.Mem[args[1]]
			S := vm.Mem[args[2]].V.(values.Set)
			if S.Contains(x) {
				vm.Mem[args[0]] = values.Value{values.BOOL, true}
			} else {
				vm.Mem[args[0]] = values.Value{values.BOOL, false}
			}
		case InxT:
			x := vm.Mem[args[1]]
			T := vm.Mem[args[2]].V.([]values.Value)
			vm.Mem[args[0]] = values.Value{values.BOOL, false}
			for _, el := range T {
				if x.T == el.T {
					if vm.equals(x, el) {
						vm.Mem[args[0]] = values.Value{values.BOOL, true}
						break
					}
				}
			}
		case Inxt:
			vm.Mem[args[0]] = values.Value{values.BOOL, false}
			for _, t := range vm.Mem[args[2]].V.(values.AbstractType).Types {
				if vm.Mem[args[1]].T == t {
					vm.Mem[args[0]] = values.Value{values.BOOL, true}
				}
			}
		case Itgk:
			vm.Mem[args[0]] = vm.Mem[args[1]].V.(values.Iterator).GetKey()
		case Itkv:
			vm.Mem[args[0]], vm.Mem[args[1]] = vm.Mem[args[2]].V.(values.Iterator).GetKeyValuePair()
		case Itgv:
			vm.Mem[args[0]] = vm.Mem[args[1]].V.(values.Iterator).GetValue()
		case Itor:
			vm.Mem[args[0]] = values.Value{values.RUNE, rune(vm.Mem[args[1]].V.(int))}
		case IxTn:
			vm.Mem[args[0]] = vm.Mem[args[1]].V.([]values.Value)[args[2]]
		case IxXx:
			container := vm.Mem[args[1]]
			index := vm.Mem[args[2]]
			indexType := index.T
			if cloneInfo, ok := vm.concreteTypes[indexType].(cloneType); ok {
				indexType = cloneInfo.parent
			}
			if indexType == values.PAIR { // Then we're slicing.
				containerType := container.T
				if cloneInfo, ok := vm.concreteTypes[containerType].(cloneType); ok && cloneInfo.isSliceable {
					containerType = cloneInfo.parent
				}
				ix := vm.Mem[args[2]].V.([]values.Value)
				if ix[0].T != values.INT {
					vm.Mem[args[0]] = vm.makeError("vm/index/a", args[3], vm.DescribeType(ix[0].T, LITERAL))
					break Switch
				}
				if ix[1].T != values.INT {
					vm.Mem[args[0]] = vm.makeError("vm/index/b", args[3], vm.DescribeType(ix[1].T, LITERAL))
					break Switch
				}
				if ix[0].V.(int) < 0 {
					vm.Mem[args[0]] = vm.makeError("vm/index/c", args[3], ix[0].V.(int))
					break Switch
				}
				if ix[1].V.(int) < ix[0].V.(int) {
					vm.Mem[args[0]] = vm.makeError("vm/index/d", args[3], ix[0].V.(int), ix[1].V.(int))
					break Switch
				}
				// We switch on the type of the lhs.
				switch container.T {
				case values.LIST:
					vec := vm.Mem[args[1]].V.(vector.Vector)
					if ix[1].V.(int) > vec.Len() {
						vm.Mem[args[0]] = vm.makeError("vm/index/e", args[3], ix[1], vec.Len(), args[1], args[2])
						break Switch
					}
					vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vec.SubVector(ix[0].V.(int), ix[1].V.(int))}
				case values.STRING:
					str := container.V.(string)
					ix := index.V.([]values.Value)
					if ix[1].V.(int) > len(str) {
						vm.Mem[args[0]] = vm.makeError("vm/index/f", args[3], ix[1], len(str), args[1], args[2])
						break Switch
					}
					vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, str[ix[0].V.(int):ix[1].V.(int)]}
				case values.TUPLE:
					tup := container.V.([]values.Value)
					if ix[1].V.(int) > len(tup) {
						vm.Mem[args[0]] = vm.makeError("vm/index/f", args[3])
						break Switch
					}
					vm.Mem[args[0]] = values.Value{values.TUPLE, tup[ix[0].V.(int):ix[1].V.(int)]}
				default:
					vm.Mem[args[0]] = vm.makeError("vm/index/g", args[3])
					break Switch
				}
			} else {
				// Otherwise it's not a slice. We switch on the type of the lhs.
				containerType := container.T
				if cloneInfo, ok := vm.concreteTypes[containerType].(cloneType); ok {
					containerType = cloneInfo.parent
				}
				typeInfo := vm.concreteTypes[containerType]
				if typeInfo.isStruct() {
					ix := typeInfo.(structType).resolve(vm.Mem[args[2]].V.(int))
					vm.Mem[args[0]] = vm.Mem[args[1]].V.([]values.Value)[ix]
					break
				}
				if containerType == values.MAP {
					mp := container.V.(*values.Map)
					ix := vm.Mem[args[2]]
					result, ok := mp.Get(ix)
					if !ok {
						vm.Mem[args[0]] = vm.makeError("vm/index/h", args[3], vm.DefaultDescription(vm.Mem[args[2]]), args[1], args[2])
					} else {
						vm.Mem[args[0]] = result
					}
					break
				}
				if indexType != values.INT {
					vm.Mem[args[0]] = vm.makeError("vm/index/i", args[3], vm.DescribeType(vm.Mem[args[1]].T, LITERAL), vm.DescribeType(vm.Mem[args[2]].T, LITERAL), args[1], args[2])
					break
				}
				ty := container.T
				if cloneInfo, ok := vm.concreteTypes[container.T].(cloneType); ok {
					ty = cloneInfo.parent
				}
				switch ty {
				case values.LIST:
					vec := container.V.(vector.Vector)
					ix := index.V.(int)
					val, ok := vec.Index(ix)
					if !ok {
						vm.Mem[args[0]] = vm.makeError("vm/index/j", args[3], ix, vec.Len(), args[1], args[2])
					} else {
						vm.Mem[args[0]] = val.(values.Value)
					}
					break Switch
				case values.PAIR:
					pair := container.V.([]values.Value)
					ix := index.V.(int)
					ok := ix == 0 || ix == 1
					if ok {
						vm.Mem[args[0]] = pair[ix]
					} else {
						vm.Mem[args[0]] = vm.makeError("vm/index/k", args[3], ix)
					}
					break Switch
				case values.STRING:
					str := container.V.(string)
					ix := index.V.(int)
					ok := 0 <= ix && ix < len(str)
					if ok {
						val := values.Value{values.RUNE, rune(str[ix])}
						vm.Mem[args[0]] = val
					} else {
						vm.Mem[args[0]] = vm.makeError("vm/index/l", args[3], ix, len(str), args[1], args[2])
					}
					break Switch
				case values.TUPLE:
					tuple := container.V.([]values.Value)
					ix := index.V.(int)
					ok := 0 <= ix && ix < len(tuple)
					if ok {
						vm.Mem[args[0]] = tuple[ix]
					} else {
						vm.Mem[args[0]] = vm.makeError("vm/index/m", args[3], ix, len(tuple), args[1], args[2])
					}
					break Switch
				case values.TYPE:
					abTyp := container.V.(values.AbstractType)
					if len(abTyp.Types) != 1 {
						vm.Mem[args[0]] = vm.makeError("vm/index/n", args[3])
						break
					}
					typ := abTyp.Types[0]
					if !vm.concreteTypes[typ].isEnum() {
						vm.Mem[args[0]] = vm.makeError("vm/index/o", args[3])
						break
					}
					ix := index.V.(int)
					ok := 0 <= ix && ix < len(vm.concreteTypes[typ].(enumType).elementNames)
					if ok {
						vm.Mem[args[0]] = values.Value{typ, ix}
					} else {
						vm.Mem[args[0]] = vm.makeError("vm/index/p", args[3])
					}
					break Switch
				default:
					vm.Mem[args[0]] = vm.makeError("vm/index/q", args[3], vm.DescribeType(vm.Mem[args[1]].T, LITERAL))
					break Switch
				}
			}
		case IxZl:
			typeInfo := vm.concreteTypes[vm.Mem[args[1]].T].(structType)
			ix := typeInfo.resolve(vm.Mem[args[2]].V.(int))
			vm.Mem[args[0]] = vm.Mem[args[1]].V.([]values.Value)[ix]
		case IxZn:
			vm.Mem[args[0]] = vm.Mem[args[1]].V.([]values.Value)[args[2]]
		case Jmp:
			loc = args[0]
			continue
		case Jsr:
			vm.callstack = append(vm.callstack, loc)
			loc = args[0]
			continue
		case KeyM:
			vm.Mem[args[0]] = values.Value{values.LIST, vm.Mem[args[1]].V.(*values.Map).AsVector()}
		case KeyZ:
			result := vector.Empty
			for _, labelNumber := range vm.concreteTypes[vm.Mem[args[1]].T].(structType).labelNumbers {
				result = result.Conj(values.Value{values.LABEL, labelNumber})
			}
			vm.Mem[args[0]] = values.Value{values.LIST, result}
		case Lbls:
			stringToConvert := vm.Mem[args[1]].V.(string)
			labelNo, ok := vm.FieldLabelsInMem[stringToConvert]
			if ok {
				vm.Mem[args[0]] = vm.Mem[labelNo]
			} else {
				vm.Mem[args[0]] = vm.makeError("vm/label/exist", args[2], stringToConvert)
			}
		case LenL:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(vector.Vector).Len()}
		case LenM:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(*values.Map).Len()}
		case Lens:
			vm.Mem[args[0]] = values.Value{values.INT, len(vm.Mem[args[1]].V.(string))}
		case LenS:
			vm.Mem[args[0]] = values.Value{values.INT, vm.Mem[args[1]].V.(values.Set).Len()}
		case LenT:
			vm.Mem[args[0]] = values.Value{values.INT, len(vm.Mem[args[1]].V.([]values.Value))}
		case List:
			list := vector.Empty
			if vm.Mem[args[1]].T == values.TUPLE {
				for _, v := range vm.Mem[args[1]].V.([]values.Value) {
					list = list.Conj(v)
				}
			} else {
				list = list.Conj(vm.Mem[args[1]])
			}
			vm.Mem[args[0]] = values.Value{values.LIST, list}
		case Litx:
			vm.Mem[args[0]] = values.Value{values.STRING, vm.Literal(vm.Mem[args[1]])}
		case Log:
			fmt.Print(vm.Mem[args[0]].V.(string) + "\n\n")
		case Logn:
			vm.logging = false
		case Logy:
			vm.logging = true
		case Mker:
			vm.Mem[args[0]] = values.Value{values.ERROR, &report.Error{ErrorId: "eval/user", Message: vm.Mem[args[1]].V.(string), Token: vm.Tokens[args[2]]}}
		case Mkfn:
			lf := vm.LambdaFactories[args[1]]
			newLambda := *lf.Model
			newLambda.captures = make([]values.Value, len(lf.CaptureLocations))
			for i, v := range lf.CaptureLocations {
				val := vm.Mem[v]
				if val.T == values.THUNK {
					vm.Run(val.V.(uint32))
					val = vm.Mem[v]
				}
				newLambda.captures[i] = val
			}
			vm.Mem[args[0]] = values.Value{values.FUNC, newLambda}
		case Mkit:
			vm.Mem[args[0]] = vm.NewIterator(vm.Mem[args[1]], args[2] == 1, args[3])
		case Mkmp:
			result := &values.Map{}
			for _, p := range vm.Mem[args[1]].V.([]values.Value) {
				if p.T != values.PAIR {
					vm.Mem[args[0]] = vm.makeError("vm/map/pair", args[2], p, vm.DescribeType(p.T, LITERAL))
					break Switch
				}
				k := p.V.([]values.Value)[0]
				v := p.V.([]values.Value)[1]
				if !((values.NULL <= k.T && k.T < values.PAIR) || vm.concreteTypes[v.T].isEnum()) {
					vm.Mem[args[0]] = vm.makeError("vm/map/key", args[2], k, vm.DescribeType(k.T, LITERAL))
					break Switch
				}
				result = result.Set(k, v)
			}
			vm.Mem[args[0]] = values.Value{values.MAP, result}
		case Mkpr:
			vm.Mem[args[0]] = values.Value{values.PAIR, []values.Value{vm.Mem[args[1]], vm.Mem[args[2]]}}
		case Mkst:
			result := values.Set{}
			for _, v := range vm.Mem[args[1]].V.([]values.Value) {
				if !((values.NULL <= v.T && v.T < values.PAIR) || vm.concreteTypes[v.T].isEnum()) {
					vm.Mem[args[0]] = vm.makeError("vm/set", args[2], v, vm.DescribeType(v.T, LITERAL))
					break Switch
				}
				result = result.Add(v)
			}
			vm.Mem[args[0]] = values.Value{values.SET, result}
		case MkSn:
			sFac := vm.SnippetFactories[args[1]]
			vals := vector.Empty
			for _, v := range sFac.bindle.valueLocs {
				vals = vals.Conj(vm.Mem[v])
			}
			vm.Mem[args[0]] = values.Value{values.ValueType(sFac.snippetType),
				[]values.Value{{values.STRING, sFac.sourceString}, {values.LIST, vals}, {values.SNIPPET_DATA, sFac.bindle}}}
		case Mlfi:
			vm.Mem[args[0]] = values.Value{values.FLOAT, vm.Mem[args[1]].V.(float64) * float64(vm.Mem[args[2]].V.(int))}
		case Modi:
			divisor := vm.Mem[args[2]].V.(int)
			if divisor == 0 {
				vm.Mem[args[0]] = vm.makeError("vm/mod/int", args[3])
			} else {
				vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(int) % vm.Mem[args[2]].V.(int)}
			}
		case Mulf:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(float64) * vm.Mem[args[2]].V.(float64)}
		case Muli:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(int) * vm.Mem[args[2]].V.(int)}
		case Negf:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, -vm.Mem[args[1]].V.(float64)}
		case Negi:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, -vm.Mem[args[1]].V.(int)}
		case Notb:
			vm.Mem[args[0]] = values.Value{values.BOOL, !vm.Mem[args[1]].V.(bool)}
		case Orb:
			vm.Mem[args[0]] = values.Value{values.BOOL, (vm.Mem[args[1]].V.(bool) || vm.Mem[args[2]].V.(bool))}
		case Outp:
			vm.IoHandle.OutHandle.Out([]values.Value{vm.Mem[args[0]]}, vm)
		case Outt:
			fmt.Println(vm.Literal(vm.Mem[args[0]]))
		case Psnp: // Only for if you 'post HTML' or 'post SQL'.
			// Everything we need to evaluate the snippets has been precompiled into a secret third field of the snippet struct, having
			// type SNIPPET_DATA. We extract the relevant data from this and execute the precompiled code.
			bindle := vm.Mem[args[1]].V.([]values.Value)[2].V.(*SnippetBindle)
			objectString := vm.Mem[bindle.objectStringLoc].V.(string)
			// What we do at that point depends on what kind of snippet it is, which is also recorded in the snippet data:
			switch bindle.compiledSnippetKind {
			case HTML_SNIPPET: // We parse this and emit it to whatever Output is.
				t, err := template.New("html snippet").Parse(objectString) // TODO: parse this at compile time and stick it in the bindle.
				if err != nil {
					panic("Template parsing error.")
					// TODO --- this.
					// continue
				}
				var buf strings.Builder
				injector := HTMLInjector{make([]any, 0, len(bindle.valueLocs))}
				for i := 1; i < len(bindle.valueLocs); i = i + 2 {
					mLoc := bindle.valueLocs[i]
					v := vm.Mem[mLoc]
					switch v.T {
					case values.STRING:
						injector.Data = append(injector.Data, v.V.(string))
					case values.INT:
						injector.Data = append(injector.Data, v.V.(int))
					case values.BOOL:
						injector.Data = append(injector.Data, v.V.(bool))
					case values.FLOAT:
						injector.Data = append(injector.Data, v.V.(float64))
					default:
						panic("Unhandled case:" + vm.concreteTypes[v.T].getName(LITERAL))
					}
				}
				t.Execute(&buf, injector)
				vm.IoHandle.OutHandle.Out([]values.Value{{values.STRING, buf.String()}}, vm)
				vm.Mem[args[0]] = values.Value{values.SUCCESSFUL_VALUE, nil}
			case SQL_SNIPPET:
				injector := make([]values.Value, 0, len(bindle.valueLocs))
				for i := 1; i < len(bindle.valueLocs); i = i + 2 {
					mLoc := bindle.valueLocs[i]
					injector = append(injector, vm.Mem[mLoc])
				}
				vm.Mem[args[0]] = vm.evalPostSQL(objectString, injector)
			}
		case Qabt:
			varcharLimit := args[1]
			for _, t := range args[2 : len(args)-1] {
				if vm.Mem[args[0]].T == values.ValueType(t) &&
					!(values.ValueType(t) == values.STRING && uint32(len(vm.Mem[args[0]].V.(string))) > varcharLimit) {
					loc = loc + 1
					continue loop
				}
			}
			loc = args[len(args)-1]
			continue
		case Qfls:
			if vm.Mem[args[0]].V.(bool) {
				loc = args[1]
			} else {
				loc = loc + 1
			}
			continue
		case Qitr:
			if vm.Mem[args[0]].V.(values.Iterator).Unfinished() {
				loc = args[1]
			} else {
				loc = loc + 1

			}
			continue
		case QleT:
			if len(vm.Mem[args[0]].V.([]values.Value)) <= int(args[1]) {
				loc = loc + 1
			} else {
				loc = args[2]
			}
			continue
		case QlnT:
			if len(vm.Mem[args[0]].V.([]values.Value)) == int(args[1]) {
				loc = loc + 1
			} else {
				loc = args[2]
			}
			continue
		case Qlog:
			if vm.logging {
				loc = loc + 1
			} else {
				loc = args[0]
			}
			continue
		case Qntp:
			if vm.Mem[args[0]].T != values.ValueType(args[1]) {
				loc = loc + 1
			} else {
				loc = args[2]
			}
			continue
		case Qnvh:
			if vm.Mem[args[0]].T == values.STRING && len(vm.Mem[args[0]].V.(string)) <= int(args[1]) {
				loc = args[2]
			} else {
				loc = loc + 1
			}
			continue
		case Qnvq:
			if vm.Mem[args[0]].T == values.NULL || (vm.Mem[args[0]].T == values.STRING && len(vm.Mem[args[0]].V.(string)) <= int(args[1])) {
				loc = args[2]
			} else {
				loc = loc + 1
			}
			continue
		case Qsat:
			if vm.Mem[args[0]].T != values.UNSATISFIED_CONDITIONAL {
				loc = loc + 1
			} else {
				loc = args[1]
			}
			continue
		case Qsng:
			if vm.Mem[args[0]].T >= values.INT {
				loc = loc + 1
			} else {
				loc = args[1]
			}
			continue
		case Qsnq:
			if vm.Mem[args[0]].T >= values.NULL {
				loc = loc + 1
			} else {
				loc = args[1]
			}
			continue
		case Qspt:
			switch ty := vm.concreteTypes[vm.Mem[args[0]].T].(type) {
			case structType:
				if ty.snippet {
					loc = loc + 1
					continue
				}
			}
			loc = args[1]
			continue
		case Qspq:
			if vm.Mem[args[0]].T == values.NULL {
				loc = loc + 1
				continue
			}
			switch ty := vm.concreteTypes[vm.Mem[args[0]].T].(type) {
			case structType:
				if ty.snippet {
					loc = loc + 1
					continue
				}
			}
			loc = args[1]
			continue
		case Qstr:
			switch vm.concreteTypes[vm.Mem[args[0]].T].(type) {
			case structType:
				loc = loc + 1
			default:
				loc = args[1]
			}
			continue
		case Qstq:
			if vm.Mem[args[0]].T == values.NULL {
				loc = loc + 1
				continue
			}
			switch vm.concreteTypes[vm.Mem[args[0]].T].(type) {
			case structType:
				loc = loc + 1
			default:
				loc = args[1]
			}
		case Qtpt:
			slice := vm.Mem[args[0]].V.([]values.Value)[args[1]:]
			for _, v := range slice {
				var found bool
				for _, t := range args[2 : len(args)-1] {
					if v.T == values.ValueType(t) {
						found = true
						break
					}
				}
				if !found {
					loc = args[len(args)-1]
					continue loop
				}
			}
			loc = loc + 1
			continue
		case Qtru:
			if vm.Mem[args[0]].V.(bool) {
				loc = loc + 1
			} else {
				loc = args[1]
			}
			continue
		case Qtyp:
			if vm.Mem[args[0]].T == values.ValueType(args[1]) {
				loc = loc + 1
			} else {
				loc = args[2]
			}
			continue
		case Qvch:
			if vm.Mem[args[0]].T == values.STRING && len(vm.Mem[args[0]].V.(string)) <= int(args[1]) {
				loc = loc + 1
			} else {
				loc = args[2]
			}
			continue
		case Qvcq:
			if vm.Mem[args[0]].T == values.NULL || (vm.Mem[args[0]].T == values.STRING && len(vm.Mem[args[0]].V.(string)) <= int(args[1])) {
				loc = loc + 1
			} else {
				loc = args[2]
			}
			continue
		case Ret:
			if len(vm.callstack) == stackHeight { // This is so that we can call "Run" when we have things on the stack and it will bottom out at the appropriate time.
				break loop
			}
			loc = vm.callstack[len(vm.callstack)-1]
			vm.callstack = vm.callstack[0 : len(vm.callstack)-1]
		case Rpop:
			rData := vm.recursionStack[len(vm.recursionStack)-1]
			vm.recursionStack = vm.recursionStack[:len(vm.recursionStack)-1]
			copy(vm.Mem[rData.loc:int(rData.loc)+len(rData.mems)], rData.mems)
		case Rpsh:
			lowLoc := args[0]
			highLoc := args[1]
			memToSave := make([]values.Value, highLoc-lowLoc)
			copy(memToSave, vm.Mem[lowLoc:highLoc])
			vm.recursionStack = append(vm.recursionStack, recursionData{memToSave, lowLoc})
		case Rsit:
			vm.Mem[args[0]].V.(values.Iterator).Reset()
		case SliL:
			vec := vm.Mem[args[1]].V.(vector.Vector)
			ix := vm.Mem[args[2]].V.([]values.Value)
			if ix[0].T != values.INT {
				vm.Mem[args[0]] = vm.makeError("vm/slice/list/a", args[3], vm.DescribeType(ix[0].T, LITERAL), args[1], args[2])
				break Switch
			}
			if ix[1].T != values.INT {
				vm.Mem[args[0]] = vm.makeError("vm/slice/list/b", args[3], vm.DescribeType(ix[1].T, LITERAL), args[1], args[2])
				break Switch
			}
			if ix[0].V.(int) < 0 {
				vm.Mem[args[0]] = vm.makeError("vm/slice/list/c", args[3], vec, ix)
				break Switch
			}
			if ix[1].V.(int) < ix[0].V.(int) {
				vm.Mem[args[0]] = vm.makeError("vm/slice/list/d", args[3], ix[0].V.(int), ix[1].V.(int), args[1], args[2])
				break Switch
			}
			if vec.Len() < ix[1].V.(int) {
				vm.Mem[args[0]] = vm.makeError("vm/slice/list/e", args[3], ix[1].V.(int), vec.Len(), args[1], args[2])
				break Switch
			}
			vm.Mem[args[0]] = values.Value{values.LIST, vec.SubVector(ix[0].V.(int), ix[1].V.(int))}
		case Slis:
			str := vm.Mem[args[1]].V.(string)
			ix := vm.Mem[args[2]].V.([]values.Value)
			if ix[0].T != values.INT {
				vm.Mem[args[0]] = vm.makeError("vm/slice/string/a", args[3], vm.DescribeType(ix[0].T, LITERAL), args[1], args[2])
				break Switch
			}
			if ix[1].T != values.INT {
				vm.Mem[args[0]] = vm.makeError("vm/slice/string/b", args[3], vm.DescribeType(ix[1].T, LITERAL), args[1], args[2])
				break Switch
			}
			if ix[0].V.(int) < 0 {
				vm.Mem[args[0]] = vm.makeError("vm/slice/string/c", args[3], args[1], args[2])
				break Switch
			}
			if ix[1].V.(int) < ix[0].V.(int) {
				vm.Mem[args[0]] = vm.makeError("vm/slice/string/d", args[3], ix[0].V.(int), ix[1].V.(int), args[1], args[2])
				break Switch
			}
			if len(str) < ix[1].V.(int) {
				vm.Mem[args[0]] = vm.makeError("vm/slice/string/e", args[3], ix[1].V.(int), len(str), args[1], args[2])
				break Switch
			}
			vm.Mem[args[0]] = values.Value{values.STRING, str[ix[0].V.(int):ix[1].V.(int)]}
		case SliT:
			tup := vm.Mem[args[1]].V.([]values.Value)
			ix := vm.Mem[args[2]].V.([]values.Value)
			if ix[0].T != values.INT {
				vm.Mem[args[0]] = vm.makeError("vm/slice/tuple/a", args[3], vm.DescribeType(ix[0].T, LITERAL), args[1], args[2])
				break Switch
			}
			if ix[1].T != values.INT {
				vm.Mem[args[0]] = vm.makeError("vm/slice/tuple/b", args[3], vm.DescribeType(ix[1].T, LITERAL), args[1], args[2])
				break Switch
			}
			if ix[0].V.(int) < 0 {
				vm.Mem[args[0]] = vm.makeError("vm/slice/tuple/c", args[3], ix[0].V.(int), ix[1].V.(int), args[1], args[2])
				break Switch
			}
			if ix[1].V.(int) < ix[0].V.(int) {
				vm.Mem[args[0]] = vm.makeError("vm/slice/tuple/d", args[3], ix[0].V.(int), ix[1].V.(int), args[1], args[2])
				break Switch
			}
			if len(tup) < ix[1].V.(int) {
				vm.Mem[args[0]] = vm.makeError("vm/slice/tuple/e", args[3], ix[1].V.(int), len(tup), args[1], args[2])
				break Switch
			}
			vm.Mem[args[0]] = values.Value{values.TUPLE, tup[ix[0].V.(int):ix[1].V.(int)]}
		case SlTn:
			vm.Mem[args[0]] = values.Value{values.TUPLE, (vm.Mem[args[1]].V.([]values.Value))[args[2]:]}
		case Strc:
			fields := make([]values.Value, 0, len(args)-2)
			for _, loc := range args[2:] {
				fields = append(fields, vm.Mem[loc])
			}
			vm.Mem[args[0]] = values.Value{values.ValueType(args[1]), fields}
		case Strx:
			vm.Mem[args[0]] = values.Value{values.STRING, vm.DefaultDescription(vm.Mem[args[1]])}
		case Subf:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(float64) - vm.Mem[args[2]].V.(float64)}
		case Subi:
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(int) - vm.Mem[args[2]].V.(int)}
		case Thnk:
			vm.Mem[args[0]] = values.Value{values.THUNK, ThunkValue{args[1], args[2]}}
		case Tplf:
			tup := vm.Mem[args[1]].V.([]values.Value)
			if len(tup) == 0 {
				vm.Mem[args[0]] = vm.makeError("vm/tup/first", args[2])
				break Switch
			}
			vm.Mem[args[0]] = tup[0]
		case Tpll:
			tup := vm.Mem[args[1]].V.([]values.Value)
			if len(tup) == 0 {
				vm.Mem[args[0]] = vm.makeError("vm/tup/last", args[2])
				break Switch
			}
			vm.Mem[args[0]] = tup[len(tup)-1]
		case Trak:
			staticData := vm.tracking[args[0]]
			newData := TrackingData{staticData.flavor, staticData.tok, make([]any, len(staticData.args))}
			copy(newData.args, staticData.args)
			for i, v := range newData.args {
				if v, ok := v.(uint32); ok {
					newData.args[i] = vm.Mem[v]
				}
			}
			vm.LiveTracking = append(vm.LiveTracking, newData)
		case TuLx:
			vector, ok := vm.Mem[args[1]].V.(vector.Vector)
			if !ok {
				vm.Mem[args[0]] = vm.makeError("vm/splat/type", args[2], args[1])
				break Switch
			}
			length := vector.Len()
			slice := make([]values.Value, length)
			for i := 0; i < length; i++ {
				element, _ := vector.Index(i)
				slice[i] = element.(values.Value)
			}
			vm.Mem[args[0]] = values.Value{values.TUPLE, slice}
		case TupL:
			vector := vm.Mem[args[1]].V.(vector.Vector)
			length := vector.Len()
			slice := make([]values.Value, length)
			for i := 0; i < length; i++ {
				element, _ := vector.Index(i)
				slice[i] = element.(values.Value)
			}
			vm.Mem[args[0]] = values.Value{values.TUPLE, slice}
		case Typs:
			result := values.Set{}
			for _, v := range vm.Mem[args[1]].V.(values.AbstractType).Types {
				concType := values.AbstractType{[]values.ValueType{v}, DUMMY}
				if v == values.STRING {
					concType.Varchar = vm.Mem[args[1]].V.(values.AbstractType).Varchar
				}
				result = result.Add(values.Value{values.TYPE, concType})
			}
			vm.Mem[args[0]] = values.Value{values.SET, result}
		case Typu:
			lhs := vm.Mem[args[1]].V.(values.AbstractType)
			rhs := vm.Mem[args[2]].V.(values.AbstractType)
			vm.Mem[args[0]] = values.Value{values.TYPE, lhs.Union(rhs)}
		case Typx:
			vm.Mem[args[0]] = values.Value{values.TYPE, values.AbstractType{[]values.ValueType{vm.Mem[args[1]].T}, DUMMY}}
		case UntE:
			err := vm.Mem[args[0]].V.(*report.Error)
			newArgs := []any{}
			newVals := []values.Value{}
			for _, arg := range err.Args {
				switch arg := arg.(type) {
				case uint32:
					newVals = append(newVals, vm.Mem[arg])
				default:
					newArgs = append(newArgs, arg)
				}
			}
			err.Args = newArgs
			err.Values = newVals
		case Untk:
			if vm.Mem[args[0]].T == values.THUNK {
				resultLoc := vm.Mem[args[0]].V.(ThunkValue).MLoc
				codeAddr := vm.Mem[args[0]].V.(ThunkValue).CAddr
				vm.Run(codeAddr)
				vm.Mem[args[0]] = vm.Mem[resultLoc]
			}
		case Uwrp:
			if vm.Mem[args[1]].T == values.ERROR {
				err := vm.Mem[args[1]].V.(*report.Error)
				errWithMessage := report.CreateErr(err.ErrorId, err.Token, err.Args...)
				vm.Mem[args[0]] = values.Value{vm.typeNumberOfUnwrappedError, []values.Value{{values.STRING, errWithMessage.ErrorId}, {values.STRING, errWithMessage.Message}}}
			} else {
				vm.Mem[args[0]] = vm.makeError("vm/unwrap", args[2], vm.DescribeType(vm.Mem[args[1]].T, LITERAL))
			}
		case Varc:
			n := vm.Mem[args[1]].V.(int)
			if n < 0 || n > DUMMY {
				vm.Mem[args[0]] = vm.Mem[args[2]] // A prepared error. TODO --- do this by passing the token instead.
			} else {
				vm.Mem[args[0]] = values.Value{values.TYPE, values.AbstractType{[]values.ValueType{values.STRING}, uint32(n)}}
			}
		case Vlid:
			vm.Mem[args[0]] = values.Value{values.BOOL, vm.Mem[args[1]].T != values.ERROR}
		case WthL:
			var pairs []values.Value
			if (vm.Mem[args[2]].T) == values.PAIR {
				pairs = []values.Value{vm.Mem[args[2]]}
			} else {
				pairs = vm.Mem[args[2]].V.([]values.Value)
			}
			result := values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(vector.Vector)}
			for _, pair := range pairs {
				if pair.T != values.PAIR {
					vm.Mem[args[0]] = vm.makeError("vm/with/list/a", args[3], vm.DescribeType(pair.T, LITERAL), args[1], args[2])
					break
				}
				key := pair.V.([]values.Value)[0]
				val := pair.V.([]values.Value)[1]
				var keys []values.Value
				if key.T == values.LIST {
					vec := key.V.(vector.Vector)
					ln := vec.Len()
					if ln == 0 {
						vm.Mem[args[0]] = vm.makeError("vm/with/list/b", args[3])
						break Switch
					}
					keys = make([]values.Value, ln)
					for i := 0; i < ln; i++ {
						el, _ := vec.Index(i)
						keys[i] = el.(values.Value)
					}
				} else {
					keys = []values.Value{key}
				}
				result = vm.with(result, keys, val, args[3])
				if result.T == values.ERROR {
					break
				}
			}
			vm.Mem[args[0]] = result
		case WthM:
			var pairs []values.Value
			if (vm.Mem[args[2]].T) == values.PAIR {
				pairs = []values.Value{vm.Mem[args[2]]}
			} else {
				pairs = vm.Mem[args[2]].V.([]values.Value)
			}
			result := values.Value{vm.Mem[args[1]].T, vm.Mem[args[1]].V.(*values.Map)}
			for _, pair := range pairs {
				if pair.T != values.PAIR {
					vm.Mem[args[0]] = vm.makeError("vm/with/map/a", args[3])
					break
				}
				key := pair.V.([]values.Value)[0]
				val := pair.V.([]values.Value)[1]
				var keys []values.Value
				if key.T == values.LIST {
					vec := key.V.(vector.Vector)
					ln := vec.Len()
					if ln == 0 {
						vm.Mem[args[0]] = vm.makeError("vm/with/map/b", args[3])
						break
					}
					keys = make([]values.Value, ln)
					for i := 0; i < ln; i++ {
						el, _ := vec.Index(i)
						keys[i] = el.(values.Value)
					}
				} else {
					keys = []values.Value{key}
				}
				result = vm.with(result, keys, val, args[3])
				if result.T == values.ERROR {
					break
				}
			}
			vm.Mem[args[0]] = result
		case Wtht:
			typL := vm.Mem[args[1]].V.(values.AbstractType)
			if typL.Len() != 1 {
				vm.Mem[args[0]] = vm.makeError("vm/with/type/a", args[3], vm.DescribeAbstractType(typL, LITERAL))
				break Switch
			}
			typ := typL.Types[0]
			if !vm.concreteTypes[typ].isStruct() {
				vm.Mem[args[0]] = vm.makeError("vm/with/type/b", args[3], vm.DescribeType(typ, LITERAL))
				break Switch
			}
			typeInfo := vm.concreteTypes[typ].(structType)
			var pairs []values.Value
			if (vm.Mem[args[2]].T) == values.PAIR {
				pairs = []values.Value{vm.Mem[args[0]]}
			} else {
				pairs = vm.Mem[args[2]].V.([]values.Value)
			}
			outVals := make([]values.Value, len(vm.concreteTypes[typ].(structType).labelNumbers))
			for _, pair := range pairs {
				if pair.T != values.PAIR {
					vm.Mem[args[0]] = vm.makeError("vm/with/type/c", args[3], vm.DescribeType(pair.T, LITERAL))
					break
				}
				key := pair.V.([]values.Value)[0]
				val := pair.V.([]values.Value)[1]
				if key.T != values.LABEL {
					vm.Mem[args[0]] = vm.makeError("vm/with/type/d", args[3], vm.DescribeType(pair.T, LITERAL))
					break Switch
				}
				keyNumber := typeInfo.resolve(key.V.(int))
				if keyNumber == -1 {
					vm.Mem[args[0]] = vm.makeError("vm/with/type/e", args[3], vm.DefaultDescription(key), vm.DescribeType(typ, LITERAL))
					break Switch
				}
				if outVals[keyNumber].T != values.UNDEFINED_VALUE {
					vm.Mem[args[0]] = vm.makeError("vm/with/type/f", args[3], vm.DefaultDescription(key))
					break Switch
				}
				outVals[keyNumber] = val
			}
			for i, v := range outVals {
				if v.T == values.UNDEFINED_VALUE {
					if vm.concreteTypes[typ].(structType).abstractStructFields[i].Contains(values.NULL) {
						outVals[i] = values.Value{values.NULL, nil}
						break Switch
					} else {
						labName := vm.Labels[vm.concreteTypes[typ].(structType).labelNumbers[i]]
						vm.Mem[args[0]] = vm.makeError("vm/with/type/g", args[3], labName)
						break Switch
					}
				}
				if !vm.concreteTypes[typ].(structType).abstractStructFields[i].Contains(v.T) {
					labName := vm.Labels[vm.concreteTypes[typ].(structType).labelNumbers[i]]
					vm.Mem[args[0]] = vm.makeError("vm/with/type/h", args[3], vm.DescribeType(v.T, LITERAL), labName, vm.DescribeType(typ, LITERAL), vm.DescribeAbstractType(vm.concreteTypes[typ].(structType).abstractStructFields[i], LITERAL))
					break Switch
				}
			}
			vm.Mem[args[0]] = values.Value{typ, outVals}
		case WthZ:
			typ := vm.Mem[args[1]].T
			var pairs []values.Value
			if (vm.Mem[args[2]].T) == values.PAIR {
				pairs = []values.Value{vm.Mem[args[2]]}
			} else {
				pairs = vm.Mem[args[2]].V.([]values.Value)
			}
			outVals := make([]values.Value, len(vm.concreteTypes[typ].(structType).labelNumbers))
			copy(outVals, vm.Mem[args[1]].V.([]values.Value))
			result := values.Value{typ, outVals}
			for _, pair := range pairs {
				if pair.T != values.PAIR {
					vm.Mem[args[0]] = vm.makeError("vm/with/struct/a", args[3], vm.DescribeType(pair.T, LITERAL), args[1], args[2])
					break
				}
				key := pair.V.([]values.Value)[0]
				val := pair.V.([]values.Value)[1]
				var keys []values.Value
				if key.T == values.LIST {
					vec := key.V.(vector.Vector)
					ln := vec.Len()
					if ln == 0 {
						vm.Mem[args[0]] = vm.makeError("vm/with/struct/b", args[3])
						break
					}
					keys = make([]values.Value, ln)
					for i := 0; i < ln; i++ {
						el, _ := vec.Index(i)
						keys[i] = el.(values.Value)
					}
				} else {
					keys = []values.Value{key}
				}
				result = vm.with(result, keys, val, args[3])
				if result.T == values.ERROR {
					break
				}
			}
			vm.Mem[args[0]] = result
		case WtoM:
			var items []values.Value
			if (vm.Mem[args[2]].T) == values.TUPLE {
				items = vm.Mem[args[2]].V.([]values.Value)
			} else {
				items = []values.Value{vm.Mem[args[2]]}
			}
			mp := vm.Mem[args[1]].V.(*values.Map)
			for _, key := range items {
				if (key.T < values.NULL || key.T >= values.FUNC) && (key.T < values.LABEL || vm.concreteTypes[key.T].isEnum()) { // Check that the key is orderable.
					vm.Mem[args[0]] = vm.makeError("vm/without", args[3], vm.DescribeType(key.T, LITERAL))
					break Switch
				}
				mp = (*mp).Delete(key)
			}
			vm.Mem[args[0]] = values.Value{vm.Mem[args[1]].T, mp}
		default:
			panic("Unhandled opcode '" + OPERANDS[vm.Code[loc].Opcode].oc + "'")
		}
		loc++
	}
	if settings.SHOW_RUNTIME {
		println()
	}
}

// Implements equality-by-value. Assumes that the two values have already been verified to have the same type.
func (mc Vm) equals(v, w values.Value) bool {
	switch v.T {
	case values.NULL:
		return true
	case values.INT:
		return v.V.(int) == w.V.(int)
	case values.BOOL:
		return v.V.(bool) == w.V.(bool)
	case values.RUNE:
		return v.V.(rune) == w.V.(rune)
	case values.STRING:
		return v.V.(string) == w.V.(string)
	case values.FLOAT:
		return v.V.(float64) == w.V.(float64)
	case values.TYPE:
		return v.V.(values.AbstractType).Equals(w.V.(values.AbstractType))
	case values.PAIR:
		return mc.equals(v.V.([]values.Value)[0], w.V.([]values.Value)[0]) &&
			mc.equals(v.V.([]values.Value)[1], w.V.([]values.Value)[1])
	case values.LIST:
		K := v.V.(vector.Vector)
		L := w.V.(vector.Vector)
		lth := K.Len()
		if L.Len() != lth {
			return false
		}
		for i := 0; i < lth; i++ {
			kEl, _ := K.Index(i)
			lEl, _ := L.Index(i)
			if kEl.(values.Value).T != lEl.(values.Value).T {
				return false
			}
			if !mc.equals(kEl.(values.Value), lEl.(values.Value)) {
				return false
			}
		}
		return true
	case values.LABEL:
		return v.V.(int) == w.V.(int)
	case values.SET:

	case values.MAP:

	case values.FUNC:
		return false
	}
	if mc.concreteTypes[v.T].isEnum() {
		return v.V.(int) == w.V.(int)
	}
	if mc.concreteTypes[v.T].isStruct() {
		for i, v := range v.V.([]values.Value) {
			if !mc.equals(v, w.V.([]values.Value)[i]) {
				return false
			}
		}
		return true
	}
	panic("Wut?")
}

func (vm *Vm) with(container values.Value, keys []values.Value, val values.Value, errTok uint32) values.Value {
	key := keys[0]
	switch container.T {
	case values.LIST:
		vec := container.V.(vector.Vector)
		if key.T != values.INT {
			return vm.makeError("vm/with/a", errTok, vm.DescribeType(key.T, LITERAL))
		}
		keyNumber := key.V.(int)
		if keyNumber < 0 || keyNumber >= vec.Len() {
			return vm.makeError("vm/with/b", errTok, key.V.(int), vec.Len())
		}
		if len(keys) == 1 {
			container.V = vec.Assoc(keyNumber, val)
			return container
		}
		el, _ := vec.Index(keyNumber)
		container.V = vec.Assoc(keyNumber, vm.with(el.(values.Value), keys[1:], val, errTok))
		return container
	case values.MAP:
		mp := container.V.(*values.Map)
		if ((key.T < values.NULL) || (key.T >= values.FUNC && key.T < values.LABEL)) && !vm.concreteTypes[key.T].isEnum() { // Check that the key is orderable.
			return vm.makeError("vm/with/c", errTok, vm.DescribeType(key.T, LITERAL))
		}
		if len(keys) == 1 {
			mp = mp.Set(key, val)
			return values.Value{values.MAP, mp}
		}
		el, _ := mp.Get(key)
		mp = mp.Set(key, vm.with(el, keys[1:], val, errTok))
		return values.Value{values.MAP, mp}
	default: // It's a struct.
		fields := make([]values.Value, len(container.V.([]values.Value)))
		clone := values.Value{container.T, fields}
		copy(fields, container.V.([]values.Value))
		typeInfo := vm.concreteTypes[container.T].(structType)
		if key.T != values.LABEL {
			return vm.makeError("vm/with/d", errTok, vm.DescribeType(key.T, LITERAL))
		}
		fieldNumber := typeInfo.resolve(key.V.(int))
		if fieldNumber == -1 {
			return vm.makeError("vm/with/e", errTok, vm.DefaultDescription(key), vm.DescribeType(container.T, LITERAL))
		}
		if len(keys) > 1 {
			val = vm.with(fields[fieldNumber], keys[1:], val, errTok)
		}
		if !vm.concreteTypes[container.T].(structType).abstractStructFields[fieldNumber].Contains(val.T) {
			labName := vm.Labels[key.V.(int)]
			return vm.makeError("vm/with/f", errTok, vm.DescribeType(val.T, LITERAL), labName, vm.DescribeType(container.T, LITERAL), vm.DescribeAbstractType(vm.concreteTypes[container.T].(structType).abstractStructFields[fieldNumber], LITERAL))
		}
		fields[fieldNumber] = val
		return clone
	}
}

type supertype int

const (
	NATIVE supertype = iota
	ENUM
	STRUCT
)

type typeInformation interface {
	getName(flavor descriptionFlavor) string
	getPath() string
	isEnum() bool
	isStruct() bool
	isSnippet() bool
	isClone() bool
	isPrivate() bool
}

type builtinType struct {
	name string
	path string
}

func (t builtinType) getName(flavor descriptionFlavor) string {
	if flavor == LITERAL {
		return t.path + t.name
	}
	return string(t.name)
}

func (t builtinType) isEnum() bool {
	return false
}

func (t builtinType) isStruct() bool {
	return false
}

func (t builtinType) isSnippet() bool {
	return false
}

func (t builtinType) isClone() bool {
	return false
}

func (t builtinType) isPrivate() bool {
	return false
}

func (t builtinType) getPath() string {
	return t.path
}

type enumType struct {
	name         string
	path         string
	elementNames []string
	private      bool
}

func (t enumType) getName(flavor descriptionFlavor) string {
	if flavor == LITERAL {
		return t.path + t.name
	}
	return t.name
}

func (t enumType) isEnum() bool {
	return true
}

func (t enumType) isStruct() bool {
	return false
}

func (t enumType) isSnippet() bool {
	return false
}

func (t enumType) isPrivate() bool {
	return t.private
}

func (t enumType) isClone() bool {
	return false
}

func (t enumType) getPath() string {
	return t.path
}

type cloneType struct {
	name         string
	path         string
	parent       values.ValueType
	private      bool
	isSliceable  bool
	isFilterable bool
	isMappable   bool
}

func (t cloneType) getName(flavor descriptionFlavor) string {
	if flavor == LITERAL {
		return t.path + t.name
	}
	return t.name
}

func (t cloneType) isEnum() bool {
	return false
}

func (t cloneType) isStruct() bool {
	return false
}

func (t cloneType) isSnippet() bool {
	return false
}

func (t cloneType) isPrivate() bool {
	return t.private
}

func (t cloneType) isClone() bool {
	return true
}

func (t cloneType) getPath() string {
	return t.path
}

type structType struct {
	name                  string
	path                  string
	labelNumbers          []int
	snippet               bool
	private               bool
	abstractStructFields  []values.AbstractType
	alternateStructFields []AlternateType // TODO --- even assuming we also need this, it shouldn't be here.
	resolvingMap          map[int]int     // TODO --- it would probably be better to implment this as a linear search below a given threshhold and a binary search above it.
}

func (t structType) getName(flavor descriptionFlavor) string {
	if flavor == LITERAL {
		return t.path + t.name
	}
	return t.name
}

func (t structType) isEnum() bool {
	return false
}

func (t structType) isStruct() bool {
	return true
}

func (t structType) isSnippet() bool {
	return t.snippet
}

func (t structType) isPrivate() bool {
	return t.private
}

func (t structType) isClone() bool {
	return false
}

func (t structType) getPath() string {
	return t.path
}

func (t structType) addLabels(labels []int) structType {
	t.resolvingMap = make(map[int]int)
	for k, v := range labels {
		t.resolvingMap[v] = k
	}
	return t
}

func (t structType) resolve(labelNumber int) int {
	result, ok := t.resolvingMap[labelNumber]
	if ok {
		return result
	}
	return -1
}

func (vm *Vm) NewIterator(container values.Value, keysOnly bool, tokLoc uint32) values.Value {
	ty := container.T
	if cloneInfo, ok := vm.concreteTypes[ty].(cloneType); ok {
		ty = cloneInfo.parent
	}
	switch ty {
	case values.INT:
		return values.Value{values.ITERATOR, &values.KeyIncIterator{Max: container.V.(int)}}
	case values.LIST:
		if keysOnly {
			return values.Value{values.ITERATOR, &values.KeyIncIterator{Max: container.V.(vector.Vector).Len()}}
		} else {
			return values.Value{values.ITERATOR, &values.ListIterator{VecIt: container.V.(vector.Vector).Iterator()}}
		}
	case values.MAP:
		mapAsSlice := container.V.(*values.Map).AsSlice()
		return values.Value{values.ITERATOR, &values.MapIterator{KVPairs: mapAsSlice, Len: len(mapAsSlice)}}
	case values.PAIR:
		pair := container.V.([]values.Value)
		left := pair[0]
		right := pair[1]
		if left.T != values.INT || right.T != values.INT {
			return values.Value{values.ERROR, vm.makeError("vm/for/pair", tokLoc)}
		}
		leftV := left.V.(int)
		rightV := right.V.(int)
		if leftV <= rightV {
			return values.Value{values.ITERATOR, &values.IncIterator{StartVal: leftV, MaxVal: rightV, Val: leftV}}
		} else {
			return values.Value{values.ITERATOR, &values.DecIterator{MinVal: rightV, StartVal: leftV - 1, Val: leftV - 1}}
		}
	case values.SET:
		setAsSlice := container.V.(values.Set).AsSlice()
		return values.Value{values.ITERATOR, &values.SetIterator{Elements: setAsSlice, Len: len(setAsSlice)}}
	case values.STRING:
		if keysOnly {
			return values.Value{values.ITERATOR, &values.KeyIncIterator{Max: len(container.V.(string))}}
		} else {
			return values.Value{values.ITERATOR, &values.StringIterator{Str: container.V.(string)}}
		}
	case values.TUPLE:
		tupleElements := container.V.([]values.Value)
		if keysOnly {
			return values.Value{values.ITERATOR, &values.KeyIncIterator{Max: len(tupleElements)}}
		} else {
			return values.Value{values.ITERATOR, &values.TupleIterator{Elements: tupleElements, Len: len(tupleElements)}}
		}
	case values.TYPE:
		abTyp := container.V.(values.AbstractType)
		if len(abTyp.Types) != 1 {
			return values.Value{values.ERROR, vm.makeError("vm/for/type/a", tokLoc)}
		}
		typ := abTyp.Types[0]
		if !vm.concreteTypes[typ].isEnum() {
			return values.Value{values.ERROR, vm.makeError("vm/for/type/b", tokLoc)}
		}
		if keysOnly {
			return values.Value{values.ITERATOR, &values.KeyIncIterator{Max: len(vm.concreteTypes[typ].(enumType).elementNames)}}
		} else {
			return values.Value{values.ITERATOR, &values.EnumIterator{Type: typ, Max: len(vm.concreteTypes[typ].(enumType).elementNames)}}
		}
	default:
		return values.Value{values.ERROR, vm.makeError("vm/for/type", tokLoc)}
	}
}
