{{- define "respond.name" -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{- define "respond.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "respond.name" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}
