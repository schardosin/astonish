{{/*
_validation.tpl — fail-hard invariants checked at template time.

Run once from a top-level template (NOTES.txt includes it) so misconfigured
values are caught before any resource is rendered.

Checks:
  1. All computed namespace names conform to DNS-1123 label rules.
  2. Release.Namespace matches the computed control-plane namespace.
     (Prevents silent split-brain where the chart thinks the control plane
     lives in ns X but `helm -n` put it in ns Y.)
  3. When sandbox.enabled=true, sandbox.storage.storageClassName is set.
  4. sandbox.podSecurity is one of baseline|privileged|restricted.
  5. sandbox.overlay.mode is one of fuse|kernel|auto.
*/}}

{{- define "astonish.validate" -}}
{{- $dns1123 := "^[a-z0-9]([-a-z0-9]*[a-z0-9])?$" -}}

{{- /* 1a. Control-plane namespace. */ -}}
{{- $cp := include "astonish.namespace.controlPlane" . -}}
{{- if not (regexMatch $dns1123 $cp) -}}
{{- fail (printf "namespaces.controlPlane %q is not a valid DNS-1123 label (must match %s)" $cp $dns1123) -}}
{{- end -}}

{{- /* 1b. Sandbox namespace (only if enabled). */ -}}
{{- if .Values.sandbox.enabled -}}
{{- $sb := include "astonish.namespace.sandbox" . -}}
{{- if not (regexMatch $dns1123 $sb) -}}
{{- fail (printf "namespaces.sandbox %q is not a valid DNS-1123 label (must match %s)" $sb $dns1123) -}}
{{- end -}}
{{- if eq $sb $cp -}}
{{- fail (printf "namespaces.sandbox (%s) must differ from namespaces.controlPlane (%s)" $sb $cp) -}}
{{- end -}}
{{- end -}}

{{- /* 2. Release.Namespace must match computed control-plane namespace. */ -}}
{{- if ne .Release.Namespace $cp -}}
{{- fail (printf "Release.Namespace %q does not match computed control-plane namespace %q. Re-run with `helm upgrade --install ... -n %s --create-namespace` or set namespaces.controlPlane / namespaces.prefix so the two agree." .Release.Namespace $cp $cp) -}}
{{- end -}}

{{- /* 3. Sandbox storage class must be set. */ -}}
{{- if .Values.sandbox.enabled -}}
{{- if not .Values.sandbox.storage.storageClassName -}}
{{- fail "sandbox.storage.storageClassName is required. Set it to an RWX StorageClass (e.g. nfs-client, cephfs, efs-sc, azurefile-csi, manila-csi)." -}}
{{- end -}}
{{- end -}}

{{- /* 4. PodSecurity profile. */ -}}
{{- if .Values.sandbox.enabled -}}
{{- $ps := .Values.sandbox.podSecurity -}}
{{- if not (has $ps (list "baseline" "privileged" "restricted")) -}}
{{- fail (printf "sandbox.podSecurity must be one of baseline|privileged|restricted (got %q)" $ps) -}}
{{- end -}}
{{- end -}}

{{- /* 5. Overlay mode. */ -}}
{{- if .Values.sandbox.enabled -}}
{{- $om := .Values.sandbox.overlay.mode -}}
{{- if not (has $om (list "fuse" "kernel" "auto")) -}}
{{- fail (printf "sandbox.overlay.mode must be one of fuse|kernel|auto (got %q)" $om) -}}
{{- end -}}
{{- end -}}

{{- end -}}
