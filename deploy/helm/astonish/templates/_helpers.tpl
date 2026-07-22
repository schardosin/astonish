{{/*
Expand the name of the chart.
*/}}
{{- define "astonish.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "astonish.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "astonish.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "astonish.labels" -}}
helm.sh/chart: {{ include "astonish.chart" . }}
{{ include "astonish.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: astonish
{{- end }}

{{/*
Selector labels (shared across API and Worker)
*/}}
{{- define "astonish.selectorLabels" -}}
app.kubernetes.io/name: {{ include "astonish.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
API selector labels
*/}}
{{- define "astonish.apiSelectorLabels" -}}
{{ include "astonish.selectorLabels" . }}
app.kubernetes.io/component: api
{{- end }}

{{/*
Worker selector labels
*/}}
{{- define "astonish.workerSelectorLabels" -}}
{{ include "astonish.selectorLabels" . }}
app.kubernetes.io/component: worker
{{- end }}

{{/*
Namespace helpers — compute control-plane and sandbox namespace names from
.Values.namespaces.prefix. Either name may be overridden explicitly.
*/}}
{{- define "astonish.namespace.prefix" -}}
{{- default "astonish" .Values.namespaces.prefix -}}
{{- end }}

{{- define "astonish.namespace.controlPlane" -}}
{{- $p := include "astonish.namespace.prefix" . -}}
{{- default $p .Values.namespaces.controlPlane -}}
{{- end }}

{{- define "astonish.namespace.sandbox" -}}
{{- $p := include "astonish.namespace.prefix" . -}}
{{- default (printf "%s-sandbox" $p) .Values.namespaces.sandbox -}}
{{- end }}

{{/*
ServiceAccount name used by the control-plane pods. Defaults to
"{fullname}-daemon" when serviceAccount.name is unset.
*/}}
{{- define "astonish.serviceAccountName" -}}
{{- if .Values.serviceAccount.name -}}
{{- .Values.serviceAccount.name -}}
{{- else -}}
{{- printf "%s-daemon" (include "astonish.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
Sandbox RBAC resource names.
*/}}
{{- define "astonish.sandbox.roleName" -}}
{{- printf "%s-sandbox-operator" (include "astonish.fullname" .) -}}
{{- end }}

{{/*
Image reference for the sandbox base image, as a single
"repository:tag" string — used in the seed Job and written into
config.sandbox.kubernetes.sandbox_image.
*/}}
{{- define "astonish.sandbox.image" -}}
{{- printf "%s:%s" .Values.sandbox.image.repository .Values.sandbox.image.tag -}}
{{- end }}

{{/*
Secret name — use an existing external secret if configured, otherwise
the chart-managed secret ("{fullname}-secrets").
*/}}
{{- define "astonish.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- printf "%s-secrets" (include "astonish.fullname" .) -}}
{{- end -}}
{{- end }}

{{/*
OpenShell gateway gRPC address. Uses explicit override if set, otherwise
auto-derives from the subchart Service name within the release namespace.
The subchart uses nameOverride "openshell", so the Service is named
"{release}-openshell" and listens on port 8080.
*/}}
{{- define "astonish.openshell.gatewayAddr" -}}
{{- if .Values.sandbox.openshell.gateway.addr -}}
{{- .Values.sandbox.openshell.gateway.addr -}}
{{- else -}}
{{- printf "%s-openshell.%s.svc.cluster.local:8080" .Release.Name .Release.Namespace -}}
{{- end -}}
{{- end }}

{{/*
Cert-bundle source: "pvc" (OpenShell-native) or "configMap" (Kyverno inject).
Legacy: claimName set without configMapName → pvc; otherwise configMap.
*/}}
{{- define "astonish.certBundle.source" -}}
{{- $b := . -}}
{{- if $b.source -}}
{{- $b.source -}}
{{- else if and $b.claimName (not $b.configMapName) -}}
pvc
{{- else -}}
configMap
{{- end -}}
{{- end }}

{{/*
ConfigMap name for a cert bundle (configMap source). Falls back to claimName
or astonish-cert-<name>.
*/}}
{{- define "astonish.certBundle.configMapName" -}}
{{- $b := . -}}
{{- if $b.configMapName -}}
{{- $b.configMapName -}}
{{- else if $b.claimName -}}
{{- $b.claimName -}}
{{- else -}}
{{- printf "astonish-cert-%s" $b.name -}}
{{- end -}}
{{- end }}
