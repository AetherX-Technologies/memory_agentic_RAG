package retrieval

import "container/heap"

// SearchNode represents a node in the priority queue for hierarchical search.
type SearchNode struct {
	ID    string
	Score float64
	Depth int
}

// priorityQueue implements heap.Interface for max-heap by Score.
type priorityQueue []*SearchNode

func (pq priorityQueue) Len() int            { return len(pq) }
func (pq priorityQueue) Less(i, j int) bool  { return pq[i].Score > pq[j].Score } // max-heap
func (pq priorityQueue) Swap(i, j int)       { pq[i], pq[j] = pq[j], pq[i] }

func (pq *priorityQueue) Push(x interface{}) {
	*pq = append(*pq, x.(*SearchNode))
}

func (pq *priorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // avoid memory leak
	*pq = old[:n-1]
	return item
}

// PriorityQueue wraps the heap-based priority queue with a clean API.
type PriorityQueue struct {
	pq priorityQueue
}

// NewPriorityQueue creates an empty max-heap priority queue.
func NewPriorityQueue() *PriorityQueue {
	pq := &PriorityQueue{}
	heap.Init(&pq.pq)
	return pq
}

// Push adds a node to the queue.
func (q *PriorityQueue) Push(node *SearchNode) {
	heap.Push(&q.pq, node)
}

// Pop removes and returns the highest-score node.
func (q *PriorityQueue) Pop() *SearchNode {
	return heap.Pop(&q.pq).(*SearchNode)
}

// Len returns the number of nodes in the queue.
func (q *PriorityQueue) Len() int {
	return q.pq.Len()
}
