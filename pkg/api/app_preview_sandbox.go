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
<style type="text/tailwindcss"></style>
<style>
  * { box-sizing: border-box; }
  body { margin: 0; padding: 16px; font-family: system-ui, -apple-system, sans-serif; background: #0b1222 !important; color: #e5e5e5; }
  #root { min-height: 20px; }
  #error-display {
    padding: 12px 16px; margin: 8px; border-radius: 8px;
    background: rgba(239, 68, 68, 0.1); border: 1px solid rgba(239, 68, 68, 0.3);
    color: #ef4444; font-size: 13px; font-family: monospace; white-space: pre-wrap;
    display: none;
  }
</style>
</head>
<body class="dark" style="background: #0b1222;">
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
  };

  function sandboxRequire(name) {
    if (modules[name]) return modules[name];
    var baseName = name.split('/')[0];
    if (modules[baseName]) return modules[baseName];
    throw new Error('Module not found: ' + name + '. Available: react, recharts, lucide-react');
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
      document.body.style.background = isLight ? '#fafbfe' : '#0b1222';
      document.body.style.color = isLight ? '#1a1a1a' : '#e5e5e5';
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
