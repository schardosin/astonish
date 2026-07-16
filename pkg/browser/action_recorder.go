package browser

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

// ActionEvent is one captured DOM interaction during action capture.
type ActionEvent struct {
	T        int64  `json:"t"` // ms since capture start (best-effort)
	Type     string `json:"type"`
	Selector string `json:"selector,omitempty"`
	Label    string `json:"label,omitempty"`
	Value    string `json:"value,omitempty"`
	URL      string `json:"url,omitempty"`
	Key      string `json:"key,omitempty"`
}

// actionCaptureState tracks DOM action recording on the managed browser.
type actionCaptureState struct {
	mu           sync.Mutex
	active       bool
	forHandoff   bool
	removeScript func() error
}

// actionRecorderJS installs listeners that append to window.__astonishActionLog.
// Re-injected via Page.addScriptToEvaluateOnNewDocument for navigations.
const actionRecorderJS = `(function() {
  if (window.__astonishActionRecorderInstalled) {
    window.__astonishActionCaptureEnabled = true;
    return;
  }
  window.__astonishActionRecorderInstalled = true;
  window.__astonishActionCaptureEnabled = true;
  window.__astonishActionLog = window.__astonishActionLog || [];
  const start = Date.now();

  function cssEscape(s) {
    if (window.CSS && CSS.escape) return CSS.escape(s);
    return String(s).replace(/[^a-zA-Z0-9_-]/g, '\\$&');
  }

  function stableSelector(el) {
    if (!el || el.nodeType !== 1) return '';
    if (el.id && /^[a-zA-Z][\w:-]*$/.test(el.id)) return '#' + cssEscape(el.id);
    const testid = el.getAttribute('data-testid');
    if (testid) return '[data-testid="' + testid.replace(/"/g, '\\"') + '"]';
    const aria = el.getAttribute('aria-label');
    if (aria) {
      const tag = el.tagName.toLowerCase();
      return tag + '[aria-label="' + aria.replace(/"/g, '\\"') + '"]';
    }
    const name = el.getAttribute('name');
    if (name) {
      const tag = el.tagName.toLowerCase();
      return tag + '[name="' + name.replace(/"/g, '\\"') + '"]';
    }
    const parts = [];
    let cur = el;
    for (let depth = 0; cur && cur.nodeType === 1 && depth < 6; depth++) {
      let part = cur.tagName.toLowerCase();
      const tid = cur.getAttribute('data-testid');
      if (tid) {
        parts.unshift('[data-testid="' + tid.replace(/"/g, '\\"') + '"]');
        break;
      }
      if (cur.id && /^[a-zA-Z][\w:-]*$/.test(cur.id)) {
        parts.unshift('#' + cssEscape(cur.id));
        break;
      }
      const parent = cur.parentElement;
      if (parent) {
        const siblings = Array.from(parent.children).filter(c => c.tagName === cur.tagName);
        if (siblings.length > 1) {
          const idx = siblings.indexOf(cur) + 1;
          part += ':nth-of-type(' + idx + ')';
        }
      }
      parts.unshift(part);
      cur = parent;
      if (!cur || cur === document.body || cur === document.documentElement) break;
    }
    return parts.join(' > ');
  }

  function labelFor(el) {
    if (!el) return '';
    return (el.getAttribute('aria-label')
      || el.getAttribute('data-testid')
      || el.getAttribute('name')
      || (el.innerText || '').trim().slice(0, 80)
      || el.tagName.toLowerCase());
  }

  function push(evt) {
    if (!window.__astonishActionCaptureEnabled) return;
    evt.t = Date.now() - start;
    evt.url = location.href;
    window.__astonishActionLog.push(evt);
  }

  document.addEventListener('click', function(e) {
    const el = e.target && e.target.closest ? e.target.closest('a,button,input,select,textarea,[role="button"],[data-testid],summary') || e.target : e.target;
    push({ type: 'click', selector: stableSelector(el), label: labelFor(el) });
  }, true);

  document.addEventListener('change', function(e) {
    const el = e.target;
    if (!el) return;
    let value = '';
    if (el.type === 'password') value = '***';
    else if ('value' in el) value = String(el.value || '').slice(0, 200);
    push({ type: 'change', selector: stableSelector(el), label: labelFor(el), value: value });
  }, true);

  document.addEventListener('keydown', function(e) {
    if (e.key !== 'Enter' && e.key !== 'Tab') return;
    const el = e.target;
    push({ type: 'keydown', key: e.key, selector: stableSelector(el), label: labelFor(el) });
  }, true);

  let lastURL = location.href;
  setInterval(function() {
    if (location.href !== lastURL) {
      lastURL = location.href;
      push({ type: 'navigate', url: lastURL });
    }
  }, 400);

  window.addEventListener('popstate', function() {
    push({ type: 'navigate', url: location.href });
  });
})();`

const actionCaptureDisableJS = `window.__astonishActionCaptureEnabled = false;`

const actionCaptureClearJS = `window.__astonishActionLog = []; JSON.stringify({cleared:true});`

const actionCaptureGetLogJS = `JSON.stringify(window.__astonishActionLog || []);`

// StartActionCapture injects the DOM action recorder on the current page and
// all future documents in this target.
func (m *Manager) StartActionCapture(forHandoff bool) error {
	pg, err := m.CurrentPage()
	if err != nil {
		return fmt.Errorf("action capture: current page: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.actionCapture == nil {
		m.actionCapture = &actionCaptureState{}
	}
	m.actionCapture.mu.Lock()
	defer m.actionCapture.mu.Unlock()

	remove, err := pg.EvalOnNewDocument(actionRecorderJS)
	if err != nil {
		return fmt.Errorf("action capture: EvalOnNewDocument: %w", err)
	}
	if _, err := pg.Eval(actionRecorderJS); err != nil {
		return fmt.Errorf("action capture: inject: %w", err)
	}
	m.actionCapture.active = true
	m.actionCapture.forHandoff = forHandoff
	m.actionCapture.removeScript = remove
	return nil
}

// StopActionCapture disables capture listeners (log is retained until cleared).
func (m *Manager) StopActionCapture() error {
	m.mu.Lock()
	ac := m.actionCapture
	m.mu.Unlock()
	if ac == nil {
		return nil
	}
	ac.mu.Lock()
	wasActive := ac.active
	remove := ac.removeScript
	ac.active = false
	ac.forHandoff = false
	ac.removeScript = nil
	ac.mu.Unlock()
	if !wasActive {
		return nil
	}

	pg, err := m.CurrentPage()
	if err != nil {
		return nil // browser may already be torn down
	}
	_, _ = pg.Eval(actionCaptureDisableJS)
	if remove != nil {
		_ = remove()
	}
	return nil
}

// ActionCaptureActive reports whether DOM action capture is enabled.
func (m *Manager) ActionCaptureActive() bool {
	m.mu.Lock()
	ac := m.actionCapture
	m.mu.Unlock()
	if ac == nil {
		return false
	}
	ac.mu.Lock()
	defer ac.mu.Unlock()
	return ac.active
}

// GetActionLog drains (reads) the in-page action log without clearing it.
func (m *Manager) GetActionLog() ([]ActionEvent, error) {
	pg, err := m.CurrentPage()
	if err != nil {
		return nil, fmt.Errorf("action log: current page: %w", err)
	}
	res, err := pg.Eval(actionCaptureGetLogJS)
	if err != nil {
		return nil, fmt.Errorf("action log: eval: %w", err)
	}
	raw := res.Value.Str()
	return ParseActionLogJSON(raw)
}

// ClearActionLog empties the in-page action log.
func (m *Manager) ClearActionLog() error {
	pg, err := m.CurrentPage()
	if err != nil {
		return fmt.Errorf("clear action log: current page: %w", err)
	}
	_, err = pg.Eval(actionCaptureClearJS)
	return err
}

// stopActionCaptureIfHandoff stops capture started for a handoff session.
func (m *Manager) stopActionCaptureIfHandoff() {
	m.mu.Lock()
	ac := m.actionCapture
	m.mu.Unlock()
	if ac == nil {
		return
	}
	ac.mu.Lock()
	forHandoff := ac.forHandoff && ac.active
	ac.mu.Unlock()
	if forHandoff {
		_ = m.StopActionCapture()
	}
}

// ParseActionLogJSON parses the JSON array produced by the injected recorder.
func ParseActionLogJSON(raw string) ([]ActionEvent, error) {
	if raw == "" || raw == "null" {
		return nil, nil
	}
	var events []ActionEvent
	if err := json.Unmarshal([]byte(raw), &events); err != nil {
		return nil, fmt.Errorf("parse action log: %w", err)
	}
	return events, nil
}

// PreferStableSelector ranks candidate selectors (for tests / draft helpers).
// Order: data-testid → aria-label → id → other CSS.
func PreferStableSelector(candidates ...string) string {
	score := func(s string) int {
		switch {
		case s == "":
			return -1
		case strings.Contains(s, "data-testid="):
			return 4
		case strings.Contains(s, "aria-label="):
			return 3
		case strings.HasPrefix(s, "#"):
			return 2
		default:
			return 1
		}
	}
	best := ""
	bestScore := -1
	for _, c := range candidates {
		if sc := score(c); sc > bestScore {
			best, bestScore = c, sc
		}
	}
	return best
}
