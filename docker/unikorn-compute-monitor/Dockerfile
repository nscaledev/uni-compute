FROM gcr.io/distroless/static:nonroot

LABEL org.opencontainers.image.source=https://github.com/nscaledev/uni-compute

# This is implcitly created by 'docker buildx build'
ARG TARGETARCH

COPY bin/${TARGETARCH}-linux-gnu/unikorn-compute-monitor /

ENTRYPOINT ["/unikorn-compute-monitor"]
