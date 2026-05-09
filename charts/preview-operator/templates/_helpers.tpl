{{- define "cellenza-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cellenza-operator.fullname" -}}
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

{{- define "cellenza-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "cellenza-operator.labels" -}}
helm.sh/chart: {{ include "cellenza-operator.chart" . }}
{{ include "cellenza-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "cellenza-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cellenza-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{- define "cellenza-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- include "cellenza-operator.fullname" . }}
{{- end }}
{{- end }}

{{- define "cellenza-operator.webhookCertSecret" -}}
{{- printf "%s-webhook-cert" (include "cellenza-operator.fullname" .) }}
{{- end }}

{{- define "cellenza-operator.webhookServiceName" -}}
{{- printf "%s-webhook" (include "cellenza-operator.fullname" .) }}
{{- end }}

{{- define "cellenza-operator.issuerName" -}}
{{- if .Values.certManager.issuerName }}
{{- .Values.certManager.issuerName }}
{{- else }}
{{- printf "%s-selfsigned" (include "cellenza-operator.fullname" .) }}
{{- end }}
{{- end }}
