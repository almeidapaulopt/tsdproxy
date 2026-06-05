// SPDX-FileCopyrightText: 2026 Paulo Almeida <almeidapaulopt@gmail.com>
// SPDX-License-Identifier: MIT

package config

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/rs/zerolog"
	"gopkg.in/yaml.v3"
)

type keyIssue struct {
	Original    string
	Suggestions []string
	Line        int
	Column      int
}

type structInfo struct {
	fieldType reflect.Type
	canonical string
	kind      reflect.Kind
}

type keyLookup struct {
	canonical map[string]structInfo
	byType    map[reflect.Type]map[string]struct{}
	rootType  reflect.Type
}

// buildKeyLookup walks the type of root using reflect and indexes every
// exported yaml-tagged field, recursing through nested structs, pointer
// structs, slices of structs, and maps whose values are (pointer to)
// struct. The returned lookup is safe to reuse for one unmarshal pass.
func buildKeyLookup(root any) *keyLookup {
	lookup := &keyLookup{
		canonical: make(map[string]structInfo),
		byType:    make(map[reflect.Type]map[string]struct{}),
	}
	if root == nil {
		return lookup
	}
	t := reflect.TypeOf(root)
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return lookup
	}
	lookup.rootType = t
	lookup.walkType(t)
	return lookup
}

func (k *keyLookup) walkType(t reflect.Type) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		if t.Kind() == reflect.Map || t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
			k.walkType(t.Elem())
		}
		return
	}
	if _, seen := k.byType[t]; seen {
		return
	}
	valid := make(map[string]struct{})
	k.byType[t] = valid

	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		name, _ := parseYAMLTag(tag)
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			k.walkType(f.Type)
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		normalized := normalizeKey(name)
		if _, exists := k.canonical[normalized]; !exists {
			k.canonical[normalized] = structInfo{
				canonical: name,
				kind:      f.Type.Kind(),
				fieldType: f.Type,
			}
		}
		valid[name] = struct{}{}
		k.walkType(f.Type)
	}
}

func parseYAMLTag(tag string) (name string, opts []string) {
	if tag == "" {
		return "", nil
	}
	parts := strings.Split(tag, ",")
	name = parts[0]
	if len(parts) > 1 {
		opts = parts[1:]
	}
	return name, opts
}

func normalizeKey(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "-", "")
	return s
}

func (k *keyLookup) find(input string) (canonical string, kind reflect.Kind, found bool) {
	info, ok := k.canonical[normalizeKey(input)]
	if !ok {
		return "", reflect.Invalid, false
	}
	return info.canonical, info.kind, true
}

func (k *keyLookup) fieldTypeByCanonical(typ reflect.Type, canonicalName string) (reflect.Type, bool) {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return nil, false
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("yaml")
		name, _ := parseYAMLTag(tag)
		if name == "-" {
			continue
		}
		if f.Anonymous && name == "" {
			if nested, ok := k.fieldTypeByCanonical(f.Type, canonicalName); ok {
				return nested, true
			}
			continue
		}
		if name == "" {
			name = strings.ToLower(f.Name)
		}
		if name == canonicalName {
			return f.Type, true
		}
	}
	return nil, false
}

func (k *keyLookup) validFields(typ reflect.Type) []string {
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	set, ok := k.byType[typ]
	if !ok {
		return nil
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// suggest returns up to three values from valid that are closest to
// input by Levenshtein distance (case-insensitive). A candidate is
// included when its distance is at most 3 or it shares a
// case-insensitive prefix of length 3 or more. Results are ordered by
// distance then alphabetically.
func suggest(input string, valid []string) []string {
	if len(valid) == 0 {
		return nil
	}
	lower := strings.ToLower(input)
	type cand struct {
		name string
		dist int
	}
	cands := make([]cand, 0, len(valid))
	for _, v := range valid {
		lv := strings.ToLower(v)
		d := levenshtein(lower, lv)
		prefix := commonPrefixLen(lower, lv)
		if d <= 3 || prefix >= 3 { //nolint:mnd // suggestion thresholds per spec
			cands = append(cands, cand{name: v, dist: d})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].dist != cands[j].dist {
			return cands[i].dist < cands[j].dist
		}
		return cands[i].name < cands[j].name
	})
	if len(cands) > 3 { //nolint:mnd // max suggestions per spec
		cands = cands[:3] //nolint:mnd // max suggestions per spec
	}
	out := make([]string, 0, len(cands))
	for _, c := range cands {
		out = append(out, c.name)
	}
	return out
}

func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			m := del
			if ins < m {
				m = ins
			}
			if sub < m {
				m = sub
			}
			curr[j] = m
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func commonPrefixLen(a, b string) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// normalizeNodeKeys walks the YAML node tree rooted at node and rewrites
// every mapping key to its canonical (camelCase) form when a
// case-insensitive match exists in lookup. Keys with no match are
// returned as keyIssue entries together with suggestions scoped to the
// struct type expected at the offending location.
//
// node is typically the DocumentNode returned by yaml.Unmarshal into a
// *yaml.Node; DocumentNode and zero-value nodes are handled
// transparently. Rewrites are performed in place on the tree, so a
// subsequent (*yaml.Node).Decode(out) sees the canonical keys.
func normalizeNodeKeys(node *yaml.Node, lookup *keyLookup, log zerolog.Logger) []keyIssue {
	if node == nil || lookup == nil || lookup.rootType == nil {
		return nil
	}
	logger := log.With().Str("module", "config").Logger()
	var issues []keyIssue
	walkNode(node, lookup.rootType, lookup, &issues, logger, "")
	return issues
}

func walkNode(node *yaml.Node, typ reflect.Type, lookup *keyLookup, issues *[]keyIssue, log zerolog.Logger, path string) {
	if node == nil {
		return
	}
	if node.Kind == yaml.DocumentNode {
		for _, child := range node.Content {
			walkNode(child, typ, lookup, issues, log, path)
		}
		return
	}
	typ = indirectType(typ)
	switch {
	case node.Kind != yaml.MappingNode:
		walkSequence(node, typ, lookup, issues, log, path)
	case typ.Kind() == reflect.Map:
		walkMapValues(node, typ, lookup, issues, log, path)
	case typ.Kind() == reflect.Struct:
		walkStructMapping(node, typ, lookup, issues, log, path)
	}
}

func indirectType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}

func walkSequence(node *yaml.Node, typ reflect.Type, lookup *keyLookup, issues *[]keyIssue, log zerolog.Logger, path string) {
	if node.Kind != yaml.SequenceNode {
		return
	}
	elemType := typ
	if elemType.Kind() == reflect.Slice || elemType.Kind() == reflect.Array {
		elemType = elemType.Elem()
	}
	for _, item := range node.Content {
		walkNode(item, elemType, lookup, issues, log, path+"[]")
	}
}

func walkMapValues(node *yaml.Node, typ reflect.Type, lookup *keyLookup, issues *[]keyIssue, log zerolog.Logger, path string) {
	valueType := typ.Elem()
	for i := 1; i < len(node.Content); i += 2 {
		keyNode := node.Content[i-1]
		valNode := node.Content[i]
		walkNode(valNode, valueType, lookup, issues, log, path+"."+keyNode.Value)
	}
}

func walkStructMapping(node *yaml.Node, typ reflect.Type, lookup *keyLookup, issues *[]keyIssue, log zerolog.Logger, path string) {
	for i := 0; i < len(node.Content); i += 2 {
		keyNode := node.Content[i]
		valNode := node.Content[i+1]
		if keyNode.Kind != yaml.ScalarNode {
			continue
		}
		processStructField(keyNode, valNode, typ, lookup, issues, log, path)
	}
}

func processStructField(keyNode, valNode *yaml.Node, typ reflect.Type, lookup *keyLookup, issues *[]keyIssue, log zerolog.Logger, path string) {
	original := keyNode.Value
	canonical, _, found := lookup.find(original)
	if !found {
		*issues = append(*issues, keyIssue{
			Line:        keyNode.Line,
			Column:      keyNode.Column,
			Original:    original,
			Suggestions: suggest(original, lookup.validFields(typ)),
		})
		return
	}
	if canonical != original {
		log.Debug().
			Str("from", original).
			Str("to", canonical).
			Str("path", path).
			Msg("normalized YAML key")
		keyNode.Value = canonical
	}
	fieldType, ok := lookup.fieldTypeByCanonical(typ, canonical)
	if !ok {
		return
	}
	childPath := canonical
	if path != "" {
		childPath = path + "." + canonical
	}
	recurseFieldValue(valNode, fieldType, lookup, issues, log, childPath)
}

func recurseFieldValue(valNode *yaml.Node, fieldType reflect.Type, lookup *keyLookup, issues *[]keyIssue, log zerolog.Logger, path string) {
	ft := indirectType(fieldType)
	switch ft.Kind() {
	case reflect.Struct, reflect.Map:
		walkNode(valNode, ft, lookup, issues, log, path)
	case reflect.Slice, reflect.Array:
		walkNode(valNode, ft.Elem(), lookup, issues, log, path)
	}
}

func quoteList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return fmt.Sprintf("%q", items[0])
	}
	var b strings.Builder
	for i, s := range items {
		if i == len(items)-1 {
			b.WriteString(" or ")
		} else if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%q", s)
	}
	return b.String()
}
