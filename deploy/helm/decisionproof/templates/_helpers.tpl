{{- define "decisionproof.name" -}}decisionproof{{- end -}}
{{- define "decisionproof.fullname" -}}{{ .Release.Name }}-decisionproof{{- end -}}
{{- define "decisionproof.labels" -}}
app.kubernetes.io/name: {{ include "decisionproof.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}
