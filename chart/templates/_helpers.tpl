{{/* templates/_helpers.tpl */}}

{{/*
Expand the name of the chart.
*/}}
{{- define "hybrid-gpu-scheduler.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "hybrid-gpu-scheduler.fullname" -}}
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
{{- define "hybrid-gpu-scheduler.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "hybrid-gpu-scheduler.labels" -}}
helm.sh/chart: {{ include "hybrid-gpu-scheduler.chart" . }}
{{ include "hybrid-gpu-scheduler.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "hybrid-gpu-scheduler.selectorLabels" -}}
app.kubernetes.io/name: {{ include "hybrid-gpu-scheduler.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use.
*/}}
{{- define "hybrid-gpu-scheduler.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "hybrid-gpu-scheduler.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Image tag.
*/}}
{{- define "hybrid-gpu-scheduler.tag" -}}
{{- .Values.image.tag | default .Chart.AppVersion }}
{{- end }}

{{/*
GPU tolerations — allow running on GPU nodes.
*/}}
{{- define "hybrid-gpu-scheduler.gpuTolerations" -}}
tolerations:
  - key: "nvidia.com/gpu"
    operator: "Exists"
    effect: "NoSchedule"
  - key: "amd.com/gpu"
    operator: "Exists"
    effect: "NoSchedule"
{{- end }}
