package cache

// lruNode is a node in a doubly-linked list for LRU tracking.
type lruNode struct {
	key  string
	prev *lruNode
	next *lruNode
}

// lruList is a doubly-linked list with an index map for O(1) LRU operations.
type lruList struct {
	head *lruNode
	tail *lruNode
	idx  map[string]*lruNode
}

func newLRUList() *lruList {
	return &lruList{idx: make(map[string]*lruNode)}
}

// Touch moves a key to the front (most recent). No-op if key not found.
func (l *lruList) Touch(key string) {
	node, ok := l.idx[key]
	if !ok {
		return
	}
	l.detach(node)
	l.pushFront(node)
}

// Add inserts a new key at the front. If the key already exists, it is moved to front.
func (l *lruList) Add(key string) {
	if node, ok := l.idx[key]; ok {
		l.detach(node)
		l.pushFront(node)
		return
	}
	node := &lruNode{key: key}
	l.idx[key] = node
	l.pushFront(node)
}

// Evict removes the tail (oldest) entry and returns its key. Returns "" if empty.
func (l *lruList) Evict() string {
	if l.tail == nil {
		return ""
	}
	node := l.tail
	l.detach(node)
	delete(l.idx, node.key)
	return node.key
}

// Remove deletes a specific key from the list. No-op if key not found.
func (l *lruList) Remove(key string) {
	node, ok := l.idx[key]
	if !ok {
		return
	}
	l.detach(node)
	delete(l.idx, node.key)
}

// Len returns the number of entries.
func (l *lruList) Len() int {
	return len(l.idx)
}

func (l *lruList) pushFront(node *lruNode) {
	node.prev = nil
	node.next = l.head
	if l.head != nil {
		l.head.prev = node
	}
	l.head = node
	if l.tail == nil {
		l.tail = node
	}
}

func (l *lruList) detach(node *lruNode) {
	if node.prev != nil {
		node.prev.next = node.next
	} else {
		l.head = node.next
	}
	if node.next != nil {
		node.next.prev = node.prev
	} else {
		l.tail = node.prev
	}
	node.prev = nil
	node.next = nil
}
