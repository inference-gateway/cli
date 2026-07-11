// Package tree wraps charm.land/lipgloss/v2/tree so packages outside the styling
// layer (notably internal/domain formatters) can render rounded-connector trees
// without importing lipgloss directly — the same leaf-package convention as
// internal/ui/styles/{colors,icons}. Output is plain text; connectors only.
package tree

import gloss "charm.land/lipgloss/v2/tree"

// Tree is a node in a connector tree. Start with Root; the zero value is not usable.
type Tree struct{ t *gloss.Tree }

// Root starts a new tree with the given label.
func Root(label string) *Tree { return &Tree{t: gloss.Root(label)} }

// Rounded switches connectors to rounded glyphs (├──/╰──); children inherit it.
func (t *Tree) Rounded() *Tree {
	t.t.Enumerator(gloss.RoundedEnumerator)
	return t
}

// Child appends children; each may be a string or a nested *Tree.
func (t *Tree) Child(children ...any) *Tree {
	for i, c := range children {
		if sub, ok := c.(*Tree); ok {
			children[i] = sub.t
		}
	}
	t.t.Child(children...)
	return t
}

// String renders the tree to plain text.
func (t *Tree) String() string { return t.t.String() }
