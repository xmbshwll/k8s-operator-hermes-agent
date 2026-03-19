{{/*
Expand the name of the chart.
*/}}
{{- define "k8s-operator-hermes-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "k8s-operator-hermes-agent.fullname" -}}
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
Namespace for generated references.
Always uses the Helm release namespace.
*/}}
{{- define "k8s-operator-hermes-agent.namespaceName" -}}
{{- .Release.Namespace }}
{{- end }}

{{/*
Resource name with proper truncation for Kubernetes 63-character limit.
Takes a dict with:
  - .suffix: Resource name suffix (e.g., "metrics", "webhook")
  - .context: Template context (root context with .Values, .Release, etc.)
Dynamically calculates safe truncation to ensure total name length <= 63 chars.
*/}}
{{- define "k8s-operator-hermes-agent.resourceName" -}}
{{- $fullname := include "k8s-operator-hermes-agent.fullname" .context }}
{{- $suffix := .suffix }}
{{- $maxLen := sub 62 (len $suffix) | int }}
{{- if gt (len $fullname) $maxLen }}
{{- printf "%s-%s" (trunc $maxLen $fullname | trimSuffix "-") $suffix | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" $fullname $suffix | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Service account name for the controller manager.
*/}}
{{- define "k8s-operator-hermes-agent.serviceAccountName" -}}
{{- $defaultName := include "k8s-operator-hermes-agent.resourceName" (dict "suffix" "controller-manager" "context" .) }}
{{- if .Values.serviceAccount.create }}
{{- default $defaultName .Values.serviceAccount.name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- required "serviceAccount.name must be set when serviceAccount.create=false" .Values.serviceAccount.name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}

{{/*
Release-scoped match labels for controller-manager resources.
*/}}
{{- define "k8s-operator-hermes-agent.controllerManagerMatchLabels" -}}
app.kubernetes.io/name: {{ include "k8s-operator-hermes-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
control-plane: controller-manager
{{- end }}

{{/*
Shared labels for controller-manager resources.
*/}}
{{- define "k8s-operator-hermes-agent.controllerManagerLabels" -}}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
{{ include "k8s-operator-hermes-agent.controllerManagerMatchLabels" . }}
{{- end }}

{{/*
Label selector for controller-manager pods.
*/}}
{{- define "k8s-operator-hermes-agent.controllerManagerSelectorLabels" -}}
{{ include "k8s-operator-hermes-agent.controllerManagerMatchLabels" . }}
{{- end }}

{{/*
Whether the chart-managed webhook TLS path is enabled.
*/}}
{{- define "k8s-operator-hermes-agent.webhookTLSManaged" -}}
{{- if and .Values.webhook.enabled .Values.certManager.enabled -}}true{{- end -}}
{{- end }}

{{/*
Whether the chart-managed metrics TLS path is enabled.
*/}}
{{- define "k8s-operator-hermes-agent.metricsTLSManaged" -}}
{{- if and .Values.metrics.enabled .Values.certManager.enabled .Values.metrics.certManager.enabled -}}true{{- end -}}
{{- end }}

{{/*
Shared cert-manager issuer name.
*/}}
{{- define "k8s-operator-hermes-agent.selfSignedIssuerName" -}}
{{ include "k8s-operator-hermes-agent.resourceName" (dict "suffix" "selfsigned-issuer" "context" .) }}
{{- end }}

{{/*
Webhook serving certificate name.
*/}}
{{- define "k8s-operator-hermes-agent.webhookServingCertificateName" -}}
{{ include "k8s-operator-hermes-agent.resourceName" (dict "suffix" "serving-cert" "context" .) }}
{{- end }}

{{/*
Metrics serving certificate name.
*/}}
{{- define "k8s-operator-hermes-agent.metricsServingCertificateName" -}}
{{ include "k8s-operator-hermes-agent.resourceName" (dict "suffix" "metrics-certs" "context" .) }}
{{- end }}

{{/*
Metrics serving certificate secret name.
*/}}
{{- define "k8s-operator-hermes-agent.metricsServingCertificateSecretName" -}}
{{ include "k8s-operator-hermes-agent.resourceName" (dict "suffix" "metrics-server-cert" "context" .) }}
{{- end }}

{{/*
Internal DNS name for the metrics Service.
*/}}
{{- define "k8s-operator-hermes-agent.metricsServiceServerName" -}}
{{ printf "%s.%s.svc" (include "k8s-operator-hermes-agent.resourceName" (dict "suffix" "controller-manager-metrics-service" "context" .)) .Release.Namespace }}
{{- end }}
