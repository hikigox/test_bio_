FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -o /out/eval ./cmd/eval

FROM alpine:3.20
RUN adduser -D -u 10001 app && mkdir -p /var/lib/app && chown app /var/lib/app
COPY --from=build /out/api /usr/local/bin/api
COPY --from=build /out/eval /usr/local/bin/eval
USER app
EXPOSE 8080
ENTRYPOINT ["api"]
