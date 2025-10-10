FROM gcr.io/distroless/static:nonroot

# This is implcitly created by 'docker buildx build'
ARG TARGETARCH

COPY bin/${TARGETARCH}-linux-gnu/unikorn-compute-network-consumer /

ENTRYPOINT ["/unikorn-compute-network-consumer"]
