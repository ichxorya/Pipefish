package object

type AccessType int

const (
	ACCESS_PUBLIC = 0
	ACCESS_CONSTANT = 1
	ACCESS_PRIVATE = 2
)

type Environment struct {
	Store map[string]Storage
	Pending map[string]Object
	Ext *Environment
}

type Storage struct{
	obj Object
	access AccessType
	VarType string
}

func NewEnvironment() *Environment {
	s := make(map[string]Storage)
	p := make(map[string]Object)
	return &Environment{Store: s, Pending: p}
}


func (e *Environment) Get(name string) (Object, bool) {
	storage, ok := e.Store[name]
	if storage.access == ACCESS_PUBLIC || storage.access == ACCESS_PRIVATE {
		// Then it is a variable, and we check in the pending variables to see if there's anything to return.
		if pendingObject, exists := e.Pending[name]; exists {
			return pendingObject, exists
		}
	}
	if ok || e.Ext == nil { return storage.obj, ok }
	return e.Ext.Get(name)
}

func (e *Environment) StringDumpVariables() string { // For outputting them as a file of assignments
	result := ""
	for k, v := range e.Store {
		if v.access != ACCESS_CONSTANT {
			result = result + k + " = " + (v.obj).Inspect(ViewCharmLiteral) + "\n"
		}
	}
	return result
}

func (e *Environment) String() string { // For outputting them as a file of assignments
	result := ""
	for k, v := range e.Store {
			result = result + k + " = " + (v.obj).Inspect(ViewCharmLiteral) + ", "
	}
	if e.Ext != nil {
		result = result + "\n    + {" + e.Ext.String() + "}"
	}
	return result
}

func (e *Environment) Exists(name string) bool {
	_, ok := e.Store[name]
	if ok || e.Ext == nil { return ok }
	return e.Ext.Exists(name)
}

// Variable assumed to exist, and type check to have been done.
func (e *Environment) UpdateVar(name string, val Object) {
	storage, ok := e.Store[name]
	if ok {
		if (storage.access == ACCESS_PUBLIC || storage.access == ACCESS_PRIVATE) {
			e.Pending[name] = val 
			return
		} 
		e.Store[name] = Storage{val, e.Store[name].access, e.Store[name].VarType}
		return
	}
	e.Ext.UpdateVar(name, val)
}



func (e *Environment) getAccess(name string) AccessType {
	_, ok := e.Store[name]
	if ok || e.Ext == nil { return e.Store[name].access }
	return e.Ext.getAccess(name)
}

func (e *Environment) Set(name string, val Object) Object {
	storage, ok := e.Store[name]
	if ok && (storage.access == ACCESS_PUBLIC || storage.access == ACCESS_PRIVATE) {
		e.Pending[name] = val 
		return val
	} 
	e.Store[name] = Storage{val, e.Store[name].access, e.Store[name].VarType}
	return val
}

func (e *Environment) HardSet(name string, val Object) Object {
	e.Store[name] = Storage{val, e.Store[name].access, e.Store[name].VarType}
	return val
}


func (e *Environment) InitializeVariable(name string, val Object, ty string) Object {
	e.Store[name] = Storage{val, ACCESS_PUBLIC, ty}
	return val
}

func (e *Environment) InitializePrivate(name string, val Object, ty string) Object {
	e.Store[name] = Storage{val, ACCESS_PRIVATE, ty}
	return val
}

func (e *Environment) InitializeConstant(name string, val Object) Object {
	e.Store[name] = Storage{val, ACCESS_CONSTANT, TrueType(val)}
	return val
}

func (e *Environment) IsConstant(name string) bool {
	return e.Store[name].access == ACCESS_CONSTANT
}

func (e *Environment) IsPrivate(name string) bool {
	return e.Store[name].access == ACCESS_PRIVATE
}