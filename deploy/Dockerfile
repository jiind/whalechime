FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /out/whalechime-agent ./cmd/whalechime-agent
RUN CGO_ENABLED=0 go build -trimpath -o /out/whalechime ./cmd/whalechime

FROM scratch AS agent
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/whalechime-agent /whalechime-agent
USER 65532:65532
ENTRYPOINT ["/whalechime-agent"]

FROM scratch AS server
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/whalechime /whalechime
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/whalechime"]
