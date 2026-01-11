{{/*
Expand the name of the chart.
*/}}
{{- define "etcd-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Generate the full image tag from registry, repository, name, and tag components.
Handles empty registry and repository gracefully.
*/}}
{{- define "etcd-operator.image" -}}
    {{- $registry := .Values.image.registry -}}
    {{- $repository := .Values.image.repository -}}
    {{- $name := .Values.image.name -}}
    {{- $tag := .Values.image.tag -}}

    {{/* Validate required fields */}}
    {{- if not $name -}}
        {{- fail "image.name is required" -}}
    {{- end -}}
    {{- if not $tag -}}
        {{- fail "image.tag is required" -}}
    {{- end -}}

    {{/* Build the image path */}}
    {{- $imagePath := printf "%s:%s" $name $tag -}}
    {{- if $repository -}}
    {{-   $imagePath = printf "%s/%s" $repository $imagePath -}}
    {{- end -}}
    {{- if $registry -}}
    {{-   $imagePath = printf "%s/%s" $registry $imagePath -}}
    {{- end -}}

    {{- $imagePath -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "etcd-operator.fullname" -}}
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
{{- define "etcd-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "etcd-operator.labels" -}}
helm.sh/chart: {{ include "etcd-operator.chart" . }}
{{ include "etcd-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "etcd-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "etcd-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "etcd-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "etcd-operator.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
