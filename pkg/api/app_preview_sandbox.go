package api

import (
	"net/http"
	"sync"

	"github.com/schardosin/astonish/web"
)

// appPreviewSandboxHTML is the HTML page served inside the generative-UI
// iframe. It loads pre-bundled scripts via <script src="..."> from the
// same origin. The iframe uses sandbox="allow-scripts allow-same-origin"
// so it can load sub-resources and send auth cookies.
const appPreviewSandboxHTML = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<script>
window.__runtimeError = null;
window.onerror = function(msg, src, line, col, err) {
  window.__runtimeError = msg + ' (at ' + (src||'inline') + ':' + line + ':' + col + ')';
};
</script>
<script src="/api/app-preview/runtime.js"></script>
<script src="/api/app-preview/tailwind.js"></script>
<style type="text/tailwindcss">
  @theme {
    --color-bg-app: #0b1222;
  }
</style>
<style>
  * { box-sizing: border-box; }
  html, body { margin: 0; padding: 0; font-family: system-ui, -apple-system, sans-serif; }
  body { padding: 16px; color: #e5e5e5; }
  html { background: #0b1222 !important; }
  #root { min-height: 20px; }
  /* Force the component's outermost element to be transparent so the sandbox
     themed background (#0b1222 dark / #fafbfe light) shows through. LLMs
     frequently generate bg-gray-950, bg-black, min-h-screen etc. on the root
     container. CSS !important beats Tailwind utilities (which don't use
     !important), and unlike DOM manipulation this survives React re-renders. */
  #root > *:first-child {
    background-color: transparent !important;
    min-height: auto !important;
  }
  #error-display {
    padding: 12px 16px; margin: 8px; border-radius: 8px;
    background: rgba(239, 68, 68, 0.1); border: 1px solid rgba(239, 68, 68, 0.3);
    color: #ef4444; font-size: 13px; font-family: monospace; white-space: pre-wrap;
    display: none;
  }
</style>
</head>
<body class="dark" style="background: transparent;">
<div id="root"></div>
<div id="error-display"></div>
<script>
(function() {
  if (window.__runtimeError) {
    document.getElementById('error-display').style.display = 'block';
    document.getElementById('error-display').textContent = 'Runtime load error: ' + window.__runtimeError;
    window.parent.postMessage({ type: 'render_error', error: 'Runtime load error: ' + window.__runtimeError }, '*');
    return;
  }
  if (!window.Sucrase || !window.React || !window.ReactDOM) {
    var missing = [];
    if (!window.React) missing.push('React');
    if (!window.ReactDOM) missing.push('ReactDOM');
    if (!window.Sucrase) missing.push('Sucrase');
    var msg = 'Runtime globals missing: ' + missing.join(', ') + '. The sandbox runtime bundle may have failed to execute.';
    document.getElementById('error-display').style.display = 'block';
    document.getElementById('error-display').textContent = msg;
    window.parent.postMessage({ type: 'render_error', error: msg }, '*');
    return;
  }

  var RC = window.Recharts || {};
  var rechartsNames = [
    'AreaChart','Area','BarChart','Bar','LineChart','Line','PieChart','Pie','Cell',
    'RadarChart','Radar','PolarGrid','PolarAngleAxis','PolarRadiusAxis',
    'ScatterChart','Scatter','ComposedChart','RadialBarChart','RadialBar',
    'Treemap','Funnel','FunnelChart','Sankey',
    'XAxis','YAxis','ZAxis','CartesianGrid','Tooltip','Legend','ResponsiveContainer',
    'ReferenceLine','ReferenceArea','ReferenceDot','Label','LabelList',
    'Brush','ErrorBar'
  ];
  rechartsNames.forEach(function(name) {
    if (RC[name]) window[name] = RC[name];
  });

  var rootInstance = null;
  var errorEl = document.getElementById('error-display');

  function showError(msg, stack) {
    errorEl.style.display = 'block';
    errorEl.textContent = msg + (stack ? '\n\n' + stack : '');
    window.parent.postMessage({ type: 'render_error', error: msg, stack: stack || '' }, '*');
  }

  // ── Data hooks infrastructure ──────────────────────────────────────
  // Pending request callbacks keyed by requestId
  var __dataCallbacks = {};
  // Active data subscriptions keyed by sourceId → array of setState callbacks
  var __dataSubscriptions = {};
  var __requestCounter = 0;
  // App context — set by parent via set_context message
  var __appName = '';

  function __genRequestId() {
    return 'req-' + (++__requestCounter) + '-' + Math.random().toString(36).substr(2, 6);
  }

  // Listen for data responses from parent
  window.addEventListener('message', function(e) {
    var msg = e.data;
    if (!msg || !msg.type) return;

    if (msg.type === 'data_response' || msg.type === 'action_response' || msg.type === 'ai_response' || msg.type === 'state_response') {
      var cb = __dataCallbacks[msg.requestId];
      if (cb) {
        delete __dataCallbacks[msg.requestId];
        cb(msg);
      }
    }

    if (msg.type === 'data_update') {
      var subs = __dataSubscriptions[msg.sourceId];
      if (subs) {
        subs.forEach(function(cb) { cb(msg.data, msg.error || null); });
      }
    }
  });

  // Send a data/action/ai request to parent and return a Promise.
  // timeoutMs defaults to 30s for data/action, callers can override (e.g. 120s for AI).
  function __requestFromParent(type, payload, timeoutMs) {
    var timeout = timeoutMs || 30000;
    return new Promise(function(resolve) {
      var requestId = __genRequestId();
      __dataCallbacks[requestId] = resolve;
      payload.requestId = requestId;
      payload.type = type;
      window.parent.postMessage(payload, '*');
      setTimeout(function() {
        if (__dataCallbacks[requestId]) {
          delete __dataCallbacks[requestId];
          resolve({ error: 'Request timed out after ' + (timeout / 1000) + ' seconds' });
        }
      }, timeout);
    });
  }

  // ── useAppData hook ────────────────────────────────────────────────
  // Usage: const { data, loading, error, refetch } = useAppData('mcp:server/tool', { args: { query: 'SELECT ...' }, interval: 30000 })
  window.useAppData = function useAppData(sourceId, options) {
    options = options || {};
    var generationRef = React.useRef(0);
    var _s1 = React.useState(null);    var data = _s1[0];    var setData = _s1[1];
    var _s2 = React.useState(true);    var loading = _s2[0]; var setLoading = _s2[1];
    var _s3 = React.useState(null);    var error = _s3[0];   var setError = _s3[1];

    var fetchData = React.useCallback(function() {
      var gen = ++generationRef.current;
      setLoading(true);
      setError(null);
      __requestFromParent('data_request', {
        sourceId: sourceId,
        args: options.args || {}
      }).then(function(resp) {
        // Ignore stale responses from previous sourceId
        if (gen !== generationRef.current) return;
        if (resp.error) {
          setError(resp.error);
          setLoading(false);
        } else {
          setData(resp.data);
          setLoading(false);
        }
      });
    }, [sourceId]);

    React.useEffect(function() {
      fetchData();

      // Subscribe for push updates
      if (!__dataSubscriptions[sourceId]) __dataSubscriptions[sourceId] = [];
      var handler = function(newData, newError) {
        if (newError) { setError(newError); } else { setData(newData); }
        setLoading(false);
      };
      __dataSubscriptions[sourceId].push(handler);

      // Request polling if interval specified
      if (options.interval && options.interval > 0) {
        window.parent.postMessage({
          type: 'data_subscribe',
          sourceId: sourceId,
          args: options.args || {},
          interval: options.interval
        }, '*');
      }

      return function() {
        // Bump generation to invalidate any in-flight requests
        generationRef.current++;
        var subs = __dataSubscriptions[sourceId];
        if (subs) {
          var idx = subs.indexOf(handler);
          if (idx >= 0) subs.splice(idx, 1);
        }
        // Unsubscribe polling
        if (options.interval && options.interval > 0) {
          window.parent.postMessage({ type: 'data_unsubscribe', sourceId: sourceId }, '*');
        }
      };
    }, [sourceId, fetchData]);

    return { data: data, loading: loading, error: error, refetch: fetchData };
  };

  // ── useAppAction hook ──────────────────────────────────────────────
  // Usage: const runQuery = useAppAction('mcp:server/tool'); const result = await runQuery({ query: '...' })
  window.useAppAction = function useAppAction(actionId) {
    return React.useCallback(function(payload) {
      return __requestFromParent('action_request', {
        actionId: actionId,
        payload: payload || {}
      }).then(function(resp) {
        if (resp.error) throw new Error(resp.error);
        return resp.data;
      });
    }, [actionId]);
  };

  // ── useAppAI hook ──────────────────────────────────────────────────
  // Usage: const askAI = useAppAI({ system: 'You are a data analyst.' })
  //        const text = await askAI('Summarize this data', { context: myData })
  window.useAppAI = function useAppAI(options) {
    options = options || {};
    var systemPrompt = options.system || '';
    return React.useCallback(function(prompt, callOptions) {
      callOptions = callOptions || {};
      return __requestFromParent('ai_request', {
        prompt: prompt,
        system: systemPrompt,
        context: callOptions.context || null
      }, 120000).then(function(resp) {
        if (resp.error) throw new Error(resp.error);
        return resp.text;
      });
    }, [systemPrompt]);
  };

  // ── useAppState hook ───────────────────────────────────────────────
  // Provides per-app persistent SQLite database via the backend.
  // Usage:
  //   const db = useAppState();
  //   db.exec('CREATE TABLE IF NOT EXISTS items (id INTEGER PRIMARY KEY, name TEXT)');
  //   const { data, loading, error } = db.query('SELECT * FROM items');
  //   await db.exec('INSERT INTO items (name) VALUES (?)', ['New item']);
  //
  // Global mutation counter — shared across all useAppState instances.
  // Incremented on every db.exec(), causes all db.query() hooks to re-fetch.
  var __stateVersion = { current: 0 };
  var __stateVersionListeners = [];

  function __notifyStateChange() {
    __stateVersion.current++;
    __stateVersionListeners.forEach(function(fn) { fn(__stateVersion.current); });
  }

  window.useAppState = function useAppState() {
    // Subscribe to version changes for reactive re-fetching
    var _v = React.useState(__stateVersion.current);
    var setVersion = _v[1];

    React.useEffect(function() {
      __stateVersionListeners.push(setVersion);
      return function() {
        var idx = __stateVersionListeners.indexOf(setVersion);
        if (idx >= 0) __stateVersionListeners.splice(idx, 1);
      };
    }, [setVersion]);

    var execFn = React.useCallback(function(sql, params) {
      return __requestFromParent('state_exec', {
        appName: __appName,
        sql: sql,
        params: params || []
      }).then(function(resp) {
        if (resp.error) throw new Error(resp.error);
        __notifyStateChange();
        return resp.data;
      });
    }, []);

    var queryFn = React.useCallback(function(sql, params) {
      // queryFn is the raw async query — used internally by the query hook
      return __requestFromParent('state_query', {
        appName: __appName,
        sql: sql,
        params: params || []
      }).then(function(resp) {
        if (resp.error) throw new Error(resp.error);
        return resp.data;
      });
    }, []);

    // query() returns { data, loading, error } and reactively re-fetches on mutations
    var queryHook = React.useCallback(function useQuery(sql, params) {
      var _d = React.useState(null);   var data = _d[0];    var setData = _d[1];
      var _l = React.useState(true);   var loading = _l[0]; var setLoading = _l[1];
      var _e = React.useState(null);   var err = _e[0];     var setErr = _e[1];
      var genRef = React.useRef(0);
      var versionRef = React.useRef(-1);

      // Subscribe to version changes
      var _ver = React.useState(__stateVersion.current);
      var version = _ver[0]; var setVer = _ver[1];

      React.useEffect(function() {
        __stateVersionListeners.push(setVer);
        return function() {
          var idx = __stateVersionListeners.indexOf(setVer);
          if (idx >= 0) __stateVersionListeners.splice(idx, 1);
        };
      }, [setVer]);

      // Serialize params for dependency tracking
      var paramsKey = JSON.stringify(params || []);

      React.useEffect(function() {
        if (!sql) return;
        var gen = ++genRef.current;
        setLoading(true);
        setErr(null);
        __requestFromParent('state_query', {
          appName: __appName,
          sql: sql,
          params: params || []
        }).then(function(resp) {
          if (gen !== genRef.current) return;
          if (resp.error) {
            setErr(resp.error);
          } else {
            setData(resp.data);
          }
          setLoading(false);
        });
      }, [sql, paramsKey, version]);

      return { data: data, loading: loading, error: err };
    }, []);

    return { exec: execFn, query: queryHook };
  };

  // Make hooks available via require('astonish') too — added to modules below
  // ── End data hooks ─────────────────────────────────────────────────

  function reportHeight() {
    var h = Math.max(document.body.scrollHeight, document.documentElement.scrollHeight);
    window.parent.postMessage({ type: 'render_success', height: h }, '*');
  }

  var modules = {
    'react': window.React,
    'react-dom': window.ReactDOM,
    'react-dom/client': window.ReactDOM,
    'recharts': window.Recharts || {},
    'lucide-react': window.LucideReact || {},
    'astonish': { useAppData: window.useAppData, useAppAction: window.useAppAction, useAppAI: window.useAppAI, useAppState: window.useAppState },
  };

  function sandboxRequire(name) {
    if (modules[name]) return modules[name];
    var baseName = name.split('/')[0];
    if (modules[baseName]) return modules[baseName];
    throw new Error('Module not found: ' + name + '. Available: react, recharts, lucide-react, astonish');
  }

  window.addEventListener('message', function(e) {
    var msg = e.data;
    if (!msg || !msg.type) return;

    if (msg.type === 'render') {
      errorEl.style.display = 'none';
      try {
        var userCode = msg.code;

        if (!/export\s+default\b/.test(userCode)) {
          var fnMatch = userCode.match(/(?:function|const|let|var)\s+([A-Z][A-Za-z0-9]*)/g);
          if (fnMatch && fnMatch.length > 0) {
            var last = fnMatch[fnMatch.length - 1];
            var name = last.replace(/^(?:function|const|let|var)\s+/, '');
            userCode += '\nexport default ' + name + ';\n';
          }
        }

        var compiled = Sucrase.transform(userCode, {
          transforms: ['jsx', 'typescript', 'imports'],
          jsxPragma: 'React.createElement',
          jsxFragmentPragma: 'React.Fragment',
          production: true,
        }).code;

        var moduleObj = { exports: {} };
        var fn = new Function(
          'React', 'require', 'module', 'exports',
          compiled
        );
        fn(window.React, sandboxRequire, moduleObj, moduleObj.exports);

        var Component = moduleObj.exports.default;
        if (!Component || typeof Component !== 'function') {
          Component = moduleObj.exports;
        }
        if (typeof Component !== 'function' && Component && typeof Component === 'object') {
          var keys = Object.keys(Component).filter(function(k) { return k !== '__esModule'; });
          for (var i = 0; i < keys.length; i++) {
            if (typeof Component[keys[i]] === 'function') {
              Component = Component[keys[i]];
              break;
            }
          }
        }

        if (typeof Component !== 'function') {
          showError('No valid React component found. Make sure to export default a function component.\n\nCompiled output (first 500 chars):\n' + compiled.substring(0, 500));
          return;
        }

        var root = document.getElementById('root');
        if (!rootInstance) {
          rootInstance = ReactDOM.createRoot(root);
        }
        rootInstance.render(React.createElement(Component));

        requestAnimationFrame(function() {
          setTimeout(reportHeight, 50);
        });

      } catch (err) {
        var msg = err.message || String(err);
        if (err instanceof ReferenceError) {
          var undefinedName = msg.match(/^(\w+) is not defined/);
          if (undefinedName) {
            msg += '\n\nHint: "' + undefinedName[1] + '" does not exist in the sandbox. ' +
              'Only native HTML elements (div, button, input, span, etc.) styled with Tailwind CSS are available. ' +
              'There is no component library (no shadcn/ui, no Material UI). ' +
              'Define any custom components as functions in the same file, or use HTML elements with Tailwind classes.';
          }
        }
        showError(msg, err.stack);
      }
    }

    if (msg.type === 'theme') {
      var isLight = msg.mode === 'light';
      document.body.className = isLight ? 'light' : 'dark';
      document.documentElement.style.background = isLight ? '#fafbfe' : '#0b1222';
      document.body.style.color = isLight ? '#1a1a1a' : '#e5e5e5';
    }

    if (msg.type === 'set_context') {
      if (msg.appName) __appName = msg.appName;
    }
  });

  var resizeObserver = new ResizeObserver(function() {
    reportHeight();
  });
  resizeObserver.observe(document.body);

  window.parent.postMessage({ type: 'sandbox_ready' }, '*');
})();
</script>
</body>
</html>`

// AppPreviewSandboxHandler serves the generative-UI sandbox HTML.
// The iframe uses sandbox="allow-scripts allow-same-origin" so it shares
// the parent origin, sends auth cookies, and can load sub-resources.
// This endpoint is behind auth — no bypass.
func AppPreviewSandboxHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(appPreviewSandboxHTML))
}

// sandboxRuntimeOnce caches the embedded sandbox-runtime.js file.
var (
	sandboxRuntimeOnce sync.Once
	sandboxRuntimeJS   []byte
)

// AppPreviewRuntimeHandler serves the pre-bundled sandbox runtime JS.
func AppPreviewRuntimeHandler(w http.ResponseWriter, r *http.Request) {
	sandboxRuntimeOnce.Do(func() {
		sandboxRuntimeJS = web.GetSandboxRuntime()
	})
	if sandboxRuntimeJS == nil {
		http.Error(w, "sandbox runtime not found — run 'npm run build' in web/", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(sandboxRuntimeJS)
}

// tailwindBrowserOnce caches the Tailwind CSS browser runtime.
var (
	tailwindBrowserOnce sync.Once
	tailwindBrowserJS   []byte
)

// AppPreviewTailwindHandler serves the Tailwind CSS v4 browser runtime.
func AppPreviewTailwindHandler(w http.ResponseWriter, r *http.Request) {
	tailwindBrowserOnce.Do(func() {
		tailwindBrowserJS = web.GetTailwindBrowser()
	})
	if tailwindBrowserJS == nil {
		http.Error(w, "tailwind browser runtime not found — run 'npm run build' in web/", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(tailwindBrowserJS)
}
