def

twice(x string) : x + ", " + x

twice(x int) : 2 * x

twice(b bool) :
    b : "That's as true as things get!"
    else : "That's as false as things get!"

twice(t single) :
    error "I don't know how to double that!"

twice(t tuple) :
	t, t

