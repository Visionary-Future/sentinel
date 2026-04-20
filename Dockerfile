FROM registry.cn-beijing.aliyuncs.com/yhkl/linux_arm64_golang:1.26.2-alpine AS builder

ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=$GOPROXY

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o sentinel ./cmd/sentinel

FROM registry.cn-beijing.aliyuncs.com/yhkl/linux_arm64_alpine:3.20
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app
COPY --from=builder /app/sentinel .
COPY --from=builder /app/migrations ./migrations
COPY --from=builder /app/configs ./configs

EXPOSE 8080
ENTRYPOINT ["./sentinel"]
