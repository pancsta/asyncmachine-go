FROM golang:1.20-alpine

WORKDIR /app
RUN go install github.com/tsliwowicz/go-wrk@latest

CMD ["/app/go-wrk.sh"]
