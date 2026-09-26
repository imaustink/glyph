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
Collab (realtime collaboration) labels
*/}}
{{- define "glyph.collab.labels" -}}
{{ include "glyph.labels" . }}
app.kubernetes.io/component: collab
{{- end }}

{{- define "glyph.collab.selectorLabels" -}}
{{ include "glyph.selectorLabels" . }}
app.kubernetes.io/component: collab
{{- end }}

{{/*
Secret holding the token the collab service uses on the API's /internal routes.
*/}}
{{- define "glyph.collab.secretName" -}}
{{- .Values.collab.existingSecret | default (printf "%s-collab" (include "glyph.fullname" .)) }}
{{- end }}

{{/*
CNPG cluster name. Defaults to <fullname>-db, which is what every existing
install already has; set cnpg.clusterName to pin it so a change to the release
name or fullnameOverride can't point the chart at a new, empty database.
*/}}
{{- define "glyph.cnpg.clusterName" -}}
{{- .Values.cnpg.clusterName | default (printf "%s-db" (include "glyph.fullname" .)) }}
{{- end }}

{{/*
ScheduledBackup schedule. CNPG takes a six-field cron expression with seconds
first (robfig/cron); a crontab-style five-field one is given "0" seconds.
*/}}
{{- define "glyph.cnpg.backupSchedule" -}}
{{- $s := regexReplaceAll "\\s+" (trim .Values.cnpg.backup.schedule) " " }}
{{- $n := len (splitList " " $s) }}
{{- if eq $n 5 }}{{ printf "0 %s" $s }}
{{- else if eq $n 6 }}{{ $s }}
{{- else }}
{{- fail (printf "cnpg.backup.schedule %q must be a six-field cron expression (seconds first), e.g. \"0 0 0 * * *\"" $s) }}
{{- end }}
{{- end }}

{{/*
Validates the backup settings. Included by the Cluster template.
*/}}
{{- define "glyph.cnpg.validateBackup" -}}
{{- $b := .Values.cnpg.backup }}
{{- if and $b.required (not $b.enabled) }}
{{- fail "cnpg.backup.required is true but cnpg.backup.enabled is false: this deployment must not run without backups. Configure cnpg.backup (destinationPath and s3.secretName) and set cnpg.backup.enabled=true." }}
{{- end }}
{{- if $b.enabled }}
{{- if not $b.destinationPath }}
{{- fail "cnpg.backup.enabled is true but cnpg.backup.destinationPath is empty (expected s3://<bucket>/<prefix>)." }}
{{- end }}
{{- if not (or $b.s3.secretName $b.s3.inheritFromIAMRole) }}
{{- fail "cnpg.backup.enabled is true but cnpg.backup.s3.secretName is empty (and s3.inheritFromIAMRole is false): WAL archiving would fail and WAL would fill the volume." }}
{{- end }}
{{- end }}
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
