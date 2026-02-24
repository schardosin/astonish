package browser

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/proto"
)

// SnapshotOptions controls how the accessibility tree is formatted.
type SnapshotOptions struct {
	InteractiveOnly bool   // Only show interactive elements
	Compact         bool   // Remove unnamed structural elements
	MaxDepth        int    // Max tree depth (0 = unlimited)
	Selector        string // CSS selector to scope to a subtree
	Frame           string // iframe CSS selector to scope into
	MaxChars        int    // Truncation limit (default: 80000)
}

// SnapshotResult is returned by TakeSnapshot.
type SnapshotResult struct {
	Text     string // Formatted accessibility tree text
	RefCount int    // Number of refs assigned
	URL      string // Current page URL
	Title    string // Current page title
}

// interactiveRoles is the set of ARIA roles considered interactive.
var interactiveRoles = map[string]bool{
	"button":           true,
	"link":             true,
	"textbox":          true,
	"searchbox":        true,
	"checkbox":         true,
	"radio":            true,
	"combobox":         true,
	"listbox":          true,
	"option":           true,
	"menuitem":         true,
	"menuitemcheckbox": true,
	"menuitemradio":    true,
	"tab":              true,
	"switch":           true,
	"slider":           true,
	"spinbutton":       true,
	"treeitem":         true,
	"gridcell":         true,
	"row":              true,
	"columnheader":     true,
	"rowheader":        true,
	"scrollbar":        true,
	"meter":            true,
	"progressbar":      true,
}

// TakeSnapshot captures an accessibility tree snapshot of the page,
// formats it as LLM-friendly text with ref IDs, and populates the RefMap.
func TakeSnapshot(pg *rod.Page, refs *RefMap, opts SnapshotOptions) (*SnapshotResult, error) {
	if opts.MaxChars == 0 {
		opts.MaxChars = 80000
	}

	// Scope to an iframe if requested
	targetPage := pg
	if opts.Frame != "" {
		frameEl, err := pg.Element(opts.Frame)
		if err != nil {
			return nil, fmt.Errorf("iframe %q not found: %w", opts.Frame, err)
		}
		framePage, err := frameEl.Frame()
		if err != nil {
			return nil, fmt.Errorf("could not access iframe %q: %w", opts.Frame, err)
		}
		targetPage = framePage
	}

	// Scope to a CSS selector if requested
	var scopeNodeID proto.DOMBackendNodeID
	if opts.Selector != "" {
		el, err := targetPage.Element(opts.Selector)
		if err != nil {
			return nil, fmt.Errorf("selector %q not found: %w", opts.Selector, err)
		}
		desc, err := el.Describe(0, false)
		if err != nil {
			return nil, fmt.Errorf("could not describe element for %q: %w", opts.Selector, err)
		}
		scopeNodeID = desc.BackendNodeID
	}

	// Get the full accessibility tree
	req := proto.AccessibilityGetFullAXTree{}
	if opts.MaxDepth > 0 {
		depth := opts.MaxDepth
		req.Depth = &depth
	}
	res, err := req.Call(targetPage)
	if err != nil {
		return nil, fmt.Errorf("failed to get accessibility tree: %w", err)
	}

	if len(res.Nodes) == 0 {
		return takeDOMSnapshot(pg, refs, opts)
	}

	// Count how many nodes are actually visible (not ignored).
	// Some sites (CNN, etc.) use div-heavy DOMs with no ARIA attributes,
	// causing Chrome to mark 99%+ of the AX tree as ignored.
	// When the tree is this sparse, a DOM-based fallback produces much
	// better results.
	visible := 0
	for _, n := range res.Nodes {
		if !n.Ignored {
			visible++
		}
	}
	if visible < 10 {
		return takeDOMSnapshot(pg, refs, opts)
	}

	// Build a parent→children map and a nodeID→node lookup
	nodeMap := make(map[proto.AccessibilityAXNodeID]*proto.AccessibilityAXNode)
	children := make(map[proto.AccessibilityAXNodeID][]proto.AccessibilityAXNodeID)

	for i := range res.Nodes {
		n := res.Nodes[i]
		nodeMap[n.NodeID] = n
		if n.ParentID != "" {
			children[n.ParentID] = append(children[n.ParentID], n.NodeID)
		}
	}

	// Find root(s): nodes with no parent or parent not in the map
	var roots []proto.AccessibilityAXNodeID
	for _, n := range res.Nodes {
		if n.ParentID == "" || nodeMap[n.ParentID] == nil {
			roots = append(roots, n.NodeID)
		}
	}

	// If scoping to a specific node, find it in the tree
	if scopeNodeID != 0 {
		for _, n := range res.Nodes {
			if n.BackendDOMNodeID == proto.DOMBackendNodeID(scopeNodeID) {
				roots = []proto.AccessibilityAXNodeID{n.NodeID}
				break
			}
		}
	}

	// Reset refs for a fresh snapshot
	refs.Reset()

	// Format the tree
	var sb strings.Builder
	for _, rootID := range roots {
		formatNode(&sb, rootID, 0, nodeMap, children, refs, opts)
	}

	text := sb.String()

	// Truncate if needed
	if len(text) > opts.MaxChars {
		text = text[:opts.MaxChars] + "\n... (truncated)"
	}

	// Get page info
	info, _ := targetPage.Info()
	url := ""
	title := ""
	if info != nil {
		url = info.URL
		title = info.Title
	}

	return &SnapshotResult{
		Text:     text,
		RefCount: refs.Count(),
		URL:      url,
		Title:    title,
	}, nil
}

// formatNode recursively formats an AX node into the string builder.
func formatNode(
	sb *strings.Builder,
	nodeID proto.AccessibilityAXNodeID,
	depth int,
	nodeMap map[proto.AccessibilityAXNodeID]*proto.AccessibilityAXNode,
	children map[proto.AccessibilityAXNodeID][]proto.AccessibilityAXNodeID,
	refs *RefMap,
	opts SnapshotOptions,
) {
	node := nodeMap[nodeID]
	if node == nil {
		return
	}

	// Ignored nodes are not rendered, but their children may contain visible
	// content (e.g., NYT wraps the entire page in an ignored "none" node).
	// Always recurse into children.
	if node.Ignored {
		for _, childID := range children[nodeID] {
			formatNode(sb, childID, depth, nodeMap, children, refs, opts)
		}
		return
	}

	role := axValueStr(node.Role)
	name := axValueStr(node.Name)
	value := axValueStr(node.Value)

	// Skip nodes with the "none" or "generic" role if compact mode
	if opts.Compact && (role == "none" || role == "generic" || role == "GenericContainer") && name == "" {
		// Still process children
		for _, childID := range children[nodeID] {
			formatNode(sb, childID, depth, nodeMap, children, refs, opts)
		}
		return
	}

	// In interactive-only mode, skip non-interactive nodes (but recurse into children)
	isInteractive := interactiveRoles[role]
	if opts.InteractiveOnly && !isInteractive {
		for _, childID := range children[nodeID] {
			formatNode(sb, childID, depth, nodeMap, children, refs, opts)
		}
		return
	}

	// Skip the top-level "RootWebArea" or "WebArea" wrapper — just show children
	if depth == 0 && (role == "RootWebArea" || role == "WebArea") {
		for _, childID := range children[nodeID] {
			formatNode(sb, childID, depth, nodeMap, children, refs, opts)
		}
		return
	}

	// Assign a ref for interactive elements or elements with a name
	refTag := ""
	if isInteractive || (name != "" && role != "StaticText" && role != "InlineTextBox") {
		ref := refs.Add(RefEntry{
			BackendDOMNodeID: proto.DOMBackendNodeID(node.BackendDOMNodeID),
			Role:             role,
			Name:             name,
		})
		refTag = fmt.Sprintf("[%s] ", ref)
	}

	// Build the line
	indent := strings.Repeat("  ", depth)

	// Format: [ref] role "name" (extras)
	var line strings.Builder
	line.WriteString(indent)
	line.WriteString(refTag)
	line.WriteString(role)
	if name != "" {
		line.WriteString(fmt.Sprintf(" %q", name))
	}

	// Append extra properties
	var extras []string
	if value != "" {
		extras = append(extras, fmt.Sprintf("value=%q", value))
	}
	for _, prop := range node.Properties {
		if prop.Value == nil {
			continue
		}
		switch prop.Name {
		case "focused":
			if prop.Value.Value.Bool() {
				extras = append(extras, "focused")
			}
		case "disabled":
			if prop.Value.Value.Bool() {
				extras = append(extras, "disabled")
			}
		case "checked":
			v := prop.Value.Value.String()
			if v == "true" {
				extras = append(extras, "checked")
			} else if v == "mixed" {
				extras = append(extras, "checked=mixed")
			}
		case "expanded":
			if prop.Value.Value.Bool() {
				extras = append(extras, "expanded")
			} else {
				extras = append(extras, "collapsed")
			}
		case "selected":
			if prop.Value.Value.Bool() {
				extras = append(extras, "selected")
			}
		case "required":
			if prop.Value.Value.Bool() {
				extras = append(extras, "required")
			}
		case "readonly":
			if prop.Value.Value.Bool() {
				extras = append(extras, "readonly")
			}
		case "level":
			extras = append(extras, fmt.Sprintf("level=%s", prop.Value.Value.String()))
		}
	}
	if len(extras) > 0 {
		line.WriteString(" (")
		line.WriteString(strings.Join(extras, ", "))
		line.WriteString(")")
	}

	sb.WriteString(line.String())
	sb.WriteString("\n")

	// Recurse into children
	if opts.MaxDepth > 0 && depth >= opts.MaxDepth {
		if len(children[nodeID]) > 0 {
			sb.WriteString(indent)
			sb.WriteString("  ... (depth limit)\n")
		}
		return
	}

	for _, childID := range children[nodeID] {
		formatNode(sb, childID, depth+1, nodeMap, children, refs, opts)
	}
}

// axValueStr extracts the string from an AXValue.
func axValueStr(v *proto.AccessibilityAXValue) string {
	if v == nil {
		return ""
	}
	s := v.Value.Str()
	if s != "" {
		return s
	}
	return v.Value.String()
}

// domSnapshotRef is a single interactive element found by the JS walker.
type domSnapshotRef struct {
	Ref  string `json:"ref"`
	Tag  string `json:"tag"`
	Role string `json:"role"`
	Name string `json:"name"`
}

// takeDOMSnapshot uses JavaScript to walk the visible DOM and produce a
// structured text representation. This is the fallback when Chrome's AX tree
// is too sparse (common on sites with non-semantic div-heavy DOMs).
// Interactive elements are tagged with data-astonish-ref attributes so they
// can be resolved later via CSS selector.
func takeDOMSnapshot(pg *rod.Page, refs *RefMap, opts SnapshotOptions) (*SnapshotResult, error) {
	maxChars := opts.MaxChars
	if maxChars == 0 {
		maxChars = 80000
	}
	maxDepth := opts.MaxDepth
	if maxDepth == 0 {
		maxDepth = 50
	}
	interactiveOnly := opts.InteractiveOnly
	compact := opts.Compact

	// The JS walker produces a text tree and a list of interactive refs.
	// Each interactive element gets a data-astonish-ref attribute for later resolution.
	jsCode := fmt.Sprintf(`() => {
		const MAX_DEPTH = %d;
		const INTERACTIVE_ONLY = %t;
		const COMPACT = %t;
		const INTERACTIVE_TAGS = new Set(['A','BUTTON','INPUT','SELECT','TEXTAREA','DETAILS','SUMMARY']);
		const SKIP_TAGS = new Set(['SCRIPT','STYLE','NOSCRIPT','SVG','PATH','META','LINK']);
		const HEADING_TAGS = new Set(['H1','H2','H3','H4','H5','H6']);
		const SEMANTIC_TAGS = {'NAV':'navigation','MAIN':'main','HEADER':'banner','FOOTER':'contentinfo','ASIDE':'complementary','SECTION':'region','ARTICLE':'article','UL':'list','OL':'list','LI':'listitem','TABLE':'table','FORM':'form','IMG':'img'};

		function getRole(el) {
			const r = el.getAttribute && el.getAttribute('role');
			if (r) return r;
			const tag = el.tagName;
			if (HEADING_TAGS.has(tag)) return 'heading';
			if (SEMANTIC_TAGS[tag]) return SEMANTIC_TAGS[tag];
			switch(tag) {
				case 'A': return el.href ? 'link' : '';
				case 'BUTTON': return 'button';
				case 'INPUT': {
					const t = (el.type||'text').toLowerCase();
					if (t==='checkbox') return 'checkbox';
					if (t==='radio') return 'radio';
					if (t==='submit'||t==='button'||t==='reset') return 'button';
					if (t==='hidden') return '';
					return 'textbox';
				}
				case 'SELECT': return 'combobox';
				case 'TEXTAREA': return 'textbox';
				default: return '';
			}
		}

		function isVisible(el) {
			if (!el.offsetParent && el.tagName!=='BODY' && el.tagName!=='HTML') return false;
			const s = getComputedStyle(el);
			return s.display!=='none' && s.visibility!=='hidden';
		}

		function getName(el) {
			return el.getAttribute('aria-label') || el.getAttribute('title') || el.getAttribute('alt') || 
				(el.tagName==='INPUT'||el.tagName==='TEXTAREA' ? el.getAttribute('placeholder')||'' : '');
		}

		let lines = [];
		let refCounter = 0;
		let refList = [];

		function walk(node, depth) {
			if (depth > MAX_DEPTH) return;
			if (node.nodeType === 3) {
				const t = node.textContent.trim();
				if (t && !INTERACTIVE_ONLY) {
					const display = t.length > 200 ? t.substring(0,200) + '...' : t;
					lines.push('  '.repeat(depth) + JSON.stringify(display));
				}
				return;
			}
			if (node.nodeType !== 1) return;
			const el = node;
			if (SKIP_TAGS.has(el.tagName)) return;
			if (el.tagName !== 'BODY' && !isVisible(el)) return;

			const role = getRole(el);
			const name = getName(el);
			const isInteractive = INTERACTIVE_TAGS.has(el.tagName) || (el.getAttribute && (el.getAttribute('role')==='button' || el.getAttribute('role')==='link')) || el.getAttribute('tabindex') === '0';
			const hasRole = role !== '';

			let refTag = '';
			if (isInteractive || role==='heading' || role==='img') {
				refCounter++;
				const refId = 'ref' + refCounter;
				refTag = '[' + refId + '] ';
				el.setAttribute('data-astonish-ref', refId);
				refList.push({ref: refId, tag: el.tagName, role: role || el.tagName.toLowerCase(), name: name});
			}

			if (INTERACTIVE_ONLY && !isInteractive && role!=='heading') {
				for (const c of el.childNodes) walk(c, depth);
				return;
			}

			if (hasRole || isInteractive) {
				let line = '  '.repeat(depth) + refTag + (role || el.tagName.toLowerCase());
				if (name) line += ' ' + JSON.stringify(name);
				if (el.tagName==='A' && el.href) {
					const h = el.getAttribute('href');
					if (h && !h.startsWith('javascript:')) line += ' href=' + JSON.stringify(h.length>100?h.substring(0,100):h);
				}
				if (role==='heading') line += ' (level=' + el.tagName[1] + ')';
				if (role==='textbox' || role==='combobox') {
					const v = el.value;
					if (v) line += ' value=' + JSON.stringify(v.length>50?v.substring(0,50):v);
				}
				if ((role==='checkbox'||role==='radio') && el.checked) line += ' (checked)';
				lines.push(line);
				for (const c of el.childNodes) walk(c, depth + 1);
			} else if (COMPACT) {
				for (const c of el.childNodes) walk(c, depth);
			} else {
				for (const c of el.childNodes) walk(c, depth);
			}
		}

		walk(document.body, 0);
		return JSON.stringify({text: lines.join('\n'), refs: refList, refCount: refCounter});
	}`, maxDepth, interactiveOnly, compact)

	result, err := pg.Eval(jsCode)
	if err != nil {
		return nil, fmt.Errorf("DOM snapshot failed: %w", err)
	}

	// Parse the JSON result
	var data struct {
		Text     string           `json:"text"`
		Refs     []domSnapshotRef `json:"refs"`
		RefCount int              `json:"refCount"`
	}
	if err := json.Unmarshal([]byte(result.Value.Str()), &data); err != nil {
		return nil, fmt.Errorf("DOM snapshot parse error: %w", err)
	}

	// Populate the RefMap with CSS-selector-based entries
	refs.Reset()
	for _, r := range data.Refs {
		refs.Add(RefEntry{
			Role:        r.Role,
			Name:        r.Name,
			CSSSelector: fmt.Sprintf("[data-astonish-ref=%q]", r.Ref),
		})
	}

	text := data.Text
	if len(text) > maxChars {
		text = text[:maxChars] + "\n... (truncated)"
	}

	info, _ := pg.Info()
	url := ""
	title := ""
	if info != nil {
		url = info.URL
		title = info.Title
	}

	return &SnapshotResult{
		Text:     text,
		RefCount: data.RefCount,
		URL:      url,
		Title:    title,
	}, nil
}
