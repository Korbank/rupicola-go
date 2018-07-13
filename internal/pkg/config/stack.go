package config

type stack []string

func (q *stack) push(n string) {
	*q = append(*q, n)
}

func (q *stack) pop() (n string) {
	x := q.len() - 1
	n = (*q)[x]
	*q = (*q)[:x]
	return
}
func (q *stack) len() int {
	return len(*q)
}
