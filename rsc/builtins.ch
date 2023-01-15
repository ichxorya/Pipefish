def

file = struct(filename string, contents list)

range(p pair) : builtin "range"
len(t type) : builtin "len_of_type" // Note that this is implemented in the evaluator
file(s string) : builtin "init_file"
codepoint(s string) : builtin "codepoint"
(S struct) with (p pair) : builtin "add_pair_to_struct"
(L list) with (p pair) : builtin "add_pair_to_list"
(m map) with (p pair) : builtin "add_pair_to_map" 
(S struct) with (t tuple) : builtin "add_tuple_to_struct"
(L list) with (t tuple) : builtin "add_tuple_to_list"
(m map) with (t tuple) : builtin "add_tuple_to_map"
rune(i int) : builtin "rune"
charm(s single) : builtin "charm_any"
charm(s tuple) : builtin "charm_any"
tuple(s single) : builtin "single_to_tuple"
tuple(t tuple) : builtin "tuple_to_tuple"
spread(L list) : builtin "spread_list"
spread(S set) : builtin "spread_set"
(s single) in (L list) : builtin "single_in_list"
(s single) in (S set) : builtin "single_in_set"
(s single) in (T tuple) : builtin "single_in_tuple"
map (s set) : builtin "set_to_map"
map (t tuple) : builtin "tuple_to_map"
index (t type) by (i int) : builtin "index_int_of_type" // Note that this is implemented in the evaluator.
index (S struct) by (l label) : builtin "index_label_of_struct"
index (L list) by (i int) : builtin "index_int_of_list"
index (S string) by (i int) : builtin "index_int_of_string"
index (M map) by (i int) : builtin "index_any_of_map"
index (M map) by (s string) : builtin "index_any_of_map"
index (M map) by (b bool) : builtin "index_any_of_map"
index (M map) by (f float64) : builtin "index_any_of_map"
index (M map) by (la label) : builtin "index_any_of_map"
index (M map) by (t type) : builtin "index_any_of_map"
index (p pair) by (i int) : builtin "index_int_of_pair"
index (T tuple) by (i int) : builtin "index_int_of_tuple"
index (L list) by (p pair) : builtin "index_pair_of_list"
index (S string) by (p pair) : builtin "index_pair_of_string"
index (T tuple) by (p pair) : builtin "index_pair_of_tuple"
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
string(x int) : builtin "int_to_string"
string(x bool) : builtin "bool_to_string"
string(x float64) : builtin "float_to_string"
string(x string) : builtin "string_to_string"
string(x type) : builtin "type_to_string"
int(x string) : builtin "string_to_int"
float64(x string) : builtin "string_to_float"
int(x float64) : builtin "float_to_int"
float64(x int) : builtin "int_to_float"
bool(x int) : builtin "int_to_bool"
bool(x string) : builtin "string_to_bool"
bool(x list) : builtin "list_to_bool"
bool(x set) : builtin "set_to_bool"
bool(x map) : builtin "map_to_bool"
type(x single) : builtin "type"
type(x tuple) : builtin "type_of_tuple"
error(x string) : builtin "make_error"