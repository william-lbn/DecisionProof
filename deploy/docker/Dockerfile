# 第一阶段使用完整 Go 镜像完成可重复构建。
FROM golang:1.25@sha256:699337d620559a59b4a2bb298ad59611e535d2ee755a34cf2d2a98f37578dc80 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/control-api ./cmd/control-api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

# 运行镜像不包含 Shell、包管理器和编译器，降低攻击面。
FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35
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
