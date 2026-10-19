{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "recommendations.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "recommendations.fullname" -}}
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
{{- define "recommendations.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "recommendations.labels" -}}
helm.sh/chart: {{ include "recommendations.chart" . }}
{{ include "recommendations.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "recommendations.selectorLabels" -}}
app.kubernetes.io/name: {{ include "recommendations.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: service
app.kubernetes.io/owner: retail-store-sample
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "recommendations.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "recommendations.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the config map to use
*/}}
{{- define "recommendations.configMapName" -}}
{{- if .Values.configMap.create }}
{{- default (include "recommendations.fullname" .) .Values.configMap.name }}
{{- else }}
{{- default "default" .Values.configMap.name }}
{{- end }}
{{- end }}

{{/*
Create secret name
*/}}
{{- define "recommendations.secretName" -}}
{{- default (printf "%s-db" (include "recommendations.fullname" .)) .Values.app.persistence.secret.name }}
{{- end }}
