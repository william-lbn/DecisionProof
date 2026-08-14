# 第一阶段使用完整 Go 镜像完成可重复构建。
FROM golang:1.23@sha256:60deed95d3888cc5e4d9ff8a10c54e5edc008c6ae3fba6187be6fb592e19e8c0 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/control-api ./cmd/control-api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# 运行镜像不包含 Shell、包管理器和编译器，降低攻击面。
FROM gcr.io/distroless/static-debian12:nonroot@sha256:1b7b9f0f0e0a1d2155f531db587cc48ec26aaf97ab64364225f5bf18a054e66a
ARG VERSION=dev
ARG REVISION=unknown
LABEL org.opencontainers.image.title="DecisionProof" \
      org.opencontainers.image.description="产品决策到生产结果的可信闭环平台工程底座" \
      org.opencontainers.image.source="https://github.com/william-lbn/DecisionProof" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY --from=builder /out/control-api /control-api
COPY --from=builder /out/migrate /migrate
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/control-api"]
