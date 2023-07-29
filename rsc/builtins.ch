def

// This is implemented in the evaluator, so all this line does is enforce the syntax.
for (indexName ast) over (R) do (__f func) to (x tuple) : builtin "for_loop" 

// This is not a builtin but should be, for performance.
while (p) do (__f) to (z single) :
    p z : while p do __f to __f z     // TODO --- fix stupid namespace conflicts.
    else : z

while (p) do (__f) to (z tuple) :
    p z : while p do __f to __f z
    else : z

tail(L list) :
    L == [] :
        []
    else :
        L[1::len(L)]

range(p pair) : builtin "range"
len(t type) : builtin "len_of_type" 
codepoint(s string) : builtin "codepoint"
(S struct) with (p pair) : builtin "add_pair_to_struct"
(L list) with (p pair) : builtin "add_pair_to_list"
(m map) with (p pair) : builtin "add_pair_to_map" 
(S struct) with (t tuple) : builtin "add_tuple_to_struct"

// The evaluator will change this at runtime to the appropriate long-term constructor.
(t type) with (T tuple) : builtin "long_form_constructor" 

(L list) with (t tuple) : builtin "add_tuple_to_list"
(m map) with (t tuple) : builtin "add_tuple_to_map"
rune(i int) : builtin "rune"
literal(s single) : builtin "charm_literal"
literal(s tuple) : builtin "charm_literal"
tuple(s single) : builtin "single_to_tuple"
tuple(t tuple) : builtin "tuple_to_tuple"
tuplify(L list) : builtin "spread_list"
tuplify(S set) : builtin "spread_set"
(s single) in (L list) : builtin "single_in_list"
(s single) in (S set) : builtin "single_in_set"
(s single) in (T tuple) : builtin "single_in_tuple"
map (s set) : builtin "set_to_map"
map (t tuple) : builtin "tuple_to_map"
index (t type) by (i int): builtin "index_int_of_type" 
index (S struct) by (l label) : builtin "index_label_of_struct"
index (L list) by (i int): builtin "index_int_of_list"
index (S string) by (i int): builtin "index_int_of_string"
index (M map) by (i int): builtin "index_any_of_map"
index (M map) by (a float64): builtin "index_any_of_map"
index (M map) by (a string) : builtin "index_any_of_map"
index (M map) by (a label): builtin "index_any_of_map"
index (M map) by (a type): builtin "index_any_of_map"
index (M map) by (a bool): builtin "index_any_of_map"
keys (M map): builtin "keys_of_map"
keys (S struct) : builtin "keys_of_struct"
keys (t type) : builtin "keys_of_type"
index (p pair) by (i int): builtin "index_int_of_pair"
index (T tuple) by (i int): builtin "index_int_of_tuple"
index (L list) by (p pair): builtin "index_pair_of_list"
index (S string) by (p pair): builtin "index_pair_of_string"
index (T tuple) by (p pair): builtin "index_pair_of_tuple"
(x single) :: (y single) : builtin "make_pair"
(x int) < (y int) : builtin "< int"
(x int) <= (y int) : builtin "<= int"
(x int) > (y int) : builtin "> int"
(x int) >= (y int) : builtin ">= int"
(x string) + (y string) : builtin "add_strings"
(x list) + (y list) : builtin "add_lists"
(x set) + (y set) : builtin "add_sets"
(x int) + (y int) : builtin "add_integers"
- (x int) : builtin "negate_integer"
(x int) - (y int) : builtin "subtract_integers"
(x int) * (y int) : builtin "multiply_integers"
(x int) % (y int) : builtin "modulo_integers"
(x int) / (y int) : builtin "divide_integers"
(x float64) < (y float64) : builtin "< float64"
(x float64) <= (y float64) : builtin "<= float64"
(x float64) > (y float64) : builtin "> float64"
(x float64) >= (y float64) : builtin ">= float64"
(x float64) + (y float64) : builtin "add_floats"
- (x float64) : builtin "negate_float"
(x float64) - (y float64) : builtin "subtract_floats"
(x float64) * (y float64) : builtin "multiply_floats"
(x float64) / (y float64) : builtin "divide_floats"
len(x string) : builtin "len_string"
len(x list)	: builtin "len_list"
arity(x tuple) : builtin "arity_tuple"
string(x single) : builtin "single_to_string"
int(x string) : builtin "string_to_int"
float64(x string) : builtin "string_to_float"
int(x float64) : builtin "float_to_int"
float64(x int) : builtin "int_to_float"
type(x single) : builtin "type"
type(x tuple) : builtin "type_of_tuple"
error(x string) : builtin "make_error"