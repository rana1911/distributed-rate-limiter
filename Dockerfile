FROM golang:1.22-alpine AS build
WORKDIR /app

# Required because GOPROXY=direct downloads modules using git
RUN apk add --no-cache git

COPY go.mod go.sum ./

# GOPROXY=direct + GOSUMDB=off matches how the module was originally
# fetched in development (no module-proxy access)
ENV GOPROXY=direct GOSUMDB=off

RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 go build -o /rate-limiter ./cmd/server


FROM alpine:3.19

COPY --from=build /rate-limiter /rate-limiter

EXPOSE 3000

CMD ["/rate-limiter"]