def

while (p) do (f) to (x tuple) :
    p x : while p do f to f x
    else : x

for (n) do (f) to (x tuple) :
    (while unfinished do loop to 0, x)[1::arity(x) + 1]
given :
    unfinished = func(i int, x tuple) : i < n
    loop = func(i int, x tuple) : i + 1, f x

mergesort (L list) :
    len L <= 1 : L
    else :
        mergeSorted(mergesort(L[0 :: len(L)/2]), mergesort(L[len(L)/2 :: len(L)]))

mergeSorted(A, B) :
    (while condition do action to [], A, B) [0]
given :
    condition = func(output, A, B) : A or B
    action = func (output, A, B) :
        not A : output + B, [], []
        not B : output + A, [], []
        A[0] < B[0] : output + [A[0]], tail(A), B
        else : output + [B[0]], A, tail(B)

tail (L) : 
    len(L) <= 1 : []
    else : L[1::len L]