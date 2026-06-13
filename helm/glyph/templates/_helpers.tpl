{{/*
Expand the name of the chart.
*/}}
{{- define "glyph.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "glyph.fullname" -}}
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
{{- define "glyph.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "glyph.labels" -}}
helm.sh/chart: {{ include "glyph.chart" . }}
{{ include "glyph.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "glyph.selectorLabels" -}}
app.kubernetes.io/name: {{ include "glyph.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Frontend labels
*/}}
{{- define "glyph.frontend.labels" -}}
{{ include "glyph.labels" . }}
app.kubernetes.io/component: frontend
{{- end }}

{{- define "glyph.frontend.selectorLabels" -}}
{{ include "glyph.selectorLabels" . }}
app.kubernetes.io/component: frontend
{{- end }}

{{/*
API labels
*/}}
{{- define "glyph.api.labels" -}}
{{ include "glyph.labels" . }}
app.kubernetes.io/component: api
{{- end }}

{{- define "glyph.api.selectorLabels" -}}
{{ include "glyph.selectorLabels" . }}
app.kubernetes.io/component: api
{{- end }}

{{/*
CNPG cluster name
*/}}
{{- define "glyph.cnpg.clusterName" -}}
{{- printf "%s-db" (include "glyph.fullname" .) }}
{{- end }}

{{/*
Database URL constructed from CNPG secret.
The CNPG operator creates a secret named <cluster>-app with keys: host, port, dbname, user, password.
*/}}
{{- define "glyph.databaseSecretName" -}}
{{- printf "%s-app" (include "glyph.cnpg.clusterName" .) }}
{{- end }}

{{/*
Migration job name — includes a hash of the migration config to re-run on changes.
*/}}
{{- define "glyph.migrate.name" -}}
{{- printf "%s-migrate" (include "glyph.fullname" .) }}
{{- end }}
