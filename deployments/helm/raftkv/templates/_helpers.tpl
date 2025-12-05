{{/*
Expand the name of the chart.
*/}}
{{- define "raftkv.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "raftkv.fullname" -}}
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
{{- define "raftkv.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "raftkv.labels" -}}
helm.sh/chart: {{ include "raftkv.chart" . }}
{{ include "raftkv.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "raftkv.selectorLabels" -}}
app.kubernetes.io/name: {{ include "raftkv.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "raftkv.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "raftkv.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the headless service
*/}}
{{- define "raftkv.headlessServiceName" -}}
{{- printf "%s-headless" (include "raftkv.fullname" .) }}
{{- end }}

{{/*
Generate JWT secret
*/}}
{{- define "raftkv.jwtSecret" -}}
{{- if .Values.raftkv.auth.jwtSecret }}
{{- .Values.raftkv.auth.jwtSecret }}
{{- else }}
{{- randAlphaNum 32 | b64enc }}
{{- end }}
{{- end }}

{{/*
Generate admin password
*/}}
{{- define "raftkv.adminPassword" -}}
{{- .Values.raftkv.auth.adminPassword | default "admin" }}
{{- end }}

{{/*
RaftKV image
*/}}
{{- define "raftkv.image" -}}
{{- $registry := .Values.image.registry | default "" }}
{{- $repository := .Values.image.repository }}
{{- $tag := .Values.image.tag | default .Chart.AppVersion }}
{{- if $registry }}
{{- printf "%s/%s:%s" $registry $repository $tag }}
{{- else }}
{{- printf "%s:%s" $repository $tag }}
{{- end }}
{{- end }}

{{/*
Init container image
*/}}
{{- define "raftkv.initImage" -}}
{{- .Values.initContainers.image }}
{{- end }}

{{/*
Return the appropriate apiVersion for PodDisruptionBudget
*/}}
{{- define "raftkv.pdb.apiVersion" -}}
{{- if semverCompare ">=1.21-0" .Capabilities.KubeVersion.GitVersion }}
{{- print "policy/v1" }}
{{- else }}
{{- print "policy/v1beta1" }}
{{- end }}
{{- end }}

{{/*
Return the appropriate apiVersion for Ingress
*/}}
{{- define "raftkv.ingress.apiVersion" -}}
{{- if semverCompare ">=1.19-0" .Capabilities.KubeVersion.GitVersion }}
{{- print "networking.k8s.io/v1" }}
{{- else if semverCompare ">=1.14-0" .Capabilities.KubeVersion.GitVersion }}
{{- print "networking.k8s.io/v1beta1" }}
{{- else }}
{{- print "extensions/v1beta1" }}
{{- end }}
{{- end }}

{{/*
Return the storage class name
*/}}
{{- define "raftkv.storageClass" -}}
{{- if .Values.persistence.storageClass }}
{{- .Values.persistence.storageClass }}
{{- else }}
{{- "" }}
{{- end }}
{{- end }}

{{/*
Return true if TLS is enabled
*/}}
{{- define "raftkv.tls.enabled" -}}
{{- if .Values.raftkv.tls.enabled }}
{{- true }}
{{- else }}
{{- false }}
{{- end }}
{{- end }}

{{/*
Return the Raft address for a pod
*/}}
{{- define "raftkv.raftAddr" -}}
{{- printf "$(POD_NAME).%s.$(POD_NAMESPACE).svc.cluster.local:%d" (include "raftkv.headlessServiceName" .) (int .Values.service.headless.raftPort) }}
{{- end }}

{{/*
Return the join address for non-bootstrap nodes
*/}}
{{- define "raftkv.joinAddr" -}}
{{- printf "http://%s-0.%s.$(POD_NAMESPACE).svc.cluster.local:%d" (include "raftkv.fullname" .) (include "raftkv.headlessServiceName" .) (int .Values.service.httpPort) }}
{{- end }}
