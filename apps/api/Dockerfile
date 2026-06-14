FROM golang:1.23 AS build
WORKDIR /src

COPY apps/api/go.mod apps/api/go.sum* ./
RUN go mod download || true
COPY apps/api/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /out/meterbase ./cmd/meterbase

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/meterbase /meterbase
EXPOSE 48888
ENTRYPOINT ["/meterbase"]
