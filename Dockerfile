FROM container-registry.test.betsson.tech/cache/library/golang:1.16 as builder

WORKDIR /app
COPY . .
RUN bash ./build.sh

FROM container-registry.test.betsson.tech/cache/library/alpine:latest as release
RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*
COPY --from=builder /app/bin/app .
CMD ["./app", "-o", "BetssonGroup"]