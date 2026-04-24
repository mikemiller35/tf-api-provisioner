FROM golang:1.26.2-alpine AS builder
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -ldflags "-s -w" -a -o go-tf-provisioner .

FROM hashicorp/terraform:1.14.9 AS tf

FROM gcr.io/distroless/static:nonroot AS dist
COPY --from=tf /bin/terraform /usr/local/bin/terraform
COPY --from=builder /workspace/go-tf-provisioner /usr/local/bin/go-tf-provisioner
ENTRYPOINT ["/usr/local/bin/go-tf-provisioner"]
CMD ["serve"]
