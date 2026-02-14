FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod server.go ./
COPY templates/ templates/
COPY static/ static/
RUN go build -o /chomp-server server.go

FROM alpine:3.19
RUN apk add --no-cache bash jq
COPY --from=build /chomp-server /usr/local/bin/chomp-server
COPY bin/chomp /usr/local/bin/chomp
RUN chmod +x /usr/local/bin/chomp
WORKDIR /app
ENV CHOMP_DIR=/app
ENV PORT=8001
EXPOSE 8001
VOLUME /app/state
CMD ["chomp-server"]
