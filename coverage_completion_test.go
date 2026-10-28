package goquery

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, errors.New("write failed") }

type matchAllOnly struct{ Matcher }

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func coverageDocument(t *testing.T) *Document {
	t.Helper()
	return loadString(t, `<!doctype html><html><head><title>x</title></head><body>
		<section id="root"><p class="a">one</p>text<p class="b">two<span>inner</span></p><p class="c">three</p></section>
		<div id="wrappers"><i class="wrapper"><b></b></i><i class="inner"></i></div>
		<input type="email"><input class="named" type="text">
	</body></html>`)
}

func TestCoverageArrayFilterAndQueryEdges(t *testing.T) {
	d := coverageDocument(t)
	ps := d.Find("p")
	empty := d.Find("missing")
	m := compileMatcher(".b")

	if got := empty.Index(); got != -1 {
		t.Fatalf("empty Index = %d", got)
	}
	if got := empty.IndexSelector("p"); got != -1 {
		t.Fatalf("empty IndexSelector = %d", got)
	}
	if got := ps.Eq(1).IndexMatcher(m); got != 0 {
		t.Fatalf("IndexMatcher = %d", got)
	}
	if got := empty.IndexMatcher(m); got != -1 {
		t.Fatalf("empty IndexMatcher = %d", got)
	}
	if got := ps.IndexOfSelection(nil); got != -1 {
		t.Fatalf("IndexOfSelection(nil) = %d", got)
	}
	if got := ps.IndexOfSelection(empty); got != -1 {
		t.Fatalf("IndexOfSelection(empty) = %d", got)
	}

	if got := ps.Union(ps.First()).Length(); got != 3 {
		t.Fatalf("Union length = %d", got)
	}
	if got := ps.Filter(".b").AddBackMatcher(compileMatcher(".a")).Length(); got != 2 {
		t.Fatalf("AddBackMatcher length = %d", got)
	}
	if got := ps.NotSelection(nil).Length(); got != 3 {
		t.Fatalf("NotSelection(nil) length = %d", got)
	}
	if got := ps.HasSelection(nil).Length(); got != 0 {
		t.Fatalf("HasSelection(nil) length = %d", got)
	}
	if ps.IsFunction(func(int, *Selection) bool { return false }) {
		t.Fatal("IsFunction unexpectedly matched")
	}
	if ps.IsSelection(nil) {
		t.Fatal("IsSelection(nil) unexpectedly matched")
	}

	large := make([]*html.Node, minNodesForSet)
	for i := range large {
		large[i] = &html.Node{Type: html.ElementNode, Data: "x"}
	}
	largeSel := &Selection{Nodes: large, document: d}
	if got := largeSel.FilterNodes(large[0]).Length(); got != 1 {
		t.Fatalf("large FilterNodes length = %d", got)
	}
	if got := largeSel.NotNodes(large[0]).Length(); got != minNodesForSet-1 {
		t.Fatalf("large NotNodes length = %d", got)
	}

	called := 0
	empty.EachWithBreak(func(int, *Selection) bool { called++; return true })
	if called != 0 {
		t.Fatalf("empty EachWithBreak called %d times", called)
	}
}

func TestCoverageTraversalVariants(t *testing.T) {
	d := coverageDocument(t)
	mP := compileMatcher("p")
	mAny := compileMatcher("*")
	root := d.Find("#root")
	middle := d.Find("p.b")
	last := d.Find("p.c")
	span := d.Find("span")

	calls := map[string]*Selection{
		"find nil":                   root.FindSelection(nil),
		"find nodes empty":           root.FindNodes(),
		"contents empty selector":    root.ContentsFiltered(""),
		"contents matcher":           root.ContentsMatcher(mP),
		"children matcher":           root.ChildrenMatcher(mP),
		"parent matcher":             middle.ParentMatcher(mAny),
		"closest nil":                middle.ClosestSelection(nil),
		"parents matcher":            span.ParentsMatcher(mAny),
		"parents until matcher":      span.ParentsUntilMatcher(compileMatcher("body")),
		"parents until nil":          span.ParentsUntilSelection(nil),
		"parents filtered matcher":   span.ParentsFilteredUntilMatcher(mAny, compileMatcher("body")),
		"parents matcher nil":        span.ParentsMatcherUntilSelection(mAny, nil),
		"siblings matcher":           middle.SiblingsMatcher(mP),
		"next matcher":               middle.NextMatcher(mP),
		"next all matcher":           middle.NextAllMatcher(mP),
		"prev matcher":               middle.PrevMatcher(mP),
		"prev all matcher":           last.PrevAllMatcher(mP),
		"next until matcher":         middle.NextUntilMatcher(compileMatcher(".c")),
		"next until nil":             middle.NextUntilSelection(nil),
		"prev until matcher":         last.PrevUntilMatcher(compileMatcher(".a")),
		"prev until nil":             last.PrevUntilSelection(nil),
		"next filtered matcher":      middle.NextFilteredUntilMatcher(mP, compileMatcher(".c")),
		"next matcher selection nil": middle.NextMatcherUntilSelection(mP, nil),
		"prev filtered matcher":      last.PrevFilteredUntilMatcher(mP, compileMatcher(".a")),
		"prev matcher selection nil": last.PrevMatcherUntilSelection(mP, nil),
	}
	for name, got := range calls {
		if got == nil {
			t.Fatalf("%s returned nil", name)
		}
	}
	if got := middle.NextUntilNodes().Length(); got != 1 {
		t.Fatalf("NextUntilNodes() length = %d", got)
	}
	parent := root.Get(0)
	if got := getChildrenWithSiblingType(parent, siblingAll, parent.FirstChild, nil); len(got) == 0 {
		t.Fatal("siblingAll with first child skipped returned no nodes")
	}
	t.Run("invalid sibling type", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("invalid sibling type did not panic")
			}
		}()
		getChildrenWithSiblingType(parent, siblingType(99), nil, nil)
	})
}

func TestCoverageManipulationVariants(t *testing.T) {
	d := coverageDocument(t)
	wrapper := d.Find(".wrapper")
	inner := d.Find(".inner")

	detached := &html.Node{Type: html.ElementNode, Data: "em"}
	(&Selection{Nodes: []*html.Node{detached}, document: d}).WrapHtml("<strong></strong>")

	d.Find("p.a").WrapNode(&html.Node{Type: html.ElementNode, Data: "aside"})
	d.Find("p.b").WrapAllSelection(wrapper)
	(&Selection{document: d}).WrapAllHtml("<div></div>")
	(&Selection{document: d}).WrapAllNode(&html.Node{Type: html.ElementNode, Data: "div"})
	(&Selection{Nodes: []*html.Node{{Type: html.ElementNode, Data: "small"}}, document: d}).WrapAllHtml("<div></div>")
	d.Find("p.c").WrapInnerSelection(inner)
	d.Find("p.c").WrapInnerNode(&html.Node{Type: html.ElementNode, Data: "u"})

	if got := (*Selection)(nil).parseOpts(); got != nil {
		t.Fatalf("nil parseOpts = %#v", got)
	}
	if got := parseHtml("<mark>x</mark>", nil); len(got) != 1 {
		t.Fatalf("parseHtml nodes = %d", len(got))
	}
	t.Run("invalid fragment context", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("invalid fragment context did not panic")
			}
		}()
		parseHtmlWithContext("<b></b>", &html.Node{Type: html.TextNode}, nil)
	})

	text := &html.Node{Type: html.TextNode, Data: "detached"}
	(&Selection{Nodes: []*html.Node{text}, document: d}).eachNodeHtml("<b></b>", true, func(*html.Node, []*html.Node) {})
}

func TestCoveragePropertiesSelectorsAndUtilities(t *testing.T) {
	d := coverageDocument(t)
	empty := d.Find("missing")
	if value, err := empty.Html(); value != "" || err != nil {
		t.Fatalf("empty Html = %q, %v", value, err)
	}
	badContainer := &html.Node{Type: html.ElementNode, Data: "div"}
	badContainer.AppendChild(&html.Node{Type: html.ErrorNode})
	if _, err := (&Selection{Nodes: []*html.Node{badContainer}, document: d}).Html(); err == nil {
		t.Fatal("Html invalid child returned nil")
	}
	text := &html.Node{Type: html.TextNode, Data: "x"}
	nodes := &Selection{Nodes: []*html.Node{text, d.Find("p").Get(0)}, document: d}
	if !nodes.HasClass("a") {
		t.Fatal("HasClass failed after skipping text node")
	}
	if got := nodes.ToggleClass().Length(); got != 2 {
		t.Fatalf("ToggleClass empty length = %d", got)
	}
	if getAttributePtr("x", nil) != nil {
		t.Fatal("getAttributePtr(nil) was non-nil")
	}

	if _, ok := SelectorForNode(&html.Node{Type: html.ElementNode, Data: "orphan", Parent: &html.Node{Type: html.TextNode}}); ok {
		t.Fatal("selector for invalid parent unexpectedly succeeded")
	}
	if _, _, ok := selectorStep(nil, true); ok {
		t.Fatal("selectorStep(nil) unexpectedly succeeded")
	}
	if selector, ok := SelectorForNode(&html.Node{Type: html.ElementNode, Data: "custom"}); !ok || selector != "custom" {
		t.Fatalf("orphan selector = %q, %v", selector, ok)
	}
	parent := &html.Node{Type: html.ElementNode, Data: "div"}
	orphan := &html.Node{Type: html.ElementNode, Data: "span", Parent: parent}
	if _, _, ok := selectorStep(orphan, true); ok {
		t.Fatal("detached child selector unexpectedly succeeded")
	}

	if got := NodeName(empty); got != "" {
		t.Fatalf("empty NodeName = %q", got)
	}
	if got := nodeName(nil); got != "" {
		t.Fatalf("nil nodeName = %q", got)
	}
	if got := nodeName(&html.Node{Type: html.NodeType(99)}); got != "" {
		t.Fatalf("unknown nodeName = %q", got)
	}
	if err := Render(io.Discard, empty); err != nil {
		t.Fatalf("Render empty: %v", err)
	}
	if err := Render(errorWriter{}, d.Find("p").First()); err == nil {
		t.Fatal("Render error writer returned nil")
	}
	if _, err := OuterHtml(&Selection{Nodes: []*html.Node{{Type: html.ErrorNode}}, document: d}); err == nil {
		t.Fatal("OuterHtml invalid node returned nil")
	}

	large := make([]*html.Node, minNodesForSet)
	for i := range large {
		large[i] = &html.Node{Type: html.ElementNode, Data: "x"}
	}
	if got := appendWithoutDuplicates(large, []*html.Node{large[0]}, nil); len(got) != minNodesForSet {
		t.Fatalf("large append length = %d", len(got))
	}
}

func TestCoverageDocumentErrorsAndSingleMatcher(t *testing.T) {
	if _, err := NewDocumentFromReader(errorReader{}); err == nil {
		t.Fatal("NewDocumentFromReader error reader returned nil")
	}
	if _, err := NewDocumentFromReaderWithOptions(errorReader{}); err == nil {
		t.Fatal("NewDocumentFromReaderWithOptions error reader returned nil")
	}
	if _, err := NewDocumentFromResponse(nil); err == nil {
		t.Fatal("NewDocumentFromResponse(nil) returned nil")
	}
	if _, err := NewDocumentFromResponse(&http.Response{Body: io.NopCloser(strings.NewReader("x"))}); err == nil {
		t.Fatal("response without request returned nil")
	}
	badResponse := &http.Response{
		Body:    io.NopCloser(errorReader{}),
		Request: &http.Request{},
	}
	if _, err := NewDocumentFromResponse(badResponse); err == nil {
		t.Fatal("response with bad body returned nil")
	}

	oldClient := http.DefaultClient
	http.DefaultClient = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader("<html><body><p>ok</p></body></html>")),
			Request:    r,
		}, nil
	})}
	t.Cleanup(func() { http.DefaultClient = oldClient })
	d, err := NewDocument("https://example.test")
	if err != nil || d.Find("p").Text() != "ok" {
		t.Fatalf("NewDocument = %v, %v", d, err)
	}
	if _, err := NewDocument("://bad"); err == nil {
		t.Fatal("NewDocument invalid URL returned nil")
	}

	base := compileMatcher("p")
	if got := SingleMatcher(SingleMatcher(base)); got == nil {
		t.Fatal("nested SingleMatcher returned nil")
	}
	fallback := singleMatcher{matchAllOnly{Matcher: base}}
	root := coverageDocument(t).rootNode
	if fallback.MatchFirst(root) == nil || len(fallback.MatchAll(root)) != 1 {
		t.Fatal("singleMatcher fallback did not match")
	}
	if got := (singleMatcher{matchAllOnly{Matcher: compileMatcher("missing")}}).MatchAll(root); got != nil {
		t.Fatalf("missing MatchAll = %#v", got)
	}
}
