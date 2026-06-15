{{/*
Expand the name of the chart.
*/}}
{{- define "kubefisher.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this
(by the DNS naming spec).
*/}}
{{- define "kubefisher.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kubefisher.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "kubefisher.labels" -}}
helm.sh/chart: {{ include "kubefisher.chart" . }}
{{ include "kubefisher.selectorLabels" . }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: kubefisher
{{- with .Values.commonLabels }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/*
Selector labels (must be stable across upgrades; do NOT include version here).
*/}}
{{- define "kubefisher.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubefisher.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Component labels for the cost-patcher workload.
*/}}
{{- define "kubefisher.costPatcher.labels" -}}
{{ include "kubefisher.labels" . }}
app.kubernetes.io/component: cost-patcher
{{- end -}}

{{- define "kubefisher.costPatcher.selectorLabels" -}}
{{ include "kubefisher.selectorLabels" . }}
app.kubernetes.io/component: cost-patcher
{{- end -}}

{{/*
ServiceAccount name to use.
*/}}
{{- define "kubefisher.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "kubefisher.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Image reference (repository:tag). Tag falls back to .Chart.AppVersion when
not set in values.
*/}}
{{- define "kubefisher.image" -}}
{{- $tag := default .Chart.AppVersion .Values.image.tag -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end -}}

{{/*
Common annotations (merged into every resource).
*/}}
{{- define "kubefisher.annotations" -}}
{{- with .Values.commonAnnotations }}
{{ toYaml . }}
{{- end }}
{{- end -}}

{{/*
Resolve namespace for cross-namespace observability resources.
Usage: include "kubefisher.namespaceOrDefault" (dict "ns" .Values.x.namespace "ctx" .)
*/}}
{{- define "kubefisher.namespaceOrDefault" -}}
{{- if .ns -}}
{{- .ns -}}
{{- else -}}
{{- .ctx.Release.Namespace -}}
{{- end -}}
{{- end -}}

{{/*
Operator component names and labels.
*/}}
{{- define "kubefisher.operator.fullname" -}}
{{- printf "%s-operator" (include "kubefisher.fullname" .) -}}
{{- end -}}

{{- define "kubefisher.operator.labels" -}}
{{ include "kubefisher.labels" . }}
app.kubernetes.io/component: quota-operator
{{- end -}}

{{- define "kubefisher.operator.selectorLabels" -}}
{{ include "kubefisher.selectorLabels" . }}
app.kubernetes.io/component: quota-operator
{{- end -}}

{{- define "kubefisher.operator.serviceAccountName" -}}
{{- if .Values.operator.serviceAccount.create -}}
{{- default (include "kubefisher.operator.fullname" .) .Values.operator.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.operator.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "kubefisher.operator.image" -}}
{{- $tag := default .Chart.AppVersion .Values.operator.image.tag -}}
{{- printf "%s:%s" .Values.operator.image.repository $tag -}}
{{- end -}}

{{- define "kubefisher.operator.prometheusUrl" -}}
{{- if .Values.operator.prometheusUrl -}}
{{- .Values.operator.prometheusUrl -}}
{{- else -}}
{{- .Values.config.prometheusUrl -}}
{{- end -}}
{{- end -}}

{{- define "kubefisher.operator.webhookServiceName" -}}
{{- printf "%s-webhook-svc" (include "kubefisher.operator.fullname" .) -}}
{{- end -}}

{{- define "kubefisher.operator.webhookSecretName" -}}
{{- default (printf "%s-webhook-tls" (include "kubefisher.operator.fullname" .)) .Values.operator.webhook.certManager.secretName -}}
{{- end -}}

{{- define "kubefisher.operator.webhookCertificateName" -}}
{{- default (include "kubefisher.operator.webhookSecretName" .) .Values.operator.webhook.certManager.certificateName -}}
{{- end -}}

{{- define "kubefisher.operator.webhookIssuerName" -}}
{{- default (printf "%s-selfsigned" (include "kubefisher.operator.fullname" .)) .Values.operator.webhook.certManager.issuerName -}}
{{- end -}}

{{- define "kubefisher.operator.webhookCAInjectFrom" -}}
{{- printf "%s/%s" .Release.Namespace (include "kubefisher.operator.webhookCertificateName" .) -}}
{{- end -}}
