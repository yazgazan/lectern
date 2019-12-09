package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/gdamore/tcell"
	"github.com/k3a/html2text"
	"github.com/meskio/epubgo"
	"github.com/rivo/tview"
)

var BackgroundColor = tcell.NewHexColor(0x002833)

type Chapter struct {
	url   string
	index int

	g *tview.Grid
	t *tview.TextView
}

func (c Chapter) GetOffset() int {
	r, _ := c.t.GetScrollOffset()

	return r
}

func (c *Chapter) SetOffset(r int) {
	c.t.ScrollTo(r, 0)
}

func (c *Chapter) SetWidth(w int) {
	c.g.SetColumns(-1, w, -1)
}

func (c Chapter) URL() string {
	return c.url
}

func (c Chapter) Index() int {
	return c.index
}

type TOC struct {
	url string

	g *tview.Grid
	l *tview.List
}

func (t TOC) URL() string {
	return t.url
}

func (t TOC) Index() int {
	return -1
}

func (t *TOC) SetWidth(w int) {
	t.g.SetColumns(-1, w, -1)
}

func (t *TOC) SetSelected(idx int) {
	t.l.SetCurrentItem(idx)
}

type Page interface {
	Index() int
	SetWidth(int)
	URL() string
}

type Book struct {
	app    *tview.Application
	tPages *tview.Pages

	Title    string
	TOC      *TOC
	Chapters []*Chapter
	Pages    []Page

	pagesMap map[string]Page

	MarkChapter int
	MarkLine    int

	Width       int
	Current     int
	menuContext int
}

func (b *Book) Initialize() {
	b.app = tview.NewApplication()
	b.tPages = tview.NewPages()
	b.tPages.SetBackgroundColor(BackgroundColor)
}

func (b *Book) Run() error {

	base := tview.NewGrid()
	base.SetColumns(-1)
	base.SetRows(2, -1)
	base.SetBackgroundColor(BackgroundColor)
	base.Clear()

	title := tview.NewTextView()
	title.SetBackgroundColor(BackgroundColor)
	title.SetTextColor(tcell.ColorDefault)
	title.SetText(b.Title)
	title.SetTextAlign(tview.AlignCenter)

	base.AddItem(title, 0, 0, 1, 1, 0, 0, false)
	base.AddItem(b.tPages, 1, 0, 1, 1, 0, 0, true)

	b.app.SetRoot(base, true)
	b.app.SetFocus(base)

	actions := map[rune]func(){
		'q': b.app.Stop,
		'l': b.NextChapter,
		'h': b.PreviousChapter,
		'/': b.ToggleMenu,
		'j': b.MenuDown,
		'k': b.MenuUp,
		'm': func() {
			if b.Current == b.TOC.Index() {
				return
			}

			b.MarkChapter = b.Current
			b.MarkLine = b.Chapters[b.Current].GetOffset()
		},
		'\'': func() {
			if b.MarkChapter == -1 || b.MarkLine == -1 {
				return
			}

			if b.Chapters[b.MarkChapter].GetOffset() != b.MarkLine {
				b.Chapters[b.MarkChapter].SetOffset(b.MarkLine)
			}
			if b.Current != b.MarkChapter {
				b.GoToPage(b.MarkChapter)
			}
		},
		' ': b.JumpScroll,
		'+': func() { b.SetWidth(b.Width + 5) },
		'-': func() { b.SetWidth(b.Width + -5) },
		'=': func() { b.SetWidth(80) },
	}

	b.app.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		action, ok := actions[event.Rune()]
		if !ok {
			return event
		}
		action()
		return event
	})

	return b.app.Run()
}

func (b Book) Page(u string) (Page, error) {
	if b.pagesMap == nil {
		return nil, fmt.Errorf("page %q not found: no pages added", u)
	}

	p, ok := b.pagesMap[u]
	if !ok {
		return nil, fmt.Errorf("page %q not found", u)
	}

	return p, nil
}

func (b *Book) AddChapter(c *Chapter) {
	b.Chapters = append(b.Chapters, c)
	b.Pages = append(b.Pages, c)
	if b.pagesMap == nil {
		b.pagesMap = map[string]Page{}
	}
	b.pagesMap[c.URL()] = c
}

func (b *Book) SetTOC(t *TOC) {
	b.TOC = t
	b.Pages = append(b.Pages, t)
	if b.pagesMap == nil {
		b.pagesMap = map[string]Page{}
	}
	b.pagesMap[t.URL()] = t
}

func (b *Book) SetWidth(w int) {
	b.Width = w
	for _, p := range b.Pages {
		p.SetWidth(w)
	}
}

func (b *Book) GoToPage(idx int) {
	u := b.IndexToURL(idx)
	b.Current = idx
	if idx != b.TOC.Index() {
		b.TOC.SetSelected(idx)
	}
	b.tPages.SwitchToPage(u)
}

func (b Book) IndexToURL(idx int) string {
	if idx == -1 {
		return b.TOC.URL()
	}

	return b.Chapters[idx].URL()
}

func (b *Book) GenerateTOC(toc []TOCEntry, initialPage int) {
	tocP, tocL := renderTOC(b.Width, toc, func(i int) {
		b.GoToPage(i)
	})
	if b.Current != -1 {
		tocL.SetCurrentItem(b.Current)
	}

	b.SetTOC(&TOC{
		url: "TOC",
		g:   tocP,
		l:   tocL,
	})

	b.tPages.AddPage(b.TOC.URL(), b.TOC.g, true, initialPage == b.TOC.Index())
}

func (b *Book) GenerateChapter(book *EBook, i int, u string, initialPage, initialOffset int, progress string, queueFn func(func())) error {
	p, t, err := renderChapter(b.Width, book, u, progress, queueFn)
	if err != nil {
		return err
	}

	page := &Chapter{
		url:   u,
		index: i,
		g:     p,
		t:     t,
	}

	if initialOffset > 0 {
		t.ScrollTo(initialOffset, 0)
	}
	b.tPages.AddPage(page.URL(), page.g, true, initialPage == page.Index())

	b.AddChapter(page)

	return nil
}

func (b Book) State() State {
	current := b.Current
	if current == -1 {
		current = b.menuContext
	}

	state := State{
		Page:    current,
		Offsets: map[int]int{},
		Width:   b.Width,
	}

	for _, c := range b.Chapters {
		r := c.GetOffset()
		if r <= 0 {
			continue
		}

		state.Offsets[c.Index()] = r
	}

	return state
}

func (b *Book) LoadState(state State) {
	b.Current = state.Page
	b.menuContext = state.Page
	b.SetWidth(state.Width)

	b.GoToPage(state.Page)
}

func (b *Book) NextChapter() {
	if b.Current+1 >= len(b.Chapters) {
		return
	}

	b.GoToPage(b.Current + 1)
}

func (b *Book) PreviousChapter() {
	if b.Current-1 < b.TOC.Index() {
		return
	}

	b.GoToPage(b.Current - 1)
}

func (b *Book) ToggleMenu() {
	if b.Current == b.TOC.Index() {
		b.GoToPage(b.menuContext)
		return
	}
	b.menuContext = b.Current
	b.GoToPage(b.TOC.Index())
}

func (b *Book) MenuDown() {
	if b.Current != b.TOC.Index() {
		return
	}
	i := b.TOC.l.GetCurrentItem()
	if i > b.TOC.l.GetItemCount() {
		return
	}
	b.TOC.l.SetCurrentItem(i + 1)
}

func (b *Book) MenuUp() {
	if b.Current != b.TOC.Index() {
		return
	}
	i := b.TOC.l.GetCurrentItem()
	if i == 0 {
		return
	}
	b.TOC.l.SetCurrentItem(i - 1)
}

func (b *Book) JumpScroll() {
	if b.Current == b.TOC.Index() {
		return
	}
	r := b.Chapters[b.Current].GetOffset()
	b.Chapters[b.Current].SetOffset(r + 80)
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <filename>\n", os.Args[0])
		os.Exit(2)
	}

	ebook, err := NewBook(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer ebook.Close()

	loadedState, stateExists, err := LoadState(os.Args[1])
	if err != nil {
		panic(err)
	}

	title, err := ebook.Metadata("title")
	if err != nil {
		panic(err)
	}
	if len(title) == 0 {
		title = []string{filepath.Base(os.Args[1])}
	}

	book := &Book{
		Title:       title[0],
		Current:     -1,
		menuContext: -1,
		Width:       80,
		MarkChapter: -1,
		MarkLine:    -1,
	}

	initialPage := -1
	initialOffsets := map[int]int{}
	if stateExists {
		initialPage = loadedState.Page
		initialOffsets = loadedState.Offsets
	}

	book.Initialize()

	toc, err := ebook.TOC()
	if err != nil {
		panic(err)
	}

	book.GenerateTOC(toc, initialPage)

	for i, entry := range toc {
		err = book.GenerateChapter(
			ebook, i, entry.URL,
			initialPage, initialOffsets[i],
			fmt.Sprintf("%q (%.2f%%)", entry.Name, 100*float64(i)/float64(len(toc))),
			func(fn func()) { book.app.QueueUpdateDraw(fn) },
			// func(fn func()) { book.app.QueueUpdate(fn) },
		)
		if err != nil {
			panic(err)
		}
	}

	if stateExists {
		book.LoadState(loadedState)
	}

	err = book.Run()
	if err != nil {
		panic(err)
	}

	state := book.State()

	err = SaveState(os.Args[1], state)
	if err != nil {
		panic(err)
	}
}

func stateFname(bookFname string) string {
	return filepath.Join(
		filepath.Dir(bookFname),
		"."+filepath.Base(bookFname)+".lectern.json",
	)
}

func LoadState(bookFname string) (State, bool, error) {
	var state State

	fname := stateFname(bookFname)

	f, err := os.Open(fname)
	if os.IsNotExist(err) {
		return state, false, nil
	}
	if err != nil {
		return state, true, err
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	err = dec.Decode(&state)
	if err != nil {
		return state, true, err
	}

	return state, true, nil
}

func SaveState(bookFname string, state State) error {
	fname := stateFname(bookFname)

	f, err := os.Create(fname)
	if err != nil {
		return err
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	err = enc.Encode(state)

	return err
}

type State struct {
	Page    int
	Offsets map[int]int
	Width   int
}

func renderTOC(width int, toc []TOCEntry, cb func(int)) (*tview.Grid, *tview.List) {
	l := tview.NewList()
	l.SetBackgroundColor(BackgroundColor)
	for i, entry := range toc {
		u := entry.URL
		j := i
		l.AddItem(entry.Name, u, 0, func() {
			cb(j)
		})
	}

	g := tview.NewGrid()
	g.SetColumns(-1, width, -1)
	g.SetBackgroundColor(BackgroundColor)

	g.Clear()
	g.AddItem(l, 0, 1, 1, 1, 0, 0, true)

	return g, l
}

func renderChapter(width int, book *EBook, u string, progress string, queueFn func(func())) (*tview.Grid, *tview.TextView, error) {
	text := tview.NewTextView()
	text.SetBackgroundColor(BackgroundColor)
	text.SetTextColor(tcell.ColorDefault)
	text.SetWrap(true)
	text.SetWordWrap(true)

	b, err := book.ReadChapter(u)
	if err != nil {
		return nil, nil, err
	}
	text.SetText(b)

	g := tview.NewGrid()
	g.SetColumns(-1, width, -1)
	g.SetRows(-1, 1, 1)
	g.SetBackgroundColor(BackgroundColor)

	g.Clear()
	g.AddItem(text, 0, 1, 1, 1, 0, 0, true)

	progressText := tview.NewTextView()
	progressText.SetBackgroundColor(BackgroundColor)
	progressText.SetTextColor(tcell.ColorDefault)
	progressText.SetText(progress)
	progressText.SetTextAlign(tview.AlignCenter)

	g.AddItem(progressText, 2, 1, 1, 1, 0, 0, false)

	justUpdated := false
	setLine := func(currentLine int) {
		queueFn(func() {
			newLine, _ := text.GetScrollOffset()

			if newLine == currentLine {
				return
			}
			_, _, _, h := text.GetRect()

			nLines, err := text.NLines()
			if err != nil {
				progressText.SetText(fmt.Sprintf("%s - lines %d-%d", progress, newLine+1, newLine+h+1))
			} else {
				if newLine+h >= nLines {
					h = nLines - newLine - 1
				}
				progressText.SetText(fmt.Sprintf("%s - lines %d-%d/%d", progress, newLine+1, newLine+h+1, nLines))
			}
		})
	}

	text.SetDrawFunc(func(screen tcell.Screen, x, y, width, height int) (int, int, int, int) {
		if !justUpdated {
			setLine(-1)
			justUpdated = true
		} else {
			justUpdated = false
		}
		return x, y, width, height
	})

	return g, text, nil
}

type EBook struct {
	*epubgo.Epub
	Title string

	it *epubgo.SpineIterator
}

type TOCEntry struct {
	Name string
	URL  string
}

func NewBook(fname string) (*EBook, error) {

	book, err := epubgo.Open(fname)
	if err != nil {
		return nil, err
	}

	title, err := book.Metadata("title")
	if err != nil {
		book.Close()
		return nil, err
	}
	if len(title) == 0 {
		title = []string{""}
	}

	it, err := book.Spine()
	if err != nil {
		book.Close()
		return nil, err
	}

	return &EBook{
		Epub:  book,
		Title: title[0],
		it:    it,
	}, nil
}

func (b *EBook) ReadCurrentChapter() (string, error) {
	r, err := b.it.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()

	buf, err := ioutil.ReadAll(r)
	if err != nil {
		return "", err
	}

	return html2text.HTML2Text(string(buf)), nil
}

func (b *EBook) ReadChapter(u string) (string, error) {
	current := b.it.URL()

	for {
		if b.it.URL() == u {
			return b.ReadCurrentChapter()
		}
		if b.it.IsFirst() {
			break
		}
		err := b.it.Previous()
		if err != nil {
			return "", err
		}
	}

	for {
		if b.it.URL() == u {
			return b.ReadCurrentChapter()
		}
		if b.it.IsLast() {
			break
		}
		err := b.it.Next()
		if err != nil {
			return "", err
		}
	}

	return b.ReadChapter(current)
}

func (b *EBook) TOC() ([]TOCEntry, error) {
	it, err := b.Epub.Navigation()
	if err != nil {
		return nil, err
	}

	toc := []TOCEntry{}
	for {
		toc = append(toc, TOCEntry{
			Name: it.Title(),
			URL:  it.URL(),
		})
		if it.IsLast() {
			break
		}

		err = it.Next()
		if err != nil {
			return nil, err
		}
	}

	return toc, nil
}
