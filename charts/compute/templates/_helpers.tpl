{{/*
Create the container images
*/}}
{{- define "unikorn.defaultTag" -}}
v{{ .Chart.Version }}
{{- end }}

{{- define "unikorn.computeInstanceControllerImage" -}}
{{- .Values.instanceController.image | default (printf "%s/unikorn-compute-instance-controller:%s" (include "unikorn.defaultRepositoryPath" .) (.Values.tag | default (include "unikorn.defaultTag" .))) }}
{{- end }}

{{- define "unikorn.computeClusterControllerImage" -}}
{{- .Values.clusterController.image | default (printf "%s/unikorn-compute-cluster-controller:%s" (include "unikorn.defaultRepositoryPath" .) (.Values.tag | default (include "unikorn.defaultTag" .))) }}
{{- end }}

{{- define "unikorn.networkConsumerImage" -}}
{{- .Values.networkConsumer.image | default (printf "%s/unikorn-compute-network-consumer:%s" (include "unikorn.defaultRepositoryPath" .) (.Values.tag | default (include "unikorn.defaultTag" .))) }}
{{- end }}

{{- define "unikorn.computeServerImage" -}}
{{- .Values.server.image | default (printf "%s/unikorn-compute-server:%s" (include "unikorn.defaultRepositoryPath" .) (.Values.tag | default (include "unikorn.defaultTag" .))) }}
{{- end }}

{{- define "unikorn.computeMonitorImage" -}}
{{- .Values.monitor.image | default (printf "%s/unikorn-compute-monitor:%s" (include "unikorn.defaultRepositoryPath" .) (.Values.tag | default (include "unikorn.defaultTag" .))) }}
{{- end }}
