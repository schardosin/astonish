package browser

// webglConsistencyJS is injected via EvalOnNewDocument AFTER stealth.JS to fix
// the WebGL fingerprint consistency problem. stealth.JS already patches the
// vendor/renderer strings (getParameter params 37445/37446) to report
// "Intel Inc." / "Intel Iris OpenGL Engine", but it does NOT patch:
//
//   - getParameter capability values (MAX_VIEWPORT_DIMS, MAX_VARYING_VECTORS, etc.)
//   - getSupportedExtensions() (SwiftShader has far fewer extensions)
//   - getExtension() (must return stubs for "added" extensions)
//
// Without these patches, a fingerprinter sees "Intel Iris" identity but
// SwiftShader behavior, which is a strong spoofing signal (arguably worse
// than just letting SwiftShader identify itself).
//
// The values below match Intel Iris Plus Graphics 640/655 on macOS Chrome
// (via ANGLE). Sources: ANGLE source, Intel GPU hardware specs, Chromium
// WebGL conformance data.
const webglConsistencyJS = `(function() {
  'use strict';

  // Intel Iris capability values that differ from SwiftShader.
  // Only the parameters that actually differ are patched; matching values
  // are left to pass through to the real implementation.
  const PARAM_OVERRIDES = {
    // MAX_VIEWPORT_DIMS: SwiftShader=[8192,8192], Intel Iris=[16384,16384]
    0x0D3D: new Int32Array([16384, 16384]),
    // MAX_VARYING_VECTORS: SwiftShader=15, Intel Iris=31
    0x8DFC: 31,
    // MAX_COMBINED_TEXTURE_IMAGE_UNITS: SwiftShader=48, Intel Iris=80
    0x8B4D: 80,
    // ALIASED_POINT_SIZE_RANGE: SwiftShader=[1,1023], Intel Iris=[1,255.875]
    0x846D: new Float32Array([1, 255.875]),
  };

  // Extensions that Intel Iris supports but SwiftShader does not.
  // These must appear in getSupportedExtensions() and return non-null
  // from getExtension() for fingerprint consistency.
  const EXTRA_EXTENSIONS = [
    'EXT_disjoint_timer_query',
    'EXT_texture_compression_bptc',
    'EXT_texture_compression_rgtc',
    'EXT_texture_filter_anisotropic',
    'KHR_parallel_shader_compile',
    'OES_fbo_render_mipmap',
    'WEBGL_blend_equation_advanced_coherent',
    'WEBGL_compressed_texture_s3tc',
    'WEBGL_compressed_texture_s3tc_srgb',
    'WEBGL_polygon_mode',
  ];

  // Stub extension objects returned by getExtension() for the extra extensions.
  // Each extension type needs specific constants/methods to pass basic checks.
  const EXTENSION_STUBS = {
    'EXT_disjoint_timer_query': {
      QUERY_COUNTER_BITS_EXT: 0x8864,
      TIME_ELAPSED_EXT: 0x88BF,
      TIMESTAMP_EXT: 0x8E28,
      GPU_DISJOINT_EXT: 0x8FBB,
      QUERY_RESULT_EXT: 0x8866,
      QUERY_RESULT_AVAILABLE_EXT: 0x8867,
    },
    'EXT_texture_filter_anisotropic': {
      MAX_TEXTURE_MAX_ANISOTROPY_EXT: 0x84FF,
      TEXTURE_MAX_ANISOTROPY_EXT: 0x84FE,
    },
    'EXT_texture_compression_bptc': {
      COMPRESSED_RGBA_BPTC_UNORM_EXT: 0x8E8C,
      COMPRESSED_SRGB_ALPHA_BPTC_UNORM_EXT: 0x8E8D,
      COMPRESSED_RGB_BPTC_SIGNED_FLOAT_EXT: 0x8E8E,
      COMPRESSED_RGB_BPTC_UNSIGNED_FLOAT_EXT: 0x8E8F,
    },
    'EXT_texture_compression_rgtc': {
      COMPRESSED_RED_RGTC1_EXT: 0x8DBB,
      COMPRESSED_SIGNED_RED_RGTC1_EXT: 0x8DBC,
      COMPRESSED_RED_GREEN_RGTC2_EXT: 0x8DBD,
      COMPRESSED_SIGNED_RED_GREEN_RGTC2_EXT: 0x8DBE,
    },
    'WEBGL_compressed_texture_s3tc': {
      COMPRESSED_RGB_S3TC_DXT1_EXT: 0x83F0,
      COMPRESSED_RGBA_S3TC_DXT1_EXT: 0x83F1,
      COMPRESSED_RGBA_S3TC_DXT3_EXT: 0x83F2,
      COMPRESSED_RGBA_S3TC_DXT5_EXT: 0x83F3,
    },
    'WEBGL_compressed_texture_s3tc_srgb': {
      COMPRESSED_SRGB_S3TC_DXT1_EXT: 0x8C4C,
      COMPRESSED_SRGB_ALPHA_S3TC_DXT1_EXT: 0x8C4D,
      COMPRESSED_SRGB_ALPHA_S3TC_DXT3_EXT: 0x8C4E,
      COMPRESSED_SRGB_ALPHA_S3TC_DXT5_EXT: 0x8C4F,
    },
    'KHR_parallel_shader_compile': {
      COMPLETION_STATUS_KHR: 0x91B1,
    },
  };

  // Cache original prototypes to avoid re-patching and for Reflect.apply.
  const origGetParam1 = WebGLRenderingContext.prototype.getParameter;
  const origGetExts1 = WebGLRenderingContext.prototype.getSupportedExtensions;
  const origGetExt1 = WebGLRenderingContext.prototype.getExtension;

  // Patch getParameter to return Intel Iris capability values.
  function patchGetParameter(proto, origFn) {
    Object.defineProperty(proto, 'getParameter', {
      configurable: true,
      enumerable: true,
      writable: true,
      value: new Proxy(origFn, {
        apply: function(target, thisArg, args) {
          const param = args[0];
          if (param in PARAM_OVERRIDES) {
            return PARAM_OVERRIDES[param];
          }
          return Reflect.apply(target, thisArg, args);
        }
      })
    });
  }

  // Patch getSupportedExtensions to include the extra extensions.
  function patchGetSupportedExtensions(proto, origFn) {
    Object.defineProperty(proto, 'getSupportedExtensions', {
      configurable: true,
      enumerable: true,
      writable: true,
      value: new Proxy(origFn, {
        apply: function(target, thisArg, args) {
          const exts = Reflect.apply(target, thisArg, args);
          if (!exts) return exts;
          const extSet = new Set(exts);
          for (const ext of EXTRA_EXTENSIONS) {
            extSet.add(ext);
          }
          return Array.from(extSet).sort();
        }
      })
    });
  }

  // Patch getExtension to return stub objects for extra extensions.
  function patchGetExtension(proto, origFn) {
    Object.defineProperty(proto, 'getExtension', {
      configurable: true,
      enumerable: true,
      writable: true,
      value: new Proxy(origFn, {
        apply: function(target, thisArg, args) {
          const name = args[0];
          const result = Reflect.apply(target, thisArg, args);
          if (result) return result;
          // Return a stub for extensions that Intel Iris supports.
          if (EXTENSION_STUBS[name]) {
            return Object.create(null, Object.fromEntries(
              Object.entries(EXTENSION_STUBS[name]).map(([k, v]) => [k, {value: v, writable: false, enumerable: true, configurable: false}])
            ));
          }
          // Some extra extensions have no constants (just enable a capability).
          if (EXTRA_EXTENSIONS.includes(name)) {
            return Object.create(null);
          }
          return result;
        }
      })
    });
  }

  // Apply to WebGL1.
  patchGetParameter(WebGLRenderingContext.prototype, origGetParam1);
  patchGetSupportedExtensions(WebGLRenderingContext.prototype, origGetExts1);
  patchGetExtension(WebGLRenderingContext.prototype, origGetExt1);

  // Apply to WebGL2 if available.
  if (typeof WebGL2RenderingContext !== 'undefined') {
    const origGetParam2 = WebGL2RenderingContext.prototype.getParameter;
    const origGetExts2 = WebGL2RenderingContext.prototype.getSupportedExtensions;
    const origGetExt2 = WebGL2RenderingContext.prototype.getExtension;
    patchGetParameter(WebGL2RenderingContext.prototype, origGetParam2);
    patchGetSupportedExtensions(WebGL2RenderingContext.prototype, origGetExts2);
    patchGetExtension(WebGL2RenderingContext.prototype, origGetExt2);
  }
})();`
