package config

/*type Node string
type Queue []*Node

func (q *Queue) Push(n *Node) {
	*q = append(*q, n)
}

func (q *Queue) Pop() (n *Node) {
	n = (*q)[0]
	*q = (*q)[1:]
	return
}

func (q *Queue) Len() int {
	return len(*q)
}
*/
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
