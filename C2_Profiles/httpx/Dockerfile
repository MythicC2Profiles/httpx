FROM golang:1.25 AS builder

WORKDIR /Mythic/

COPY [".", "."]

RUN go mod tidy && cd httpx/c2_code && go mod tidy && cd /Mythic/

RUN make build

FROM alpine

RUN apk add make

COPY --from=builder /main /main
COPY --from=builder /mythic_httpx_server /mythic_httpx_server

WORKDIR /Mythic/

COPY [".", "."]

CMD make run