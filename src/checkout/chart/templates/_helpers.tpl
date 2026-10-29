{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "checkout.name" -}}
{{- default "checkout" .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "checkout.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default "checkout" .Values.nameOverride }}
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
{{- define "checkout.chart" -}}
{{- printf "%s-%s" "checkout" .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "checkout.labels" -}}
helm.sh/chart: {{ include "checkout.chart" . }}
{{ include "checkout.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "checkout.selectorLabels" -}}
app.kubernetes.io/name: {{ include "checkout.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: service
app.kubernetes.io/owner: retail-store-sample
{{- end }}

{{/*
API component labels
*/}}
{{- define "checkout.api.labels" -}}
helm.sh/chart: {{ include "checkout.chart" . }}
{{ include "checkout.api.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "checkout.api.selectorLabels" -}}
app.kubernetes.io/name: {{ include "checkout.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: api
app.kubernetes.io/owner: retail-store-sample
{{- end }}

{{/*
Worker component labels
*/}}
{{- define "checkout.worker.labels" -}}
helm.sh/chart: {{ include "checkout.chart" . }}
{{ include "checkout.worker.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "checkout.worker.selectorLabels" -}}
app.kubernetes.io/name: {{ include "checkout.fullname" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: worker
app.kubernetes.io/owner: retail-store-sample
{{- end }}

{{/* Separate identities for API producers and SQS workers. */}}
{{- define "checkout.apiServiceAccountName" -}}
{{- required "apiServiceAccount.name is required" .Values.apiServiceAccount.name -}}
{{- end }}

{{- define "checkout.workerServiceAccountName" -}}
{{- required "workerServiceAccount.name is required" .Values.workerServiceAccount.name -}}
{{- end }}

{{/*
Create the name of the config map to use
*/}}
{{- define "checkout.configMapName" -}}
{{- if .Values.configMap.create }}
{{- default (include "checkout.fullname" .) .Values.configMap.name }}
{{- else }}
{{- default "default" .Values.configMap.name }}
{{- end }}
{{- end }}

{{/*
Create the secret name
*/}}
{{- define "checkout.secretName" -}}
{{- default (printf "%s-db" (include "checkout.fullname" .)) .Values.database.secret.name }}
{{- end }}