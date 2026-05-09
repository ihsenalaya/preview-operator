{{- define "preview-operator.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "preview-operator.fullname" -}}
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

{{- define "preview-operator.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "preview-operator.labels" -}}
helm.sh/chart: {{ include "preview-operator.chart" . }}
{{ include "preview-operator.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "preview-operator.selectorLabels" -}}
app.kubernetes.io/name: {{ include "preview-operator.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{- define "preview-operator.serviceAccountName" -}}
{{- if .Values.serviceAccount.name }}
{{- .Values.serviceAccount.name }}
{{- else }}
{{- include "preview-operator.fullname" . }}
{{- end }}
{{- end }}

{{- define "preview-operator.webhookCertSecret" -}}
{{- printf "%s-webhook-cert" (include "preview-operator.fullname" .) }}
{{- end }}

{{- define "preview-operator.webhookServiceName" -}}
{{- printf "%s-webhook" (include "preview-operator.fullname" .) }}
{{- end }}

{{- define "preview-operator.issuerName" -}}
{{- if .Values.certManager.issuerName }}
{{- .Values.certManager.issuerName }}
{{- else }}
{{- printf "%s-selfsigned" (include "preview-operator.fullname" .) }}
{{- end }}
{{- end }}
