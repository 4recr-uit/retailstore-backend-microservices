{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "orders.name" -}}
{{- default "orders" .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "orders.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "orders" .Values.nameOverride }}
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
{{- define "orders.chart" -}}
{{- printf "%s-%s" "orders" .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "orders.labels" -}}
helm.sh/chart: {{ include "orders.chart" . }}
{{ include "orders.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "orders.selectorLabels" -}}
app.kubernetes.io/name: {{ include "orders.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: service
app.kubernetes.io/owner: retail-store-sample
{{- end }}

{{/*
API component labels
*/}}
{{- define "orders.api.labels" -}}
helm.sh/chart: {{ include "orders.chart" . }}
{{ include "orders.api.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "orders.api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "orders.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: api
app.kubernetes.io/owner: retail-store-sample
{{- end }}

{{/*
Worker component labels
*/}}
{{- define "orders.worker.labels" -}}
helm.sh/chart: {{ include "orders.chart" . }}
{{ include "orders.worker.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "orders.worker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "orders.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: worker
app.kubernetes.io/owner: retail-store-sample
{{- end }}

{{/* Separate identities for API producers and SQS workers. */}}
{{- define "orders.apiServiceAccountName" -}}
{{- required "apiServiceAccount.name is required" .Values.apiServiceAccount.name -}}
{{- end }}

{{- define "orders.workerServiceAccountName" -}}
{{- required "workerServiceAccount.name is required" .Values.workerServiceAccount.name -}}
{{- end }}

{{/*
Create the name of the config map to use
*/}}
{{- define "orders.configMapName" -}}
{{- if .Values.configMap.create }}
{{- default (include "orders.fullname" .) .Values.configMap.name }}
{{- else }}
{{- default "default" .Values.configMap.name }}
{{- end }}
{{- end }}

{{/*
Create the secret name
*/}}
{{- define "orders.secretName" -}}
{{- default (printf "%s-db" (include "orders.fullname" .)) .Values.database.secret.name }}
{{- end }}