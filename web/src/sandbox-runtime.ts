/**
 * Sandbox Runtime - Pre-bundled dependencies for the generative UI iframe.
 *
 * This file is built as a standalone IIFE bundle (via vite.config.sandbox.js)
 * that exposes React, ReactDOM, Sucrase, Recharts, and Lucide React icons as
 * window globals. The sandbox iframe loads this single file from the same
 * origin, avoiding CDN dependencies and CSP issues.
 */

import React from 'react'
import ReactDOM from 'react-dom/client'
import { transform } from 'sucrase'
import * as Recharts from 'recharts'
import * as LucideReact from 'lucide-react'

// Expose as globals for the sandbox runtime script
;(window as any).React = React
;(window as any).ReactDOM = ReactDOM
;(window as any).Sucrase = { transform }
;(window as any).Recharts = Recharts
;(window as any).LucideReact = LucideReact
