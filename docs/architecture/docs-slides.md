# Docs — Slides (AI Presentation Generation)

> **Status**: Planned  
> **Category**: Productivity Tools  
> **First doc type**: Slides  
> **Future doc types**: Documents (rich text), Spreadsheets (structured data)

## Overview

Astonish Docs is a new productivity category that enables the AI agent to create, edit, and export professional presentations (and later other document types) using natural language. The first implementation focuses on **Slides** — AI-generated HTML presentations that render live in Studio and export to PDF, PPTX, and standalone HTML.

### Design Philosophy

- **AI-first authoring**: No manual editor in V1. The LLM generates and refines slides via dedicated tools.
- **HTML as the internal format**: Each slide is stored as self-contained HTML with absolute positioning (proven by Genspark at scale). This gives the LLM maximum styling freedom while enabling pixel-perfect rendering.
- **Pre-built themes with AI flexibility**: Ship with 5 strong defaults; users can ask the AI to modify or create custom themes.
- **Database-first storage**: Follows the existing `entstore` pattern (like apps, sessions, memories). Personal → Team → Org scoping from day one.
- **Export-native**: PDF (via existing go-rod), PPTX (custom minimal OOXML writer, image-based), standalone HTML.
- **System fonts for V1**: Use system font stacks (`system-ui`, `monospace`) to avoid font-loading complexity. Custom web fonts in V2.

### Inspiration

The architecture is inspired by Genspark's slide generation system, which:
1. Uses individual HTML files per slide with absolute positioning and CSS variables
2. Renders in iframes at 1920×1080, scaled for preview
3. Maintains a manifest for slide ordering and metadata
4. Exports to PPTX/PDF via headless browser screenshots

Additionally validated by PPTAgent research (95% success rate with HTML rendering vs 74.6% without).

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│                         LLM AGENT                                 │
│  System prompt guidance: how to produce slide HTML                │
│  Tools: create_slides, write_slide, read_slide, update_theme ... │
└──────────────┬──────────────────────────────────────┬────────────┘
               │ tool calls                            │ returns results
               ▼                                       ▼
┌──────────────────────────┐    ┌──────────────────────────────────┐
│   DATABASE (entstore)    │    │   ChatRunner drain pattern       │
│                          │    │                                  │
│   DocsStore interface    │    │   afterToolCallback captures     │
│   ├─ SQLite (local)      │    │   → pendingDocsUpdates buffer    │
│   └─ PostgreSQL (plat)   │    │   → DrainDocsUpdates() called    │
│                          │    │   → emitEvent("docs_update",{})  │
│   Tables:                │    └──────────────┬───────────────────┘
│   - docs (metadata)      │                   │
│   - doc_slides (content) │                   ▼
│                          │    ┌──────────────────────────────────┐
└──────────────────────────┘    │   FRONTEND: SlidesCard           │
                                │   • Iframe renders current slide │
                                │   • Nav arrows / keyboard        │
                                │   • Slide counter / thumbnails   │
                                │   • Fullscreen presenter mode    │
                                │   • Export dropdown              │
                                └──────────────┬───────────────────┘
                                               │ export request
                                               ▼
                                ┌──────────────────────────────────┐
                                │   EXPORT PIPELINE (Go backend)   │
                                │                                  │
                                │  PDF: go-rod renders each slide  │
                                │       at 1920×1080 → multi-page  │
                                │       landscape PDF              │
                                │                                  │
                                │  PPTX: go-rod screenshots →     │
                                │        custom OOXML writer →     │
                                │        full-bleed image slides   │
                                │                                  │
                                │  HTML: Bundle slides + theme     │
                                │        into self-contained file  │
                                │        with keyboard navigation  │
                                └──────────────────────────────────┘
```

---

## Storage & Data Model

### Database Schema (Ent-based)

Following the existing `pkg/store/` + `ent/` pattern used by apps, sessions, and memories:

**Interface** (`pkg/store/docs.go`):
```go
package store

type DocListItem struct {
    ID          string    `json:"id"`
    Slug        string    `json:"slug"`
    Title       string    `json:"title"`
    Description string    `json:"description,omitempty"`
    DocType     string    `json:"docType"`     // "slides", "document", "sheet"
    SlideCount  int       `json:"slideCount"`
    Version     int       `json:"version"`
    ThemeName   string    `json:"themeName"`
    UpdatedAt   time.Time `json:"updatedAt"`
    CreatedAt   time.Time `json:"createdAt"`
}

type DocsStore interface {
    // Deck CRUD
    CreateDeck(ctx context.Context, deck *DeckManifest) error
    GetDeck(ctx context.Context, slug string) (*DeckManifest, error)
    UpdateDeck(ctx context.Context, slug string, deck *DeckManifest) error
    DeleteDeck(ctx context.Context, slug string) error
    ListDecks(ctx context.Context) ([]DocListItem, error)

    // Slide CRUD
    WriteSlide(ctx context.Context, deckSlug string, index int, slide *SlideContent) error
    ReadSlide(ctx context.Context, deckSlug string, index int) (*SlideContent, error)
    DeleteSlide(ctx context.Context, deckSlug string, index int) error
    ReorderSlides(ctx context.Context, deckSlug string, newOrder []int) error
}
```

**Ent Schema** (`ent/team/schema/doc.go`):
```go
type Doc struct { ent.Schema }

func (Doc) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).Default(uuid.New),
        field.String("slug").NotEmpty(),                    // ULID-based, unique
        field.String("title").NotEmpty(),
        field.String("description").Default(""),
        field.String("doc_type").Default("slides"),         // "slides" | "document" | "sheet"
        field.Int("version").Default(1),
        field.String("theme_name").Default("dark-minimal"),
        field.Text("theme_css").Default(""),                // Full theme CSS content
        field.JSON("metadata", map[string]any{}).Optional(), // Dimensions, extra config
        field.Int("slide_count").Default(0),
        field.String("session_id").Default(""),
        field.Time("created_at").Default(time.Now).Immutable(),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}
func (Doc) Indexes() []ent.Index {
    return []ent.Index{index.Fields("slug").Unique()}
}
func (Doc) Annotations() []schema.Annotation {
    return []schema.Annotation{entsql.Table("docs")}
}
```

**Ent Schema** (`ent/team/schema/doc_slide.go`):
```go
type DocSlide struct { ent.Schema }

func (DocSlide) Fields() []ent.Field {
    return []ent.Field{
        field.UUID("id", uuid.UUID{}).Default(uuid.New),
        field.String("doc_slug").NotEmpty(),                // FK to docs.slug
        field.Int("position").Default(0),                   // Order index (0-based)
        field.String("title").Default(""),                  // Slide title (for nav)
        field.Text("html_content").Default(""),             // Full slide HTML
        field.Text("speaker_notes").Default(""),            // Presenter notes
        field.Time("created_at").Default(time.Now).Immutable(),
        field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
    }
}
func (DocSlide) Indexes() []ent.Index {
    return []ent.Index{
        index.Fields("doc_slug", "position").Unique(),
        index.Fields("doc_slug"),
    }
}
func (DocSlide) Annotations() []schema.Annotation {
    return []schema.Annotation{entsql.Table("doc_slides")}
}
```

### Domain Types (`pkg/docs/slides/types.go`)

```go
package slides

import "time"

type DeckManifest struct {
    ID          string            `json:"id"`
    Slug        string            `json:"slug"`
    Title       string            `json:"title"`
    Description string            `json:"description,omitempty"`
    Version     int               `json:"version"`
    Theme       ThemeInfo         `json:"theme"`
    Dimensions  Dimensions        `json:"dimensions"`
    Slides      []SlideInfo       `json:"slides"`
    CreatedAt   time.Time         `json:"createdAt"`
    UpdatedAt   time.Time         `json:"updatedAt"`
}

type ThemeInfo struct {
    Name string `json:"name"`    // e.g., "dark-minimal"
    CSS  string `json:"css"`     // Full CSS content (stored in DB)
}

type Dimensions struct {
    Width  int `json:"width"`    // 1920
    Height int `json:"height"`   // 1080
}

type SlideInfo struct {
    Index int    `json:"index"`
    Title string `json:"title"`
    Notes string `json:"notes,omitempty"`
}

type SlideContent struct {
    Index   int    `json:"index"`
    Title   string `json:"title"`
    HTML    string `json:"html"`    // Complete slide HTML document
    Notes   string `json:"notes"`
}
```

### Deck ID Generation

Uses ULID (Universally Unique Lexicographically Sortable Identifier):
```go
import "github.com/oklog/ulid/v2"

func generateDeckSlug(title string) string {
    // ULID prefix (sortable by creation time) + short title slug
    id := ulid.Make().String()[:10]  // First 10 chars of ULID
    slug := slugify(title)
    if len(slug) > 20 {
        slug = slug[:20]
    }
    return fmt.Sprintf("%s-%s", strings.ToLower(id), slug)
    // Example: "01j5kxyz12-microservices-migration"
}
```

**Why ULID-based?**
- No collision risk (unlike pure title slugification)
- Sortable by creation time (useful for listing)
- URL-safe (no encoding needed)
- Works for non-ASCII titles (the slug part is best-effort, ULID ensures uniqueness)

### Slide HTML Format

Each slide is stored as a complete HTML document (in the `doc_slides.html_content` column):

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <style>/* Theme CSS injected here at render time by the API */</style>
</head>
<body style="margin:0; background:var(--surface);">
  <div class="slide-container" style="width:1920px; height:1080px; position:relative; overflow:hidden;">
    
    <!-- All elements use absolute positioning -->
    <div data-element="title" style="position:absolute; left:96px; top:200px; width:800px;">
      <h1 style="font-family:var(--font-display); font-size:64px; font-weight:700; color:var(--ink);">
        Slide Title
      </h1>
    </div>

    <div data-element="body" style="position:absolute; left:96px; top:400px; width:700px;">
      <p style="font-family:var(--font-body); font-size:28px; color:var(--ink-mute); line-height:1.6;">
        Content paragraph here.
      </p>
    </div>

    <div data-element="shape" style="position:absolute; left:1000px; top:300px; width:800px; height:500px;
         background:var(--panel); border:1px solid var(--line); border-radius:12px;">
    </div>

  </div>
</body>
</html>
```

**Note:** The theme CSS is injected inline by the API at serve time (not via `<link>` to a file), since storage is database-backed. The stored HTML uses CSS variables that resolve against the injected theme.

**Conventions:**
- Fixed canvas: 1920×1080 pixels (16:9 widescreen)
- All elements use absolute positioning within `.slide-container`
- `data-element` attributes (`title`, `body`, `stat`, `shape`, `image`, `chart`) for export fidelity
- Theme CSS variables for colors/fonts — swapping the theme changes the entire deck
- Inline styles for positions and sizes; theme variables for visual properties

### Why HTML (not JSON → rendered)?

| Consideration | HTML | JSON + renderer |
|---------------|------|-----------------|
| LLM output quality | CSS is what LLMs excel at | Would need a mapping layer |
| Rendering | Direct iframe — zero compilation | Needs a rendering engine |
| Styling freedom | Unlimited (any CSS) | Limited to schema properties |
| Export fidelity | What you see = what you export | May have rendering differences |
| Editability | AI edits HTML directly | AI edits structured data |
| Proven at scale | Genspark + PPTAgent validate this | Requires custom validation |
| Absolute positioning | Maps cleanly to PPTX coordinates | Same |

---

## Backend (Go)

### Package Structure

```
pkg/
├── docs/                          # Shared docs infrastructure
│   ├── types.go                   # DocType enum, shared interfaces
│   └── registry.go                # Registry of doc types (slides, future: documents, sheets)
│
├── docs/slides/                   # Slides-specific logic
│   ├── types.go                   # DeckManifest, SlideInfo, SlideContent structs
│   ├── service.go                 # Business logic layer (wraps DocsStore)
│   ├── tools.go                   # Tool implementations (create, write, read, etc.)
│   ├── export_pdf.go              # Multi-page PDF via go-rod
│   ├── export_pptx.go            # Image-based PPTX via custom OOXML writer
│   ├── export_html.go            # Self-contained HTML bundle
│   ├── pptxwriter/                # Minimal PPTX ZIP builder (no external dep)
│   │   ├── writer.go             # ZIP assembly + OOXML XML generation
│   │   └── templates.go          # XML template strings for slide/rels/content_types
│   └── themes/                    # Embedded theme CSS files (go:embed)
│       ├── embed.go              # //go:embed directives
│       ├── dark_minimal.css
│       ├── light_corporate.css
│       ├── vibrant.css
│       ├── gradient.css
│       └── terminal_dev.css

pkg/store/
├── docs.go                        # DocsStore interface definition

pkg/store/entstore/
├── team_docs.go                   # teamDocsStore implementation
├── personal_docs.go               # personalDocsStore implementation

ent/team/schema/
├── doc.go                         # Doc Ent schema
├── doc_slide.go                   # DocSlide Ent schema

ent/personal/schema/
├── doc.go                         # Doc Ent schema (personal scope)
├── doc_slide.go                   # DocSlide Ent schema (personal scope)
```

### Tools

| Tool | Description | Key Args |
|------|-------------|----------|
| `create_slides` | Create a new slide deck | `title`, `theme`, `description?` |
| `write_slide` | Write/overwrite a single slide's HTML | `deckSlug`, `slideIndex`, `content` (HTML), `title?`, `notes?` |
| `read_slide` | Read back the current HTML of a slide | `deckSlug`, `slideIndex` |
| `update_slide_notes` | Update just speaker notes | `deckSlug`, `slideIndex`, `notes` |
| `reorder_slides` | Change slide ordering | `deckSlug`, `order` (int array) |
| `delete_slide` | Remove a slide | `deckSlug`, `slideIndex` |
| `update_theme` | Switch or replace theme CSS | `deckSlug`, `themeName` or `cssContent` |
| `list_slides_decks` | List all saved decks | (none) |

**Tool registration (following existing pattern):**
```go
// Tools implement the standard pattern:
type CreateSlidesArgs struct {
    Title       string `json:"title" jsonschema:"The presentation title"`
    Theme       string `json:"theme" jsonschema:"Theme: dark-minimal, light-corporate, vibrant, gradient, terminal-dev"`
    Description string `json:"description,omitempty" jsonschema:"Brief description of the deck's purpose"`
}

type CreateSlidesResult struct {
    DeckSlug string `json:"deck_slug"`
    Message  string `json:"message"`
}

func CreateSlides(ctx tool.Context, args CreateSlidesArgs) (CreateSlidesResult, error) {
    svc := ctx.Value(store.ServiceKey).(*store.Services)
    docsStore := svc.Docs  // or svc.PersonalDocs depending on scope

    // 1. Generate ULID-based slug
    // 2. Load theme CSS from embedded templates
    // 3. Create deck in database via docsStore.CreateDeck()
    // 4. Capture docs update for SSE emission (via ChatAgent pending buffer)
    // 5. Return deck slug
}
```

**`read_slide` tool** (critical for iterative refinement):
```go
type ReadSlideArgs struct {
    DeckSlug   string `json:"deck_slug" jsonschema:"The deck slug identifier"`
    SlideIndex int    `json:"slide_index" jsonschema:"0-based slide index to read"`
}

type ReadSlideResult struct {
    HTML  string `json:"html"`
    Title string `json:"title"`
    Notes string `json:"notes"`
}

func ReadSlide(ctx tool.Context, args ReadSlideArgs) (ReadSlideResult, error) {
    // Retrieve slide from DocsStore and return HTML + metadata
    // This allows the LLM to see current content before modifying
}
```

### SSE Event Emission (ChatRunner Drain Pattern)

Following the existing `artifact` event pattern — tools do NOT emit SSE directly:

**1. ChatAgent pending buffer** (`pkg/agent/chat_agent.go`):
```go
type ChatAgent struct {
    // ... existing fields ...

    // Docs update side-channel
    pendingDocsUpdates []DocsUpdateInfo
    docsUpdateMu       sync.Mutex
}

type DocsUpdateInfo struct {
    DocType     string `json:"type"`        // "slides"
    DeckSlug    string `json:"deckSlug"`
    Action      string `json:"action"`      // "created" | "slide_written" | "reordered" | "deleted" | "theme_updated"
    SlideIndex  int    `json:"slideIndex"`
    TotalSlides int    `json:"totalSlides"`
    Title       string `json:"title"`
}

func (c *ChatAgent) CaptureDocsUpdate(info DocsUpdateInfo) {
    c.docsUpdateMu.Lock()
    defer c.docsUpdateMu.Unlock()
    c.pendingDocsUpdates = append(c.pendingDocsUpdates, info)
}

func (c *ChatAgent) DrainDocsUpdates() []DocsUpdateInfo {
    c.docsUpdateMu.Lock()
    defer c.docsUpdateMu.Unlock()
    updates := c.pendingDocsUpdates
    c.pendingDocsUpdates = nil
    return updates
}
```

**2. afterToolCallback capture** (`pkg/agent/chat_agent_run.go`):
```go
// In afterToolCallback, after success check:
if err == nil {
    switch t.Name() {
    case "create_slides", "write_slide", "reorder_slides", "delete_slide", "update_theme":
        // Extract DocsUpdateInfo from tool output
        if info, ok := extractDocsUpdateFromOutput(output); ok {
            c.CaptureDocsUpdate(info)
        }
    }
}
```

**3. ChatRunner drain** (`pkg/api/chat_runner.go`):
```go
// In drainImagesAndFlowOutput (or renamed drainSideChannels):
for _, du := range chatAgent.DrainDocsUpdates() {
    cr.emitEvent("docs_update", map[string]any{
        "type":        du.DocType,
        "deckSlug":    du.DeckSlug,
        "action":      du.Action,
        "slideIndex":  du.SlideIndex,
        "totalSlides": du.TotalSlides,
        "title":       du.Title,
    })
}
```

**4. Persistence for session reload** (`pkg/api/chat_utils.go`):
```go
const docsUpdatePrefix = "[docs_update]"

func persistDocsUpdate(ctx context.Context, svc session.Service, userID, sessionID string, info DocsUpdateInfo) {
    data, _ := json.Marshal(info)
    text := docsUpdatePrefix + string(data)
    persistSessionMessage(ctx, svc, userID, sessionID, "model", text)
}

func tryParseDocsUpdateMessage(text string) (*DocsUpdateInfo, bool) {
    if !strings.HasPrefix(text, docsUpdatePrefix) {
        return nil, false
    }
    var info DocsUpdateInfo
    if err := json.Unmarshal([]byte(strings.TrimPrefix(text, docsUpdatePrefix)), &info); err != nil {
        return nil, false
    }
    return &info, true
}
```

### API Endpoints

```
GET    /api/docs                                    # List all docs (all types)
GET    /api/docs?type=slides                        # List slide decks only

GET    /api/docs/slides/{deckSlug}                  # Get deck manifest (JSON)
GET    /api/docs/slides/{deckSlug}/slides/{idx}     # Serve slide HTML (iframe src)
DELETE /api/docs/slides/{deckSlug}                   # Delete deck

POST   /api/docs/slides/{deckSlug}/export/pdf       # Generate + return PDF
POST   /api/docs/slides/{deckSlug}/export/pptx      # Generate + return PPTX
POST   /api/docs/slides/{deckSlug}/export/html      # Generate + return standalone HTML

GET    /api/docs/slides/{deckSlug}/present          # Self-contained presenter HTML (new window)

GET    /api/docs/slides/themes                      # List available theme names
```

**Slide serving endpoint** (`GET /api/docs/slides/{deckSlug}/slides/{idx}`):
- Reads slide HTML from database
- Injects theme CSS inline (replaces `<style>` placeholder or prepends to `<head>`)
- Returns complete HTML document ready for iframe rendering
- Sets appropriate cache headers

**Presenter endpoint** (`GET /api/docs/slides/{deckSlug}/present`):
- Returns a self-contained HTML page with all slides + navigation JS
- Can be opened in a separate browser window (external display)
- Contains keyboard shortcuts, slide counter, speaker notes toggle
- Zero React overhead — vanilla JS

### Export Pipeline

#### PDF Export (extending existing `pkg/pdfgen/chrome.go`)

```go
func ExportPDF(ctx context.Context, docsStore store.DocsStore, deckSlug string, serverBaseURL string) ([]byte, error) {
    // 1. Load deck manifest from DB
    deck, _ := docsStore.GetDeck(ctx, deckSlug)

    // 2. Launch go-rod browser (reuse pool from pkg/pdfgen)
    browser := rod.New().MustConnect()
    defer browser.MustClose()

    // 3. For each slide:
    //    a. Navigate to {serverBaseURL}/api/docs/slides/{slug}/slides/{i}
    //    b. Set viewport 1920×1080
    //    c. Wait for rendering: page.MustEval(`() => document.fonts.ready`)
    //    d. page.PDF() with:
    //       - Landscape orientation
    //       - Custom page size: 338.67mm × 190.5mm (16:9 at 144dpi)
    //       - No margins
    //       - PrintBackground: true
    // 4. Merge per-slide PDFs into single multi-page PDF
    // 5. Return combined PDF bytes
}
```

#### PPTX Export (custom minimal writer, no external dependency)

The PPTX format is a ZIP archive containing XML files. For image-only slides, the structure is well-defined and simple:

```go
// pkg/docs/slides/pptxwriter/writer.go
package pptxwriter

// Create creates a valid .pptx file with one full-bleed image per slide.
// Each image is a PNG screenshot of the rendered slide HTML.
func Create(slides []SlideImage) ([]byte, error) {
    buf := new(bytes.Buffer)
    zw := zip.NewWriter(buf)

    // 1. Write [Content_Types].xml — declares PNG + XML content types
    // 2. Write _rels/.rels — root relationships
    // 3. Write ppt/presentation.xml — slide list
    // 4. Write ppt/_rels/presentation.xml.rels — slide relationships
    // 5. For each slide:
    //    a. Write ppt/slides/slide{N}.xml — references image
    //    b. Write ppt/slides/_rels/slide{N}.xml.rels — image relationship
    //    c. Write ppt/media/image{N}.png — the screenshot bytes
    // 6. Write ppt/slideLayouts/slideLayout1.xml — blank layout
    // 7. Write ppt/slideMasters/slideMaster1.xml — blank master

    zw.Close()
    return buf.Bytes(), nil
}

type SlideImage struct {
    PNG []byte // Screenshot at 1920×1080
}
```

The XML templates for each file are ~20-50 lines of static OOXML. Total implementation: ~200 lines of Go, zero external dependencies.

```go
// pkg/docs/slides/export_pptx.go
func ExportPPTX(ctx context.Context, docsStore store.DocsStore, deckSlug string, serverBaseURL string) ([]byte, error) {
    deck, _ := docsStore.GetDeck(ctx, deckSlug)

    // 1. Launch go-rod browser
    // 2. For each slide:
    //    - Navigate to slide URL
    //    - Set viewport 1920×1080
    //    - page.MustScreenshot() → PNG bytes
    // 3. Pass []SlideImage to pptxwriter.Create()
    // 4. Return .pptx bytes
}
```

**V2 enhancement:** After creating the base image-only PPTX, optionally parse slide HTML (using `golang.org/x/net/html`) to extract `data-element` nodes with their positions/text, and add invisible text frames overlaid on the images for searchability and accessibility.

#### Standalone HTML Export

```go
func ExportHTML(ctx context.Context, docsStore store.DocsStore, deckSlug string) ([]byte, error) {
    deck, _ := docsStore.GetDeck(ctx, deckSlug)

    // 1. Load all slides from DB
    // 2. Build single HTML file:
    //    a. Inline theme CSS in a <style> block
    //    b. Each slide becomes a <section class="slide" data-index="N">
    //       with its content (strip <html>/<head>/<body> wrappers)
    //    c. Fetch and base64-encode any external images referenced in slides
    //    d. Add navigation JS:
    //       - Arrow keys / Space for next/prev
    //       - Escape for overview mode (thumbnail grid)
    //       - Slide counter overlay
    //    e. Add CSS for slide transitions (fade or instant)
    //    f. Only show one slide at a time (display:none for others)
    // 3. Return self-contained HTML bytes (zero external dependencies)
}
```

---

## Frontend (React)

### Component Structure

```
web/src/components/docs/
├── DocsView.jsx              # Main sidebar view (#/studio/docs)
├── DocsList.jsx              # Grid/list of all docs with type filters
├── slides/
│   ├── SlidesCard.jsx        # In-chat card (rendered during generation)
│   ├── SlidesViewer.jsx      # Full viewer (iframe + nav + controls)
│   ├── SlideNavigator.jsx    # Thumbnail strip + dot indicator + counter
│   ├── PresenterMode.jsx     # React wrapper that opens /present in new window
│   └── SlidesExport.jsx      # Export dropdown (PDF / PPTX / HTML)
```

### SlidesCard (in-chat inline card)

Displayed in the chat stream when the frontend receives `docs_update` SSE events. Renders the current slide in a scaled iframe with navigation controls.

```
┌──────────────────────────────────────────────────────┐
│  ┌─ Introducing Astonish ─────────────── 4 / 13 ─┐  │
│  │                                                 │  │
│  │   [Scaled iframe: 1920×1080 → ~500px wide]     │  │
│  │   src="/api/docs/slides/{slug}/slides/4"        │  │
│  │                                                 │  │
│  └─────────────────────────────────────────────────┘  │
│                                                        │
│  ◀ Prev    ● ● ● ● ○ ○ ○ ○ ○ ○ ○ ○ ○    Next ▶     │
│                                                        │
│  [⛶ Present]  [↓ Export ▾]  [📂 Open in Docs]        │
└──────────────────────────────────────────────────────┘
```

**Behavior:**
- Appears when first `docs_update` with `action: "created"` is received
- Updates live as `action: "slide_written"` events arrive (increments total, optionally auto-advances to latest slide)
- Navigation enabled immediately — user can browse already-written slides while AI writes more
- After generation completes: card is "finalized" (no more live updates expected)
- Clicking "Open in Docs" navigates to `#/studio/docs/slides/{deckSlug}`
- Clicking "Present" opens `/api/docs/slides/{slug}/present` in a new window

**Iframe rendering:**
```jsx
<div style={{ width: containerWidth, height: containerWidth * (9/16), position: 'relative', overflow: 'hidden' }}>
  <iframe
    src={`/api/docs/slides/${deckSlug}/slides/${currentSlide}`}
    style={{
      width: '1920px',
      height: '1080px',
      transform: `scale(${containerWidth / 1920})`,
      transformOrigin: 'top left',
      border: 'none',
      pointerEvents: 'none'  // Prevent interaction in preview mode
    }}
  />
</div>
```

### DocsView (sidebar, `#/studio/docs`)

```
┌─────────────────────────────────────────────────────────┐
│  Docs                                           [Filter] │
│─────────────────────────────────────────────────────────│
│                                                          │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │              │  │              │  │              │  │
│  │  [thumbnail] │  │  [thumbnail] │  │  [thumbnail] │  │
│  │              │  │              │  │              │  │
│  ├──────────────┤  ├──────────────┤  ├──────────────┤  │
│  │ Astonish     │  │ Q2 Review    │  │ Onboarding   │  │
│  │ Introduction │  │              │  │ Guide        │  │
│  │ 13 slides    │  │ 8 slides     │  │ 20 slides    │  │
│  │ Jun 22, 2026 │  │ Jun 20, 2026 │  │ Jun 18, 2026 │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
│                                                          │
│  ─────────────────────────────────────────────────────  │
│  [Improve with AI]  Opens chat with deck as context     │
└─────────────────────────────────────────────────────────┘
```

**Features:**
- Grid of deck cards with first-slide thumbnails (rendered via scaled iframe or cached screenshot)
- Click opens `SlidesViewer` (full-page viewer with navigation)
- Type filter (when Documents/Sheets are added later)
- "Improve with AI" button → starts a new chat session with the deck slug in context
- Export/delete actions per deck (context menu or action buttons)

### Presenter Mode (dual approach)

**Backend route** (`GET /api/docs/slides/{slug}/present`):
- Self-contained HTML page with all slides inlined
- Keyboard navigation (arrows, space, escape)
- Speaker notes toggle (N key)
- Slide counter overlay
- Works in external windows / second monitors
- Zero React dependency — pure vanilla JS + CSS

**React wrapper** (`PresenterMode.jsx`):
- Button in SlidesCard and SlidesViewer that calls `window.open('/api/docs/slides/{slug}/present', '_blank')`
- Also provides an in-app fullscreen overlay option (for quick previews without opening a new window)
- Fullscreen overlay uses the same iframe approach but at full viewport

### SSE Subscription

The chat component subscribes to `docs_update` events in the SSE stream:

```jsx
// In chat message stream handler (same pattern as artifact/app_preview):
case 'docs_update':
  const data = JSON.parse(event.data);
  if (data.type === 'slides') {
    updateOrCreateSlidesCard(data.deckSlug, {
      action: data.action,
      slideIndex: data.slideIndex,
      totalSlides: data.totalSlides,
      title: data.title,
    });
  }
  break;
```

**Session reload**: When loading an existing session, `tryParseDocsUpdateMessage` detects persisted `[docs_update]` events and reconstructs the SlidesCard state.

---

## Pre-Built Themes

### 5 Shipped Themes

| # | Name | Surface | Accent | Font Stack | Target Audience |
|---|------|---------|--------|------------|-----------------|
| 1 | **dark-minimal** | Near-black `#0B0D0F` | Violet `#8B5CF6` | `system-ui` + `ui-monospace` | Developer/tech talks |
| 2 | **light-corporate** | White `#FFFFFF` | Navy `#1E40AF` | `system-ui` + `ui-monospace` | Business/boardroom |
| 3 | **vibrant** | Dark slate `#0F172A` | Multi (teal/amber/rose) | `system-ui` + `ui-monospace` | Startup pitches |
| 4 | **gradient** | Dark `#09090B` + blurs | Purple-to-blue gradient | `system-ui` + `ui-monospace` | Modern SaaS / product |
| 5 | **terminal-dev** | Pure black `#000000` | Green `#22C55E` | `ui-monospace` only | Hacker/dev aesthetic |

**V1: System fonts only.** All themes use native font stacks:
- Display: `system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif`
- Monospace: `ui-monospace, 'SF Mono', 'Cascadia Code', 'Consolas', monospace`

**V2: Custom web fonts** — add Google Fonts with local caching/embedding for export.

### Theme CSS Structure

```css
/* Example: dark-minimal theme (V1 — system fonts) */
:root {
  /* Colors */
  --surface: #0B0D0F;
  --panel: #15181C;
  --panel-elevated: #1C2026;
  --line: rgba(255, 255, 255, 0.08);
  --line-strong: rgba(255, 255, 255, 0.14);
  --ink: #ECEDEE;
  --ink-mute: #94A3B8;
  --ink-dim: #5B6470;
  --accent: #8B5CF6;
  --accent-strong: #7C3AED;
  --accent-soft: #A78BFA;
  --accent-ink: #FFFFFF;

  /* Typography (system fonts — no loading required) */
  --font-display: system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif;
  --font-body: system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif;
  --font-mono: ui-monospace, 'SF Mono', 'Cascadia Code', 'Consolas', monospace;

  /* Spacing & Radii */
  --margin-edge: 96px;
  --radius-sm: 6px;
  --radius-md: 10px;
  --radius-lg: 16px;
}

/* Base slide styles */
.slide-container {
  font-family: var(--font-body);
  color: var(--ink);
  background: var(--surface);
}

/* Utility classes available to slides */
.mono { font-family: var(--font-mono); }
.accent { color: var(--accent); }
.muted { color: var(--ink-mute); }
.panel {
  background: var(--panel);
  border: 1px solid var(--line);
  border-radius: var(--radius-md);
}
```

---

## LLM System Prompt Guidance

Injected conditionally when the user's message references presentations/slides/deck (keyword detection), or when a deck is already in context for the active session.

```markdown
## Creating Presentations (Docs → Slides)

When the user asks you to create a presentation, pitch deck, slides, or any visual slide-based document:

### Workflow

1. Use `create_slides` to initialize the deck. Choose an appropriate theme:
   - **dark-minimal** — Developer/tech audience, terminal-influenced, Astonish brand violet
   - **light-corporate** — Business/boardroom, clean white, navy accents
   - **vibrant** — Startup pitch, energetic, multi-color accents on dark slate
   - **gradient** — Modern SaaS/product, dark with gradient blur halos
   - **terminal-dev** — Hacker aesthetic, pure black, green monospace

2. Use `write_slide` for each slide, one at a time. Generate complete HTML per slide.

3. To modify an existing slide, first use `read_slide` to see its current content, then `write_slide` to overwrite it.

4. After writing all slides, summarize what you created and mention export options.

### Slide HTML Rules

Each slide MUST follow this structure:
- DOCTYPE + html + head (with empty <style> tag — theme is injected at serve time) + body
- Single `.slide-container` div: exactly 1920×1080px, position:relative, overflow:hidden
- All content elements use absolute positioning inside the container
- Add `data-element` attributes: "title", "body", "stat", "shape", "image", "chart"
- Use theme CSS variables for all colors and fonts (NEVER hardcode colors or font names)
- Use inline `style` for positions and dimensions

### Design Principles

- **One idea per slide** — don't overcrowd
- **Title**: 48-72px, font-weight 700, positioned top area
- **Body text**: 24-32px minimum for readability at presentation scale
- **Consistent margins**: 96px from canvas edges (use var(--margin-edge))
- **Accent for emphasis**: stats, highlights, labels, decorative elements
- **Shapes via CSS**: border-radius, gradients, borders, box-shadow — no external images for decoration
- **Mono labels**: Use var(--font-mono) for chapter numbers, categories, metadata
- **Hierarchy**: Every slide has a clear visual hierarchy (eyebrow → title → body → detail)
- **Images**: Use `https://` URLs for images. For decorative elements, use CSS only.

### Typical Deck Structure

| Slide | Purpose | Layout Style |
|-------|---------|-------------|
| 1 | Cover | Centered title, tagline, minimal |
| 2-3 | Problem / Context | Text-heavy or stat-forward |
| 4-N | Content slides | Vary: two-column, cards grid, full-bleed, diagram |
| N+1 | Closing | CTA, contact info, links |

### Speaker Notes

Pass notes via the `notes` parameter on `write_slide`. Notes are for the presenter,
not the audience. Keep them conversational: what to say, transitions, timing cues.

### Modifying Existing Slides

When asked to improve or modify a specific slide:
1. Use `read_slide` to retrieve the current HTML
2. Modify the HTML as needed
3. Use `write_slide` to save the updated version
4. If asked to change the overall look, use `update_theme` instead of modifying individual slides
```

**Injection strategy**: Only inject when:
1. User's message contains keywords: "presentation", "slides", "deck", "pitch"
2. The active session already has a docs_update event (deck in progress)
3. Similar to how browser tool guidance is conditionally injected

---

## Error Handling & Recovery

### Partial Generation Failure

If the LLM errors mid-deck (rate limit, context overflow, etc.):
- The deck remains in a valid state with slides 1..N viewable
- `slide_count` in the DB reflects only successfully written slides
- The user can ask the AI to "continue" and it will read the manifest to know where to resume
- `read_slide` allows the AI to check what already exists

### Manifest Atomicity

All database writes use Ent's transaction support:
```go
func (s *teamDocsStore) WriteSlide(ctx context.Context, deckSlug string, index int, slide *SlideContent) error {
    return s.client.WithTx(ctx, func(tx *ent.Tx) error {
        // 1. Upsert slide content
        // 2. Update deck slide_count if this is a new slide
        // 3. Update deck updated_at
        return nil
    })
}
```

### Slide Validation

The `write_slide` tool validates HTML before accepting:
```go
func validateSlideHTML(html string) error {
    // 1. Must parse as valid HTML (golang.org/x/net/html)
    // 2. Must contain a .slide-container element
    // 3. Must not exceed size limit (e.g., 100KB — prevents accidental huge content)
    // 4. Must not contain <script> tags (security)
}
```

### Image Handling

**V1 strategy:**
- Allow `https://` image URLs in slide HTML (simplest)
- For iframe preview: browser loads images directly
- For PDF/PPTX export: go-rod renders them (has network access)
- For standalone HTML export: fetch images server-side → base64-encode inline
- LLM is guided to use `data-element="image"` on image containers

---

## Platform Sharing Architecture (Future)

The database-first design means Platform sharing requires minimal changes:

| Aspect | V1 (Personal) | V2 (Platform) |
|--------|----------------|----------------|
| Storage | Personal SQLite (same as PersonalApps) | Team PostgreSQL (same as team Apps) |
| Deck slug | Unique per user | Unique per team |
| Access | Single user | ACL: personal / team / org visibility |
| SSE events | Same session only | Broadcast to all viewers |
| API paths | Identical | Identical (store resolved by tenant middleware) |
| Presenter URL | Local only | Shareable link (authenticated) |

**What needs to change for Platform sharing:**
1. Add `DocsStore` to `TeamDataStore` interface (same pattern as `AppStore`)
2. Add team Ent schema (likely identical to personal)
3. Wire in `TenantMiddleware` (resolves correct store per request)
4. Add `published_by` field for team-published decks
5. Broadcast `docs_update` SSE to all active sessions viewing the same deck

---

## Dependencies

### Go (go.mod additions)

```
# No new external dependencies for V1-V2!
# PPTX writer is custom code (~200 lines) using only:
#   - archive/zip (stdlib)
#   - encoding/xml (stdlib)
#   - bytes (stdlib)

# Already in go.mod:
github.com/go-rod/rod         # PDF export + PPTX screenshots
github.com/oklog/ulid/v2      # Deck ID generation (already indirect dep)
golang.org/x/net/html         # HTML parsing for validation (stdlib-adjacent)
```

### Frontend (web/package.json)

**No new dependencies.** Uses:
- React 19 (existing) — component rendering
- Tailwind CSS (existing) — DocsView styling
- `file-saver` (existing) — export file downloads
- Native `<iframe>` — slide rendering

---

## Implementation Phases

### Phase 1: Core Foundation (2-3 weeks)

**Backend:**
- [ ] `pkg/store/docs.go` — `DocsStore` interface definition
- [ ] `ent/personal/schema/doc.go` + `doc_slide.go` — Ent schemas
- [ ] `ent/team/schema/doc.go` + `doc_slide.go` — Ent schemas
- [ ] `go generate ./ent/personal && go generate ./ent/team`
- [ ] `pkg/store/entstore/personal_docs.go` — DocsStore implementation
- [ ] `pkg/store/entstore/team_docs.go` — DocsStore implementation
- [ ] Wire into `Services` container + `TenantMiddleware`
- [ ] `pkg/docs/slides/types.go` — domain types
- [ ] `pkg/docs/slides/service.go` — business logic layer
- [ ] `pkg/docs/slides/tools.go` — `create_slides`, `write_slide`, `read_slide`, `update_theme`
- [ ] `pkg/docs/slides/themes/` — 5 embedded CSS theme files
- [ ] `pkg/agent/chat_agent.go` — add `pendingDocsUpdates` buffer + `DrainDocsUpdates()`
- [ ] `pkg/agent/chat_agent_run.go` — capture docs updates in `afterToolCallback`
- [ ] `pkg/api/chat_runner.go` — drain + emit `docs_update` SSE events
- [ ] `pkg/api/chat_utils.go` — persist/parse docs update markers
- [ ] API routes: slide serving, deck CRUD, theme list
- [ ] System prompt guidance (conditional injection)
- [ ] Register tools in tool registry

**Frontend:**
- [ ] `web/src/components/docs/slides/SlidesCard.jsx` — in-chat card with iframe + nav
- [ ] `web/src/components/docs/DocsView.jsx` — sidebar section, deck grid
- [ ] `web/src/components/docs/slides/SlidesViewer.jsx` — full viewer (opened from sidebar)
- [ ] SSE handler for `docs_update` events in chat stream
- [ ] Session reload: reconstruct SlidesCard from persisted markers
- [ ] Sidebar entry: icon + "Docs" label, routes to `#/studio/docs`

### Phase 2: Export + Presenter Mode (1-2 weeks)

- [ ] `pkg/docs/slides/export_pdf.go` — multi-page landscape PDF via go-rod
- [ ] `pkg/docs/slides/export_html.go` — bundled self-navigating HTML (with image inlining)
- [ ] Export API endpoints (`POST /api/docs/slides/{slug}/export/pdf`, `/html`)
- [ ] `GET /api/docs/slides/{slug}/present` — self-contained presenter HTML page
- [ ] `SlidesExport.jsx` — export dropdown component (PDF, HTML)
- [ ] `PresenterMode.jsx` — opens /present in new window + in-app fullscreen overlay
- [ ] `SlideNavigator.jsx` — thumbnail dots + counter
- [ ] Additional tools: `reorder_slides`, `delete_slide`, `update_slide_notes`

### Phase 3: PPTX Export (1-2 weeks)

- [ ] `pkg/docs/slides/pptxwriter/writer.go` — minimal OOXML ZIP builder (~200 lines)
- [ ] `pkg/docs/slides/pptxwriter/templates.go` — XML template strings
- [ ] `pkg/docs/slides/export_pptx.go` — screenshot slides → pptxwriter.Create()
- [ ] PPTX export API endpoint
- [ ] Add PPTX option to `SlidesExport.jsx` dropdown
- [ ] Unit tests for pptxwriter (verify output opens in PowerPoint/LibreOffice)

### Phase 4: Polish & Refinement (1 week)

- [ ] "Improve with AI" button in DocsView (starts chat with deck slug in context)
- [ ] Deck thumbnails in DocsView (render first slide, cache as data URL)
- [ ] Theme preview/swatches in SlidesCard
- [ ] Delete confirmation dialog
- [ ] Error states: loading, failed export, empty deck
- [ ] Slide validation in `write_slide` tool (well-formed HTML, size limits, no scripts)
- [ ] Conditional system prompt injection (keyword detection)

### Phase 5: Future Growth

- [ ] Custom web fonts (Google Fonts with local caching + embedding for export)
- [ ] PPTX import (parse theme → generate theme.css, parse content → generate slide HTML)
- [ ] Platform sharing (team scope, ACL, shared presenter URLs)
- [ ] "Documents" doc type (rich text / Markdown → DOCX export)
- [ ] "Spreadsheets" doc type (structured data → HTML tables → XLSX)
- [ ] Versioning / history (track slide modifications, diff view)
- [ ] Collaborative editing in Platform mode
- [ ] Deck templates (pre-built starter decks for common scenarios)
- [ ] PPTX text overlay extraction (V2 export enhancement)

---

## User Journey (Example)

```
User: "Create a presentation about our new microservices migration.
       10 slides, for engineering leadership. Focus on risk
       mitigation and timeline."

Agent: [calls create_slides(title="Microservices Migration",
        theme="light-corporate",
        description="Engineering leadership pitch: risk + timeline")]
       → afterToolCallback captures DocsUpdateInfo{action:"created"}
       → ChatRunner drains → emitEvent("docs_update", {...})
       → SlidesCard appears in chat (empty, title shown)

       [calls write_slide(deckSlug, 0, "<html>...cover slide...", title="Cover")]
       → SSE: docs_update {action:"slide_written", slideIndex:0, totalSlides:1}
       → SlidesCard renders slide 0 in iframe

       [calls write_slide(deckSlug, 1, "<html>...current architecture...")]
       → SSE: docs_update {action:"slide_written", slideIndex:1, totalSlides:2}
       → SlidesCard updates counter to "1 / 2"

       ... (continues for all 10 slides) ...

       [After final slide:]
       "I've created a 10-slide deck covering the migration approach,
        service dependency graph, risk matrix by service, rollback
        strategy, timeline with milestones, and team allocation.

        You can navigate through it inline, open in presenter mode
        (full screen), or export to PDF/PPTX/HTML from the Docs section."

User: "The risk slide needs more detail. Add a comparison table
       for each service."

Agent: [calls read_slide(deckSlug, 4)]
       → Returns current HTML of slide 5 (0-indexed: 4)

       [calls write_slide(deckSlug, 4, "<html>...updated risk slide with table...")]
       → SlidesCard updates, auto-navigates to slide 4

       "Updated slide 5 with a risk comparison table showing
        probability, impact, and mitigation strategy per service."

User: "Switch to the dark theme, this will be presented in a dim room."

Agent: [calls update_theme(deckSlug, "dark-minimal")]
       → All slides now render with dark-minimal CSS variables
       → SlidesCard iframe refreshes

       "Switched to the dark-minimal theme. All slides now use
        the dark background with violet accents."
```

---

## Risk Assessment

| Risk | Likelihood | Impact | Mitigation |
|------|-----------|--------|------------|
| LLM produces inconsistent slide quality | Medium | High | Strong system prompt guidance + theme constraints + `data-element` conventions |
| System font rendering varies across OS | Low | Low | System fonts are designed for cross-platform consistency; export via go-rod uses Chromium's font renderer |
| Performance (10+ go-rod page loads for export) | Low | Medium | Reuse single browser instance; parallel tab rendering |
| Scope creep (editor, collaboration) | High | High | Strict V1 = AI-only; defer manual editing entirely |
| Token cost (one tool call per slide) | Low | Low | ~500-2000 tokens per slide HTML; 10 slides = 5-20K output tokens |
| Database storage for HTML | Low | Low | Slide HTML is 2-10KB each; 100 decks x 15 slides = ~15MB (trivial) |
| PPTX image quality | Low | Medium | Screenshots at 1920x1080 → excellent quality for projection; PNG compression keeps file sizes reasonable |

---

## Comparison: Genspark vs Astonish Docs

| Aspect | Genspark | Astonish Docs |
|--------|----------|---------------|
| Rendering | Cloud-rendered iframes + screenshots | Local iframe rendering (same-origin API) |
| Storage | Cloud project/git-backed | Database (SQLite local / PostgreSQL platform) |
| Export | Server-side only (cloud) | Server-side via go-rod + custom PPTX writer |
| Editing | AI-only iteration (cloud LLM) | AI-only (V1), uses Astonish's own agent |
| Verification | Screenshot + geometry analysis | Live iframe preview (instant feedback) |
| Theme source | Uploaded PPTX or style picker | 5 built-in + AI-generated + future PPTX import |
| PPTX quality | Likely native element mapping | Image-based (V1) → hybrid text overlay (V2) |
| Presentation mode | Cloud-hosted viewer | Local presenter HTML + React fullscreen |
| Collaboration | Multi-user cloud | Personal V1 → Platform sharing V2 |
| Integration | Standalone product | Integrated in Astonish agent workflow (flows, memory, tools) |
| Font handling | Custom fonts (Google Fonts) | System fonts V1, custom fonts V2 |
| Dependencies | Proprietary | Zero new external Go deps; custom PPTX writer |

---

## Technical Decisions Log

| Decision | Choice | Rationale |
|----------|--------|-----------|
| Internal format | HTML (absolute positioning) | Maximum LLM freedom, proven by Genspark + PPTAgent, direct iframe rendering |
| Storage | Database (Ent/entstore pattern) | Consistent with apps/sessions/memories; no filesystem→DB migration needed |
| Deck ID | ULID-based slug | No collision risk, sortable by time, URL-safe, works with non-ASCII titles |
| PPTX generation | Custom minimal OOXML writer | Zero external deps, fully open-source, image-only is ~200 lines of Go |
| PDF engine | `go-rod` (already in codebase) | Proven in `pkg/pdfgen/chrome.go`, pixel-perfect, zero new deps |
| Slide dimensions | 1920×1080 (16:9) | Industry standard widescreen |
| PPTX export strategy | Image-based (V1) → hybrid (V2) | Pragmatic quality vs effort trade-off |
| UI category | "Docs" with sidebar section | Extensible to Documents and Sheets later |
| Trigger mechanism | Natural language + conditional guidance | Lowest friction, consistent with Astonish UX |
| Theme system | 5 pre-built + CSS variables | Strong defaults with full customizability |
| Font strategy | System fonts V1, custom V2 | Avoids font-loading complexity in export; guaranteed cross-platform rendering |
| Editing mode | AI-only (V1) | Keeps scope manageable; `read_slide` enables iterative refinement |
| SSE emission | ChatRunner drain pattern | Architectural consistency; tools don't emit directly |
| Presenter mode | API endpoint + React wrapper | External window for real presentations; in-app for quick preview |
| Sharing architecture | DocsStore interface (same as AppStore) | Team/Org scope is additive, not a rewrite |
