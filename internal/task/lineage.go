package task

import (
	"github.com/example/aac-block-masonry-admission-closure/internal/domain"
)

// NodeKind classifies a material lineage node.
type NodeKind string

const (
	NodeRawBatch NodeKind = "raw_batch"
	NodeMixPan   NodeKind = "mix_pan"
	NodeBody     NodeKind = "body"
	NodeBlock    NodeKind = "block"
	NodeSample   NodeKind = "sample"
	NodeOffcut   NodeKind = "offcut"
	NodeWaste    NodeKind = "waste"
)

// EdgeType classifies a lineage transformation edge.
type EdgeType string

const (
	EdgeDose    EdgeType = "dose"
	EdgeMix     EdgeType = "mix"
	EdgePour    EdgeType = "pour"
	EdgeCut     EdgeType = "cut"
	EdgeSample  EdgeType = "sample"
	EdgeOffcut  EdgeType = "offcut"
	EdgeReclaim EdgeType = "reclaim"
)

// MaterialNode is an append-only node in the material lineage. Once written a
// node must never be overwritten or deleted.
type MaterialNode struct {
	ID         string             `json:"id"`
	Kind       NodeKind           `json:"kind"`
	MassGrams  int64              `json:"mass_grams"`
	Generation domain.Generation  `json:"generation"`
	SourceOp   domain.OperationID `json:"source_op"`
	Disposed   bool               `json:"disposed"`
}

// LineageEdge is an append-only directed edge with a unique parent constraint.
type LineageEdge struct {
	Parent     string            `json:"parent"`
	Child      string            `json:"child"`
	Type       EdgeType          `json:"type"`
	Evidence   string            `json:"evidence,omitempty"`
	Generation domain.Generation `json:"generation"`
}

// Graph is the in-memory lineage graph used for cycle and multi-parent
// detection before persistence. It only ever grows. The graph is a directed
// acyclic graph: mix pans legitimately aggregate several raw batches, so
// multiple parents are permitted for mix-pan nodes, while finished blocks,
// samples, offcuts and bodies enforce a single owner via the unique-parent
// invariant.
type Graph struct {
	nodes map[string]MaterialNode
	edges map[string]LineageEdge
	// children indexes parent -> child ids; parents indexes child -> parent ids.
	children map[string][]string
	parents  map[string][]string
}

// NewGraph returns an empty lineage graph.
func NewGraph() *Graph {
	return &Graph{
		nodes:    make(map[string]MaterialNode),
		edges:    make(map[string]LineageEdge),
		children: make(map[string][]string),
		parents:  make(map[string][]string),
	}
}

// AddNode inserts a node, rejecting duplicate ids.
func (g *Graph) AddNode(n MaterialNode) error {
	if _, ok := g.nodes[n.ID]; ok {
		return domain.Newf(domain.CodeDuplicateBody, "duplicate node %s", n.ID)
	}
	g.nodes[n.ID] = n
	return nil
}

// edgeKey builds a unique key for a parent-child edge.
func edgeKey(parent, child string) string { return parent + "\x00" + child }

// AddEdge inserts a directed edge, rejecting cycles. For nodes whose kind
// requires a single owner (block, sample, offcut, body) it also rejects a
// second, different parent as MULTIPLE_PARENT.
func (g *Graph) AddEdge(e LineageEdge) error {
	if _, ok := g.nodes[e.Parent]; !ok {
		return domain.Newf(domain.CodeInvalidArgument, "unknown parent %s", e.Parent)
	}
	if _, ok := g.nodes[e.Child]; !ok {
		return domain.Newf(domain.CodeInvalidArgument, "unknown child %s", e.Child)
	}
	// Reject a cycle: if the parent already descends from the child, adding this
	// edge closes a loop.
	if g.reaches(e.Parent, e.Child) {
		return domain.Newf(domain.CodeLineageCycle, "edge %s->%s would create a cycle", e.Parent, e.Child)
	}
	// Enforce the single-owner invariant for kinds that cannot be mixed.
	if soleOwner(g.nodes[e.Child].Kind) {
		for _, p := range g.parents[e.Child] {
			if p != e.Parent {
				return domain.Newf(domain.CodeMultipleParent, "child %s already has parent %s", e.Child, p)
			}
		}
	}
	key := edgeKey(e.Parent, e.Child)
	if _, exists := g.edges[key]; exists {
		return domain.Newf(domain.CodeDuplicateBody, "duplicate edge %s->%s", e.Parent, e.Child)
	}
	g.edges[key] = e
	g.children[e.Parent] = append(g.children[e.Parent], e.Child)
	g.parents[e.Child] = append(g.parents[e.Child], e.Parent)
	return nil
}

// soleOwner reports whether a node kind enforces a single parent (unique owner).
func soleOwner(k NodeKind) bool {
	switch k {
	case NodeBlock, NodeSample, NodeOffcut, NodeBody:
		return true
	default:
		return false
	}
}

// reaches reports whether walking parent edges upward from node reaches target.
func (g *Graph) reaches(node, target string) bool {
	return g.reachesDFS(node, target, make(map[string]bool))
}

func (g *Graph) reachesDFS(node, target string, seen map[string]bool) bool {
	if node == target {
		return true
	}
	if seen[node] {
		return false
	}
	seen[node] = true
	for _, p := range g.parents[node] {
		if g.reachesDFS(p, target, seen) {
			return true
		}
	}
	return false
}

// Nodes returns the current node snapshot.
func (g *Graph) Nodes() map[string]MaterialNode { return g.nodes }

// Edges returns the current edge snapshot.
func (g *Graph) Edges() map[string]LineageEdge { return g.edges }

// ParentsOf returns the direct parent ids of a child in insertion order.
func (g *Graph) ParentsOf(child string) []string { return g.parents[child] }
