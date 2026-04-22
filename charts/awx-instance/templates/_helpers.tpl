{{/*
Expand the name of the release.
*/}}
{{- define "awx.fullname" -}}
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
Base domain derived from awx.host
*/}}
{{- define "awx.baseDomain" -}}
{{- $host := .Values.awx.host -}}
{{- $baseDomain := trimPrefix "awx." $host -}}
{{- $baseDomain -}}
{{- end -}}

{{/*
Dex domain
*/}}
{{- define "awx.dexDomain" -}}
{{- $baseDomain := include "awx.baseDomain" . -}}
https://dex.{{ $baseDomain }}
{{- end -}}

{{/*
OIDC Client ID
*/}}
{{- define "awx.oidcClientId" -}}
dex-client-{{ .Release.Name }}@{{ .Release.Namespace }}
{{- end -}}

{{/*
TLS secret name
*/}}
{{- define "awx.tlsSecret" -}}
{{ .Release.Name }}-secret-tls
{{- end -}}

{{/*
Task affinity - auto if RWO storage
*/}}
{{- define "awx.taskAffinity" -}}
{{- if eq .Values.awx.storage.accessMode "ReadWriteOnce" -}}
auto
{{- else -}}
{}
{{- end -}}
{{- end -}}
